// Package sim is the demo-only incident simulator. It drives anvil's cheat methods to
// replay an attack against the local chain, and only ever runs when DEMO_MODE=true, the chain
// id is 31337 and the node is anvil (specs D1, docs/api.md §1). Every result is labeled
// "Simulated on an Arbitrum One fork" (specs D2).
package sim

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/config"
	"github.com/AlphaTechini/heimdall/server/internal/executor"
	"github.com/AlphaTechini/heimdall/server/internal/hub"
	"github.com/AlphaTechini/heimdall/server/internal/policy"
	"github.com/AlphaTechini/heimdall/server/internal/signals"
	"github.com/AlphaTechini/heimdall/server/internal/store"
	"github.com/AlphaTechini/heimdall/server/internal/watcher"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// Label is shown with every simulated result.
const Label = "Simulated on an Arbitrum One fork"

var (
	ErrBusy    = errors.New("a scenario is already running")
	ErrUnknown = errors.New("unknown scenario")
)

const (
	stepBlocks  = 2 // blocks between attacker steps so each stage is visible
	waitTimeout = 90 * time.Second
)

// Sim is the simulator.
type Sim struct {
	ctx     context.Context
	env     *config.Env
	targets *config.Targets
	ch      *chain.Client
	st      *store.Store
	w       *watcher.Watcher
	ex      *executor.Executor
	pol     *policy.Service
	hub     *hub.Hub
	em      *hub.Emitter
	factory common.Address

	enabled bool

	mu      sync.Mutex
	running string
	snapID  string
	snapBlk uint64
}

// New creates the simulator. Call Init before use.
func New(ctx context.Context, env *config.Env, tg *config.Targets, ch *chain.Client, st *store.Store, w *watcher.Watcher, ex *executor.Executor, pol *policy.Service, h *hub.Hub, em *hub.Emitter, factory common.Address) *Sim {
	return &Sim{ctx: ctx, env: env, targets: tg, ch: ch, st: st, w: w, ex: ex, pol: pol, hub: h, em: em, factory: factory}
}

// Init evaluates the guard and, when the simulator is allowed, takes the reset snapshot.
func (s *Sim) Init(ctx context.Context) error {
	if !s.env.DemoMode {
		return nil
	}
	if s.ch.ChainID.Uint64() != 31337 {
		slog.Warn("DEMO_MODE is on but the chain is not the local fork (chain id 31337); the simulator stays off")
		return nil
	}
	v, err := s.ch.ClientVersion(ctx)
	if err != nil || !strings.HasPrefix(strings.ToLower(v), "anvil") {
		slog.Warn("DEMO_MODE is on but the node is not anvil; the simulator stays off", "clientVersion", v)
		return nil
	}
	s.enabled = true
	return s.snapshot(ctx)
}

// Enabled reports whether the simulator may run.
func (s *Sim) Enabled() bool { return s != nil && s.enabled }

func (s *Sim) snapshot(ctx context.Context) error {
	var id string
	if err := s.ch.Raw(ctx, &id, "evm_snapshot"); err != nil {
		return fmt.Errorf("evm_snapshot failed: %w", err)
	}
	n, err := s.ch.Eth.BlockNumber(ctx)
	if err != nil {
		return err
	}
	s.snapID, s.snapBlk = id, n
	_ = s.st.SetMeta(ctx, "sim_snapshot_id", id)
	_ = s.st.SetMeta(ctx, "sim_snapshot_block", fmt.Sprint(n))
	slog.Info("simulator snapshot taken", "id", id, "block", n)
	return nil
}

// Reset reverts the chain to the snapshot taken at start, clears rows created since and
// takes a new snapshot.
func (s *Sim) Reset(ctx context.Context) error {
	s.mu.Lock()
	if s.running != "" {
		s.mu.Unlock()
		return ErrBusy
	}
	defer s.mu.Unlock()
	var ok bool
	if err := s.ch.Raw(ctx, &ok, "evm_revert", s.snapID); err != nil {
		return fmt.Errorf("evm_revert failed: %w", err)
	}
	if !ok {
		return errors.New("the snapshot is no longer valid (was anvil restarted?); restart heimdalld")
	}
	if err := s.st.CleanupAfterBlock(ctx, s.snapBlk); err != nil {
		return err
	}
	s.ex.Reset()
	s.pol.Reset()
	if err := s.w.Reset(ctx); err != nil {
		return err
	}
	if err := s.snapshot(ctx); err != nil {
		return err
	}
	s.event(s.snapBlk, "sim", "Simulation reset: the chain is back to where it started.")
	return nil
}

// ---- scenarios ----

// Scenario describes one scenario for GET /sim/scenarios.
type Scenario struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Available   bool    `json:"available"`
	Reason      *string `json:"reason"`
}

// ScenariosResp is the body of GET /sim/scenarios.
type ScenariosResp struct {
	Label     string     `json:"label"`
	Scenarios []Scenario `json:"scenarios"`
	Running   *string    `json:"running"`
}

func (s *Sim) fastDrainTarget() *config.Target {
	for i := range s.targets.Targets {
		t := &s.targets.Targets[i]
		if t.Type == "ERC4626" && len(t.Sim.Drainers) > 0 {
			return t
		}
	}
	return nil
}

func (s *Sim) oracleTarget() *config.Target {
	for i := range s.targets.Targets {
		t := &s.targets.Targets[i]
		if t.Signals.MarketFeed.Set() && t.Signals.ReferenceFeed.Set() {
			return t
		}
	}
	return nil
}

func (s *Sim) collateralTarget() (*config.Target, *config.CollateralFeed) {
	for i := range s.targets.Targets {
		t := &s.targets.Targets[i]
		for j := range t.Signals.CollateralFeeds {
			if t.Signals.CollateralFeeds[j].Address != "" {
				return t, &t.Signals.CollateralFeeds[j]
			}
		}
	}
	return nil, nil
}

func reason(s string) *string { return &s }

// Scenarios lists the scenarios and whether their inputs are filled in.
func (s *Sim) Scenarios() ScenariosResp {
	s.mu.Lock()
	defer s.mu.Unlock()
	fd := Scenario{ID: "fast-drain", Name: "Fast drain",
		Description: "Attackers pull money out of the vault block by block. Outflow and liquidity signals fire, Heimdall exits, and part of the money is stuck until liquidity returns.", Available: true}
	if s.fastDrainTarget() == nil {
		fd.Available, fd.Reason = false, reason("No vault has drainer accounts set up. Fill in sim.drainers for a vault in the targets file.")
	}
	ot := Scenario{ID: "oracle-tampering", Name: "Oracle tampering",
		Description: "The price the protocol uses is pushed away from the real price, like the Ostium incident in July 2026.", Available: true}
	if s.oracleTarget() == nil {
		ot.Available, ot.Reason = false, reason("No vault has both a market price feed and a reference price feed set in the targets file.")
	}
	cd := Scenario{ID: "collateral-depeg", Name: "Collateral depeg",
		Description: "A stablecoin backing the loans loses its peg, like Stream Finance's xUSD in November 2025.", Available: true}
	if t, _ := s.collateralTarget(); t == nil {
		cd.Available, cd.Reason = false, reason("No vault has a collateral price feed set in the targets file.")
	}
	var running *string
	if s.running != "" {
		r := s.running
		running = &r
	}
	return ScenariosResp{Label: Label, Scenarios: []Scenario{fd, ot, cd}, Running: running}
}

// Run starts a scenario in the background and returns its run id.
func (s *Sim) Run(id string) (int64, error) {
	var t *config.Target
	var fn func(ctx context.Context, r *run) error
	switch id {
	case "fast-drain":
		t, fn = s.fastDrainTarget(), s.fastDrain
		if t == nil {
			return 0, errors.New("This scenario needs a vault with drainer accounts (sim.drainers in the targets file).")
		}
	case "oracle-tampering":
		t, fn = s.oracleTarget(), s.oracleTampering
		if t == nil {
			return 0, errors.New("This scenario needs a vault with a market price feed and a reference price feed in the targets file.")
		}
	case "collateral-depeg":
		t, _ = s.collateralTarget()
		fn = s.collateralDepeg
		if t == nil {
			return 0, errors.New("This scenario needs a vault with a collateral price feed in the targets file.")
		}
	default:
		return 0, ErrUnknown
	}
	s.mu.Lock()
	if s.running != "" {
		s.mu.Unlock()
		return 0, ErrBusy
	}
	head, err := s.ch.Eth.BlockNumber(s.ctx)
	if err != nil {
		s.mu.Unlock()
		return 0, fmt.Errorf("cannot read the chain: %w", err)
	}
	runID, err := s.st.InsertSimRun(s.ctx, id, t.ID, head)
	if err != nil {
		s.mu.Unlock()
		return 0, err
	}
	s.running = id
	s.mu.Unlock()
	r := &run{id: runID, scenario: id, target: t, start: head}
	go func() {
		status := "done"
		err := fn(s.ctx, r)
		if err != nil {
			status = "failed"
			slog.Error("simulation failed", "scenario", id, "err", err)
			s.line(r, "Simulation stopped: "+err.Error(), true)
		}
		end, _ := s.ch.Eth.BlockNumber(s.ctx)
		if r.drainDone == 0 {
			r.drainDone = end
		}
		if err := s.st.FinishSimRun(s.ctx, runID, status, r.drainDone, end); err != nil {
			slog.Error("cannot store simulation result", "err", err)
		}
		s.mu.Lock()
		s.running = ""
		s.mu.Unlock()
	}()
	return runID, nil
}

type run struct {
	id        int64
	scenario  string
	target    *config.Target
	start     uint64
	drainDone uint64
}

func (s *Sim) event(blk uint64, kind, msg string) {
	s.em.Emit(s.ctx, store.Event{Kind: kind, Block: blk, Message: Label + ": " + msg})
}

// line reports progress over WebSocket and in the activity feed.
func (s *Sim) line(r *run, text string, done bool) {
	blk, _ := s.ch.Eth.BlockNumber(s.ctx)
	s.hub.Publish("sim", map[string]any{"runId": r.id, "line": text, "done": done, "label": Label})
	tid := r.target.ID
	s.em.Emit(s.ctx, store.Event{Kind: "sim", Block: blk, TargetID: &tid, Message: Label + ": " + text})
}

// ---- anvil helpers ----

func (s *Sim) impersonate(ctx context.Context, a common.Address) error {
	if err := s.ch.Raw(ctx, nil, "anvil_impersonateAccount", a.Hex()); err != nil {
		return fmt.Errorf("anvil_impersonateAccount failed (is this anvil?): %w", err)
	}
	return s.ch.Raw(ctx, nil, "anvil_setBalance", a.Hex(), hexutil.EncodeBig(new(big.Int).Mul(big.NewInt(100), chain.Pow10(18))))
}

func (s *Sim) waitReceipt(ctx context.Context, h common.Hash) error {
	// Generous: on a fresh fork the first call into a protocol fetches its state from the upstream RPC.
	deadline := time.Now().Add(120 * time.Second)
	lastMine := time.Now()
	for time.Now().Before(deadline) {
		rc, err := s.ch.Eth.TransactionReceipt(ctx, h)
		if err == nil {
			if rc.Status != 1 {
				return fmt.Errorf("transaction %s reverted", h.Hex())
			}
			return nil
		}
		if !errors.Is(err, ethereum.NotFound) {
			return err
		}
		if time.Since(lastMine) > 3*time.Second {
			// No block-time configured: mine one so the transaction lands.
			_ = s.ch.Raw(ctx, nil, "evm_mine")
			lastMine = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("transaction %s was not mined in time", h.Hex())
}

func (s *Sim) send(ctx context.Context, from, to common.Address, data []byte) error {
	call := map[string]any{"from": from.Hex(), "to": to.Hex(), "data": hexutil.Encode(data), "gas": hexutil.EncodeUint64(3_000_000)}
	var h common.Hash
	if err := s.ch.Raw(ctx, &h, "eth_sendTransaction", call); err != nil {
		return fmt.Errorf("sending a simulated transaction failed: %w", err)
	}
	return s.waitReceipt(ctx, h)
}

func (s *Sim) waitBlocks(ctx context.Context, n uint64) error {
	start, err := s.ch.Eth.BlockNumber(ctx)
	if err != nil {
		return err
	}
	lastMine, lastHead := time.Now(), start
	for {
		cur, err := s.ch.Eth.BlockNumber(ctx)
		if err != nil {
			return err
		}
		if cur >= start+n {
			return nil
		}
		if cur != lastHead {
			lastHead, lastMine = cur, time.Now()
		}
		if time.Since(lastMine) > 3*time.Second {
			_ = s.ch.Raw(ctx, nil, "evm_mine")
			lastMine = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func fmtUnits(v *big.Int, t *config.Target) string {
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(v), new(big.Float).SetInt(chain.Pow10(t.AssetDecimals))).Float64()
	return signals.FormatAmount(f, t.AssetSymbol)
}

// ---- scenarios ----

// adaGuard returns the demo Ada's guard, if she has one.
func (s *Sim) adaGuard(ctx context.Context) (common.Address, bool) {
	if s.targets.Demo.Ada == "" {
		return common.Address{}, false
	}
	g, err := s.ch.Addr(ctx, &chain.FactoryABI, s.factory, nil, "guardOf", common.HexToAddress(s.targets.Demo.Ada))
	if err != nil || g == (common.Address{}) {
		return common.Address{}, false
	}
	return g, true
}

// exitState reports Ada's newest exit of the target since the run began.
func (s *Sim) exitState(ctx context.Context, r *run, guard common.Address) (started, progressed, finished bool) {
	e, err := s.st.LastExit(ctx, guard.Hex(), r.target.ID)
	if err != nil || e.StartBlock < r.start {
		return false, false, false
	}
	return true, e.TotalOut != "0" || e.Status != "active", e.Status != "active"
}

func (s *Sim) waitExit(ctx context.Context, r *run, guard common.Address, want func(started, progressed, finished bool) bool, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		a, b, c := s.exitState(ctx, r, guard)
		if want(a, b, c) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func (s *Sim) fastDrain(ctx context.Context, r *run) error {
	t := r.target
	vault, asset := t.Addr(), t.AssetAddr()
	var drainers []common.Address
	for _, d := range t.Sim.Drainers {
		a := common.HexToAddress(d)
		if err := s.impersonate(ctx, a); err != nil {
			return err
		}
		drainers = append(drainers, a)
	}
	ta0, err := s.ch.Uint(ctx, &chain.ERC4626ABI, vault, nil, "totalAssets")
	if err != nil {
		return err
	}
	chunk := new(big.Int).Div(new(big.Int).Mul(ta0, big.NewInt(55)), big.NewInt(1000)) // 5.5% of assets per step
	adaG, hasAda := s.adaGuard(ctx)
	s.line(r, fmt.Sprintf("Attack starting on %s (%s in the vault).", t.Label, fmtUnits(ta0, t)), false)

	redeem := func(d common.Address, assets *big.Int) (*big.Int, error) {
		shares, err := s.ch.Uint(ctx, &chain.ERC4626ABI, vault, nil, "convertToShares", assets)
		if err != nil {
			return nil, err
		}
		maxR, err := s.ch.Uint(ctx, &chain.ERC4626ABI, vault, nil, "maxRedeem", d)
		if err != nil {
			return nil, err
		}
		if maxR.Cmp(shares) < 0 {
			shares = maxR
		}
		if shares.Sign() == 0 {
			return new(big.Int), nil
		}
		data, _ := chain.ERC4626ABI.Pack("redeem", shares, d, d)
		if err := s.send(ctx, d, vault, data); err != nil {
			return nil, err
		}
		out, err := s.ch.Uint(ctx, &chain.ERC4626ABI, vault, nil, "convertToAssets", shares)
		if err != nil {
			return shares, nil
		}
		return out, nil
	}
	pull := func(i int) error {
		d := drainers[i%len(drainers)]
		out, err := redeem(d, chunk)
		if err != nil {
			return err
		}
		blk, _ := s.ch.Eth.BlockNumber(ctx)
		s.line(r, fmt.Sprintf("Block %d: drainer %s redeemed about %s.", blk, short(d), fmtUnits(out, t)), false)
		return nil
	}

	for i := 0; i < 3; i++ {
		if err := pull(i); err != nil {
			return err
		}
		// On the local mock the third pull is followed at once by the borrowers' move below, so
		// the cash is already gone when Heimdall's exit lands (that makes the exit partial).
		if i < 2 || !t.Sim.MockVault {
			if err := s.waitBlocks(ctx, stepBlocks); err != nil {
				return err
			}
		}
	}
	if t.Sim.MockVault {
		// Borrowers take most of the remaining idle cash, so less is withdrawable than Ada holds.
		idle, err := s.ch.Uint(ctx, &chain.MockVaultABI, vault, nil, "idle")
		if err != nil {
			return err
		}
		pos := new(big.Int).Div(ta0, big.NewInt(22)) // ~4.5% of the vault, about Ada's 10,000 of 220,000
		if hasAda {
			if held, err := s.ch.Uint(ctx, &chain.GuardABI, adaG, nil, "held", t.PositionType(), vault); err == nil && held.Sign() > 0 {
				if v, err := s.ch.Uint(ctx, &chain.ERC4626ABI, vault, nil, "convertToAssets", held); err == nil {
					pos = v
				}
			}
		}
		keep := new(big.Int).Div(new(big.Int).Mul(pos, big.NewInt(62)), big.NewInt(100)) // leave ~62% of her position
		if idle.Cmp(keep) > 0 {
			take := new(big.Int).Sub(idle, keep)
			data, _ := chain.MockVaultABI.Pack("lend", take, drainers[0])
			if err := s.send(ctx, drainers[0], vault, data); err != nil {
				return err
			}
			blk, _ := s.ch.Eth.BlockNumber(ctx)
			s.line(r, fmt.Sprintf("Block %d: borrowers took %s of idle cash. Only %s is left to withdraw.", blk, fmtUnits(take, t), fmtUnits(keep, t)), false)
		}
	}
	// Give the watcher time to reach Critical and the executor time to react.
	if hasAda {
		if s.waitExit(ctx, r, adaG, func(a, p, f bool) bool { return p }, waitTimeout) {
			s.line(r, "Heimdall's first exit transaction has landed.", false)
		}
		if t.Sim.MockVault {
			if err := s.waitBlocks(ctx, stepBlocks); err != nil {
				return err
			}
			if _, _, fin := s.exitState(ctx, r, adaG); !fin {
				bal, err := s.ch.BalanceOf(ctx, asset, drainers[0], nil)
				if err != nil {
					return err
				}
				repay := new(big.Int).Div(new(big.Int).Mul(ta0, big.NewInt(30)), big.NewInt(100))
				if bal.Cmp(repay) < 0 {
					repay = bal
				}
				// New money comes into the vault as a deposit, so the share price does not change.
				approve, _ := chain.ERC20ABI.Pack("approve", vault, repay)
				if err := s.send(ctx, drainers[0], asset, approve); err != nil {
					return err
				}
				data, _ := chain.ERC4626ABI.Pack("deposit", repay, drainers[0])
				if err := s.send(ctx, drainers[0], vault, data); err != nil {
					return err
				}
				blk, _ := s.ch.Eth.BlockNumber(ctx)
				s.line(r, fmt.Sprintf("Block %d: new deposits of %s arrived. Liquidity is back.", blk, fmtUnits(repay, t)), false)
			}
		} else {
			// Real vaults: keep pulling what the drainers can, so liquidity stays scarce.
			for i := 3; i < 6; i++ {
				if err := pull(i); err != nil {
					return err
				}
				if err := s.waitBlocks(ctx, stepBlocks); err != nil {
					return err
				}
			}
		}
		if s.waitExit(ctx, r, adaG, func(a, p, f bool) bool { return f }, waitTimeout) {
			s.line(r, "Heimdall's exit is finished.", false)
		}
	} else {
		s.line(r, "Ada has no Guard yet, so nothing is protected. Create a Guard and run this again.", false)
		if err := s.waitBlocks(ctx, stepBlocks); err != nil {
			return err
		}
	}
	// The drain finishes: whatever cash is left is taken, so unprotected holders are stuck.
	for _, d := range drainers {
		for k := 0; k < 3; k++ {
			maxA, err := s.ch.Uint(ctx, &chain.ERC4626ABI, vault, nil, "maxWithdraw", d)
			if err != nil || maxA.Sign() == 0 {
				break
			}
			out, err := redeem(d, maxA)
			if err != nil || out.Sign() == 0 {
				break
			}
		}
	}
	r.drainDone, _ = s.ch.Eth.BlockNumber(ctx)
	s.line(r, fmt.Sprintf("Block %d: the drain is finished. Anyone still in the vault without protection cannot withdraw.", r.drainDone), true)
	return nil
}

func short(a common.Address) string { h := a.Hex(); return h[:6] + ".." + h[len(h)-4:] }

// mockFeed makes sure the feed at addr is a controllable MockPriceFeed. On a fork it replaces the
// real feed's code with the mock's runtime bytecode (anvil_setCode) and rewrites the mock's own
// storage slots (layout from `forge inspect MockPriceFeed storageLayout`: 0 latestAnswer,
// 1 decimals, 2 latestTimestamp, 3 latestRound). It returns the feed's current answer and decimals.
func (s *Sim) mockFeed(ctx context.Context, addr common.Address) (*big.Int, int, error) {
	p, err := s.ch.LatestPrice(ctx, addr, nil)
	if err != nil {
		return nil, 0, err
	}
	code, err := s.ch.Eth.CodeAt(ctx, addr, nil)
	if err != nil {
		return nil, 0, err
	}
	if string(code) != string(chain.MockFeedRuntime) {
		if err := s.ch.Raw(ctx, nil, "anvil_setCode", addr.Hex(), hexutil.Encode(chain.MockFeedRuntime)); err != nil {
			return nil, 0, fmt.Errorf("anvil_setCode failed: %w", err)
		}
		word := func(v *big.Int) string { return common.BigToHash(v).Hex() }
		slots := map[int]*big.Int{0: p.Answer, 1: big.NewInt(int64(p.Decimals)), 2: big.NewInt(0), 3: big.NewInt(0)}
		for slot, v := range slots {
			if err := s.ch.Raw(ctx, nil, "anvil_setStorageAt", addr.Hex(), word(big.NewInt(int64(slot))), word(v)); err != nil {
				return nil, 0, fmt.Errorf("anvil_setStorageAt failed: %w", err)
			}
		}
	}
	return p.Answer, p.Decimals, nil
}

func (s *Sim) setPrice(ctx context.Context, feed common.Address, answer *big.Int) error {
	from := common.HexToAddress(s.targets.Demo.Ada)
	if s.targets.Demo.Ada == "" {
		from = feed
	}
	if err := s.impersonate(ctx, from); err != nil {
		return err
	}
	data, _ := chain.MockFeedABI.Pack("setPrice", answer)
	return s.send(ctx, from, feed, data)
}

func scale(v *big.Int, num, den int64) *big.Int {
	return new(big.Int).Div(new(big.Int).Mul(v, big.NewInt(num)), big.NewInt(den))
}

// pushPrice moves a feed to base*num/den in two visible steps: first a Warning-sized move, then a Critical one.
func (s *Sim) twoStep(ctx context.Context, r *run, label string, feed common.Address, base *big.Int, warnNum, critNum int64, wasAt string) error {
	for _, num := range []int64{warnNum, critNum} {
		ans := scale(base, num, 100)
		if err := s.setPrice(ctx, feed, ans); err != nil {
			return err
		}
		blk, _ := s.ch.Eth.BlockNumber(ctx)
		s.line(r, fmt.Sprintf("Block %d: %s moved to %d%% of %s.", blk, label, num, wasAt), false)
		if err := s.waitBlocks(ctx, stepBlocks+1); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sim) finishScenario(ctx context.Context, r *run) error {
	adaG, hasAda := s.adaGuard(ctx)
	if hasAda {
		if s.waitExit(ctx, r, adaG, func(a, p, f bool) bool { return f }, waitTimeout) {
			s.line(r, "Heimdall's exit is finished.", false)
		}
	} else {
		s.line(r, "Ada has no Guard yet, so nothing is protected. Create a Guard and run this again.", false)
	}
	r.drainDone, _ = s.ch.Eth.BlockNumber(ctx)
	s.line(r, "Scenario finished. Use Reset to go back to the start.", true)
	return nil
}

func (s *Sim) oracleTampering(ctx context.Context, r *run) error {
	t := r.target
	feed := t.Signals.MarketFeed.Addr()
	s.line(r, fmt.Sprintf("Tampering with the price feed %s uses for %s.", short(feed), t.Label), false)
	ans, _, err := s.mockFeed(ctx, feed)
	if err != nil {
		return err
	}
	if err := s.twoStep(ctx, r, "the protocol's price", feed, ans, 97, 88, "the real price"); err != nil {
		return err
	}
	return s.finishScenario(ctx, r)
}

func (s *Sim) collateralDepeg(ctx context.Context, r *run) error {
	t, cf := s.collateralTarget()
	r.target = t
	feed := common.HexToAddress(cf.Address)
	s.line(r, fmt.Sprintf("%s starts losing its peg.", cf.Label), false)
	ans, _, err := s.mockFeed(ctx, feed)
	if err != nil {
		return err
	}
	if err := s.twoStep(ctx, r, cf.Label+"'s price", feed, ans, 97, 90, "$1"); err != nil {
		return err
	}
	return s.finishScenario(ctx, r)
}

// ---- compare ----

// Holder is one side of the comparison.
type Holder struct {
	Address         string `json:"address"`
	InWallet        string `json:"inWallet"`
	InVault         string `json:"inVault"`
	WithdrawableNow string `json:"withdrawableNow"`
}

// Timeline lists the key blocks of the latest fast drain.
type Timeline struct {
	FirstSignalBlock   *uint64 `json:"firstSignalBlock"`
	ExitSubmittedBlock *uint64 `json:"exitSubmittedBlock"`
	ExitConfirmedBlock *uint64 `json:"exitConfirmedBlock"`
	DrainFinishedBlock *uint64 `json:"drainFinishedBlock"`
}

// Compare is the body of GET /sim/compare.
type Compare struct {
	Label    string   `json:"label"`
	TargetID string   `json:"targetId"`
	Ada      Holder   `json:"ada"`
	Ben      Holder   `json:"ben"`
	Timeline Timeline `json:"timeline"`
}

func (s *Sim) holder(ctx context.Context, t *config.Target, who common.Address) (Holder, error) {
	h := Holder{Address: who.Hex(), InWallet: "0", InVault: "0", WithdrawableNow: "0"}
	w, err := s.ch.BalanceOf(ctx, t.AssetAddr(), who, nil)
	if err != nil {
		return h, err
	}
	h.InWallet = w.String()
	shares := new(big.Int)
	withdrawable := new(big.Int)
	if b, err := s.ch.BalanceOf(ctx, t.Addr(), who, nil); err == nil {
		shares.Add(shares, b)
	}
	if v, err := s.ch.Uint(ctx, &chain.ERC4626ABI, t.Addr(), nil, "maxWithdraw", who); err == nil {
		withdrawable.Add(withdrawable, v)
	}
	if g, err := s.ch.Addr(ctx, &chain.FactoryABI, s.factory, nil, "guardOf", who); err == nil && g != (common.Address{}) {
		if held, err := s.ch.Uint(ctx, &chain.GuardABI, g, nil, "held", t.PositionType(), t.Addr()); err == nil {
			shares.Add(shares, held)
		}
		if ex, err := s.ch.Uint(ctx, &chain.GuardABI, g, nil, "exitable", t.PositionType(), t.Addr()); err == nil && ex.Sign() > 0 {
			if v, err := s.ch.Uint(ctx, &chain.ERC4626ABI, t.Addr(), nil, "convertToAssets", ex); err == nil {
				withdrawable.Add(withdrawable, v)
			}
		}
	}
	vaultVal := new(big.Int)
	if shares.Sign() > 0 {
		v, err := s.ch.Uint(ctx, &chain.ERC4626ABI, t.Addr(), nil, "convertToAssets", shares)
		if err != nil {
			return h, err
		}
		vaultVal = v
	}
	h.InVault, h.WithdrawableNow = vaultVal.String(), withdrawable.String()
	return h, nil
}

// Compare compares Ada (protected) with Ben (unprotected) after the latest fast drain.
func (s *Sim) Compare(ctx context.Context) (*Compare, error) {
	t := s.fastDrainTarget()
	if t == nil {
		return nil, errors.New("No vault is set up for the simulator.")
	}
	if s.targets.Demo.Ada == "" || s.targets.Demo.Ben == "" {
		return nil, errors.New("The demo wallets (demo.ada and demo.ben) are not set in the targets file.")
	}
	ada, ben := common.HexToAddress(s.targets.Demo.Ada), common.HexToAddress(s.targets.Demo.Ben)
	c := &Compare{Label: Label, TargetID: t.ID}
	var err error
	if c.Ada, err = s.holder(ctx, t, ada); err != nil {
		return nil, fmt.Errorf("Could not read Ada's balances: %w", err)
	}
	if c.Ben, err = s.holder(ctx, t, ben); err != nil {
		return nil, fmt.Errorf("Could not read Ben's balances: %w", err)
	}
	run, err := s.st.LatestSimRun(ctx, "fast-drain")
	if err != nil {
		return c, nil // no run yet: balances only
	}
	c.Timeline.DrainFinishedBlock = run.DrainFinishedBlock
	if incs, err := s.st.IncidentsSince(ctx, t.ID, run.StartBlock); err == nil {
		for _, in := range incs {
			if in.Severity != signals.SevWatch {
				b := in.Block
				c.Timeline.FirstSignalBlock = &b
				break
			}
		}
	}
	if g, ok := s.adaGuard(ctx); ok {
		if e, err := s.st.LastExit(ctx, g.Hex(), t.ID); err == nil && e.StartBlock >= run.StartBlock {
			b := e.StartBlock
			c.Timeline.ExitSubmittedBlock = &b
			c.Timeline.ExitConfirmedBlock = e.EndBlock
		}
	}
	return c, nil
}
