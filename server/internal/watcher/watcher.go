// Package watcher follows new blocks, decodes Heimdall and watched-contract logs, reads each
// target's state, computes the signals (package signals) and hands decisions to the executor.
package watcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/config"
	"github.com/AlphaTechini/heimdall/server/internal/executor"
	"github.com/AlphaTechini/heimdall/server/internal/hub"
	"github.com/AlphaTechini/heimdall/server/internal/notify"
	"github.com/AlphaTechini/heimdall/server/internal/policy"
	"github.com/AlphaTechini/heimdall/server/internal/signals"
	"github.com/AlphaTechini/heimdall/server/internal/store"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	pollInterval    = 250 * time.Millisecond
	wsHeartbeat     = 3 * time.Second
	checkEventEvery = 30 * time.Second
	snapshotEvery   = 2 * time.Second
	pushEvery       = 250 * time.Millisecond
	logChunk        = 5000
)

// Latest is the body of GET /signals/{targetId} and of the "signals" WebSocket message.
type Latest struct {
	TargetID  string           `json:"targetId"`
	Severity  string           `json:"severity"`
	Reason    string           `json:"reason"`
	UpdatedAt time.Time        `json:"updatedAt"`
	Block     uint64           `json:"block"`
	Signals   []signals.Signal `json:"signals"`
}

type guardInfo struct {
	Addr, Owner  common.Address
	CreatedBlock uint64
}

type targetState struct {
	t          *config.Target
	eng        *signals.Engine
	vaultDec   int
	severity   string
	episode    string
	latest     Latest
	lastCheck  time.Time
	lastSnap   time.Time
	lastPush   time.Time
	lastLevels string
	readWarned map[string]bool
}

// Watcher is the block follower.
type Watcher struct {
	env     *config.Env
	targets *config.Targets
	sig     *config.Signals
	ch      *chain.Client
	st      *store.Store
	em      *hub.Emitter
	hub     *hub.Hub
	pol     *policy.Service
	exec    *executor.Executor
	factory common.Address
	pool    common.Address

	procMu   sync.Mutex // one iteration (or a Reset) at a time
	next     uint64
	backfill bool // the first scan after a fresh start covers history: config changes in it are not live signals
	kick     chan struct{}
	wsUp     atomic.Bool
	last     atomic.Uint64 // last processed block
	lastAt   atomic.Int64

	mu       sync.RWMutex
	guards   map[common.Address]guardInfo
	states   map[string]*targetState
	s6Topics []common.Hash
	s6Names  map[common.Hash]string
}

// New creates the watcher.
func New(env *config.Env, tg *config.Targets, sig *config.Signals, ch *chain.Client, st *store.Store, em *hub.Emitter, h *hub.Hub, pol *policy.Service, ex *executor.Executor, factory common.Address) *Watcher {
	w := &Watcher{env: env, targets: tg, sig: sig, ch: ch, st: st, em: em, hub: h, pol: pol, exec: ex, factory: factory,
		kick: make(chan struct{}, 1), guards: map[common.Address]guardInfo{}, states: map[string]*targetState{}, s6Names: map[common.Hash]string{}}
	if tg.AaveV3.Pool.Set() {
		w.pool = tg.AaveV3.Pool.Addr()
	}
	for _, sigStr := range chain.S6Signatures {
		id := crypto.Keccak256Hash([]byte(sigStr))
		w.s6Topics = append(w.s6Topics, id)
		w.s6Names[id] = sigStr[:strings.Index(sigStr, "(")]
	}
	w.resetStates()
	return w
}

func (w *Watcher) resetStates() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.states = map[string]*targetState{}
	for i := range w.targets.Targets {
		t := &w.targets.Targets[i]
		ts := &targetState{t: t, severity: signals.SevWatch, readWarned: map[string]bool{}}
		noun := "Vault"
		if t.IsAave() {
			noun = "Aave reserve"
		}
		ts.eng = signals.NewEngine(w.sig, signals.Meta{Noun: noun, Label: t.Label, AssetSymbol: t.AssetSymbol, AssetDecimals: t.AssetDecimals, AssetIsStable: t.AssetIsStable})
		ts.latest = startingLatest(t.ID)
		w.states[t.ID] = ts
	}
}

func startingLatest(id string) Latest {
	l := Latest{TargetID: id, Severity: signals.SevWatch, Reason: "Starting up: waiting for the first check.", UpdatedAt: time.Now().UTC()}
	for _, i := range signals.Catalog {
		l.Signals = append(l.Signals, signals.Signal{ID: i.ID, Level: signals.LevelUnavailable, Detail: "Waiting for the first check."})
	}
	return l
}

// Latest returns the newest signals for a target.
func (w *Watcher) Latest(id string) (Latest, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	ts, ok := w.states[id]
	if !ok {
		return Latest{}, false
	}
	return ts.latest, true
}

// Severity returns a target's current severity.
func (w *Watcher) Severity(id string) string {
	l, ok := w.Latest(id)
	if !ok {
		return signals.SevWatch
	}
	return l.Severity
}

// Head returns the last processed block.
func (w *Watcher) Head() uint64 { return w.last.Load() }

// Guard returns a known guard by address.
func (w *Watcher) Guard(a common.Address) (guardInfo, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	g, ok := w.guards[a]
	return g, ok
}

// GuardInfo is the exported view of a guard.
type GuardInfo = guardInfo

func (w *Watcher) addGuard(g guardInfo) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.guards[g.Addr]; ok {
		return false
	}
	w.guards[g.Addr] = g
	return true
}

// EnsureGuard makes sure the owner's guard (if it exists on chain) is known. Used by the API
// when a guard was created before the watcher's log scan reached it.
func (w *Watcher) EnsureGuard(ctx context.Context, owner common.Address) (common.Address, bool, error) {
	g, err := w.ch.Addr(ctx, &chain.FactoryABI, w.factory, nil, "guardOf", owner)
	if err != nil {
		return common.Address{}, false, err
	}
	if g == (common.Address{}) {
		return common.Address{}, false, nil
	}
	if _, ok := w.Guard(g); !ok {
		gi := guardInfo{Addr: g, Owner: owner}
		if w.addGuard(gi) {
			if err := w.st.UpsertGuard(ctx, store.Guard{Address: g.Hex(), Owner: owner.Hex()}); err != nil {
				return g, true, err
			}
			_ = w.st.EnsureUser(ctx, owner.Hex())
		}
	}
	return g, true, nil
}

// LoadGuards reads known guards from the database.
func (w *Watcher) LoadGuards(ctx context.Context) error {
	gs, err := w.st.ListGuards(ctx)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.guards = map[common.Address]guardInfo{}
	for _, g := range gs {
		a := common.HexToAddress(g.Address)
		w.guards[a] = guardInfo{Addr: a, Owner: common.HexToAddress(g.Owner), CreatedBlock: g.CreatedBlock}
	}
	w.mu.Unlock()
	return nil
}

func (w *Watcher) guardList() []guardInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]guardInfo, 0, len(w.guards))
	for _, g := range w.guards {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedBlock < out[j].CreatedBlock })
	return out
}

// Reset clears rolling state after the simulator reverted the chain (docs/api.md /sim/reset).
func (w *Watcher) Reset(ctx context.Context) error {
	w.procMu.Lock()
	defer w.procMu.Unlock()
	h, err := w.ch.Eth.HeaderByNumber(ctx, nil)
	if err != nil {
		return err
	}
	w.next = h.Number.Uint64() + 1
	w.last.Store(h.Number.Uint64())
	w.resetStates()
	return w.LoadGuards(ctx)
}

// Run follows the chain until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	if w.env.RPCWSURL != "" {
		go w.subscribeWS(ctx)
	}
	if err := w.initCursor(ctx); err != nil {
		slog.Error("watcher cannot start", "err", err)
		return
	}
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	var lastErr time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
		case <-tick.C:
			if w.wsUp.Load() && time.Since(time.Unix(0, w.lastAt.Load())) < wsHeartbeat {
				continue
			}
		}
		if err := w.iterate(ctx); err != nil && ctx.Err() == nil {
			if time.Since(lastErr) > 5*time.Second {
				slog.Warn("watcher iteration failed; will retry", "err", err)
				lastErr = time.Now()
			}
		}
	}
}

func (w *Watcher) initCursor(ctx context.Context) error {
	head, err := w.ch.Eth.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("cannot read the latest block: %w", err)
	}
	w.procMu.Lock()
	defer w.procMu.Unlock()
	if v, _ := w.st.GetMeta(ctx, "cursor"); v != "" {
		var n uint64
		if _, err := fmt.Sscan(v, &n); err == nil && n <= head+1 {
			w.next = n
			w.backfill = false
			slog.Info("watcher resuming", "nextBlock", n, "head", head)
			return nil
		}
	}
	w.backfill = true
	switch {
	case w.env.StartBlock != nil:
		w.next = *w.env.StartBlock
	case w.ch.ChainID.Uint64() == 31337:
		w.next = w.forkBlock(ctx)
	default:
		w.next = head + 1
	}
	slog.Info("watcher starting", "nextBlock", w.next, "head", head)
	return nil
}

// forkBlock returns the block an anvil fork started from (0 on a plain anvil).
func (w *Watcher) forkBlock(ctx context.Context) uint64 {
	var info struct {
		ForkConfig *struct {
			ForkBlockNumber uint64 `json:"forkBlockNumber"`
		} `json:"forkConfig"`
	}
	if err := w.ch.Raw(ctx, &info, "anvil_nodeInfo"); err == nil && info.ForkConfig != nil {
		return info.ForkConfig.ForkBlockNumber
	}
	return 0
}

func (w *Watcher) subscribeWS(ctx context.Context) {
	for ctx.Err() == nil {
		func() {
			c, err := ethclient.DialContext(ctx, w.env.RPCWSURL)
			if err != nil {
				slog.Warn("websocket RPC unavailable; polling instead", "err", err)
				return
			}
			defer c.Close()
			heads := make(chan *types.Header, 16)
			sub, err := c.SubscribeNewHead(ctx, heads)
			if err != nil {
				slog.Warn("cannot subscribe to new blocks over websocket; polling instead", "err", err)
				return
			}
			defer sub.Unsubscribe()
			w.wsUp.Store(true)
			defer w.wsUp.Store(false)
			slog.Info("subscribed to new blocks over websocket")
			for {
				select {
				case <-ctx.Done():
					return
				case err := <-sub.Err():
					slog.Warn("websocket subscription ended", "err", err)
					return
				case <-heads:
					w.lastAt.Store(time.Now().UnixNano())
					select {
					case w.kick <- struct{}{}:
					default:
					}
				}
			}
		}()
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (w *Watcher) filter(ctx context.Context, from, to uint64, addrs []common.Address, topics [][]common.Hash) ([]types.Log, error) {
	var out []types.Log
	for s := from; s <= to; s += logChunk {
		e := s + logChunk - 1
		if e > to {
			e = to
		}
		logs, err := w.ch.Eth.FilterLogs(ctx, ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(s), ToBlock: new(big.Int).SetUint64(e), Addresses: addrs, Topics: topics})
		if err != nil {
			return nil, err
		}
		out = append(out, logs...)
	}
	return out, nil
}

type effects struct {
	ownExit map[string]*big.Int
	s6      map[string][]signals.S6Event
}

func (w *Watcher) iterate(ctx context.Context) error {
	w.procMu.Lock()
	defer w.procMu.Unlock()
	hdr, err := w.ch.Eth.HeaderByNumber(ctx, nil)
	if err != nil {
		return err
	}
	n := hdr.Number.Uint64()
	if n+1 < w.next {
		// The chain went backwards (anvil revert): start over from here.
		slog.Warn("chain head moved backwards; resetting watcher state", "head", n, "next", w.next)
		w.next = n + 1
		w.last.Store(n)
		w.resetStates()
		if err := w.LoadGuards(ctx); err != nil {
			return err
		}
		return nil
	}
	if n < w.next {
		return nil // nothing new
	}
	fx, err := w.readLogs(ctx, hdr, w.next, n)
	if err != nil {
		return fmt.Errorf("cannot read logs: %w", err)
	}
	w.exec.OnBlock(hdr)
	w.checkAll(ctx, hdr, fx)
	w.next = n + 1
	w.last.Store(n)
	if err := w.st.SetMeta(ctx, "cursor", fmt.Sprint(w.next)); err != nil {
		slog.Warn("cannot save block cursor", "err", err)
	}
	return nil
}

func (w *Watcher) readLogs(ctx context.Context, head *types.Header, from, to uint64) (*effects, error) {
	fx := &effects{ownExit: map[string]*big.Int{}, s6: map[string][]signals.S6Event{}}
	guards := w.guardList()
	addrs := []common.Address{w.factory}
	for _, g := range guards {
		addrs = append(addrs, g.Addr)
	}
	logs, err := w.filter(ctx, from, to, addrs, nil)
	if err != nil {
		return nil, err
	}
	// New guards discovered in this range: their own events in the same range.
	var fresh []common.Address
	for _, l := range logs {
		if l.Address != w.factory {
			continue
		}
		d, err := chain.DecodeFactory(l)
		if err != nil || d.Name != "GuardCreated" {
			continue
		}
		gi := guardInfo{Addr: d.Addr("guard"), Owner: d.Addr("owner"), CreatedBlock: l.BlockNumber}
		if w.addGuard(gi) {
			fresh = append(fresh, gi.Addr)
		}
	}
	if len(fresh) > 0 {
		more, err := w.filter(ctx, from, to, fresh, nil)
		if err != nil {
			return nil, err
		}
		logs = append(logs, more...)
	}
	// Risky config changes on watched contracts (S6).
	watch := map[common.Address][]struct{ target, label string }{}
	var waddrs []common.Address
	for i := range w.targets.Targets {
		t := &w.targets.Targets[i]
		for _, wc := range t.Signals.WatchContracts {
			a := common.HexToAddress(wc.Address)
			if _, ok := watch[a]; !ok {
				waddrs = append(waddrs, a)
			}
			watch[a] = append(watch[a], struct{ target, label string }{t.ID, wc.Label})
		}
	}
	var s6logs []types.Log
	if len(waddrs) > 0 {
		s6logs, err = w.filter(ctx, from, to, waddrs, [][]common.Hash{w.s6Topics})
		if err != nil {
			return nil, err
		}
	}
	sort.SliceStable(logs, func(i, j int) bool {
		if logs[i].BlockNumber != logs[j].BlockNumber {
			return logs[i].BlockNumber < logs[j].BlockNumber
		}
		return logs[i].Index < logs[j].Index
	})
	for _, l := range logs {
		if l.Address == w.factory {
			w.handleFactoryLog(ctx, l)
		} else {
			w.handleGuardLog(ctx, l, fx)
		}
	}
	window := time.Duration(w.sig.S6.CritComboWindowSec) * time.Second
	if w.backfill {
		s6logs = nil
		w.backfill = false
	}
	for _, l := range s6logs {
		// A config change from long ago (seen while catching up) is history, not a live signal.
		if l.BlockNumber != head.Number.Uint64() {
			bh, err := w.ch.Eth.HeaderByNumber(ctx, new(big.Int).SetUint64(l.BlockNumber))
			if err != nil {
				return nil, err
			}
			if chain.HeaderTime(head).Sub(chain.HeaderTime(bh)) > window {
				continue
			}
		}
		name := w.s6Names[l.Topics[0]]
		for _, wt := range watch[l.Address] {
			fx.s6[wt.target] = append(fx.s6[wt.target], signals.S6Event{Name: name, Label: wt.label, Block: l.BlockNumber})
			tid := wt.target
			w.em.Emit(ctx, store.Event{Kind: "alert", Block: l.BlockNumber, TargetID: &tid,
				Message: fmt.Sprintf("Config change on %s: %s.", wt.label, strings.ToLower(name)), TxHash: strPtr(l.TxHash.Hex())})
		}
	}
	return fx, nil
}

func strPtr(s string) *string { return &s }

func (w *Watcher) handleFactoryLog(ctx context.Context, l types.Log) {
	d, err := chain.DecodeFactory(l)
	if err != nil || d.Name != "GuardCreated" {
		return
	}
	owner, guard := d.Addr("owner"), d.Addr("guard")
	if err := w.st.UpsertGuard(ctx, store.Guard{Address: guard.Hex(), Owner: owner.Hex(), CreatedBlock: l.BlockNumber}); err != nil {
		slog.Error("cannot store guard", "err", err)
	}
	_ = w.st.EnsureUser(ctx, owner.Hex())
	g := guard.Hex()
	w.em.Emit(ctx, store.Event{Kind: "guard", Block: l.BlockNumber, Guard: &g, Owner: owner.Hex(), TxHash: strPtr(l.TxHash.Hex()),
		Message: "Your Guard was created. Only you own it; Heimdall can only send money back to you."})
}

func (w *Watcher) targetFor(addr common.Address, posType uint8) *config.Target {
	for i := range w.targets.Targets {
		t := &w.targets.Targets[i]
		if t.Addr() == addr && t.PositionType() == posType {
			return t
		}
	}
	return nil
}

func (w *Watcher) handleGuardLog(ctx context.Context, l types.Log, fx *effects) {
	d, err := chain.DecodeGuard(l)
	if err != nil {
		return
	}
	gi, ok := w.Guard(l.Address)
	if !ok {
		return
	}
	g := gi.Addr.Hex()
	owner := gi.Owner.Hex()
	tx := l.TxHash.Hex()
	ev := store.Event{Kind: "guard", Block: l.BlockNumber, Guard: &g, Owner: owner, TxHash: &tx}
	switch d.Name {
	case "Deposited", "Withdrawn":
		t := w.targetFor(d.Addr("target"), d.Uint8("positionType"))
		if t == nil {
			return
		}
		ev.TargetID = &t.ID
		amt := w.positionAmountText(ctx, t, d.Big("amount"), l.BlockNumber)
		if d.Name == "Deposited" {
			ev.Message = fmt.Sprintf("Moved %s into your Guard. Heimdall now watches it.", amt)
		} else {
			ev.Message = fmt.Sprintf("You took %s back out of your Guard.", amt)
		}
		w.em.Emit(ctx, ev)
	case "KeeperToggled":
		if d.Bool("enabled") {
			ev.Message = "You turned Heimdall's keeper on for your Guard."
		} else {
			ev.Message = "You turned Heimdall's keeper off for your Guard. Heimdall will not exit for you until you turn it back on."
		}
		w.em.Emit(ctx, ev)
	case "Paused":
		if d.Bool("paused") {
			ev.Message = "You paused keeper exits for your Guard."
		} else {
			ev.Message = "You resumed keeper exits for your Guard."
		}
		w.em.Emit(ctx, ev)
	case "Exited", "ExitDeferred":
		t := w.targetFor(d.Addr("target"), d.Uint8("positionType"))
		if t == nil {
			return
		}
		if d.Name == "Exited" {
			cur := fx.ownExit[t.ID]
			if cur == nil {
				cur = new(big.Int)
			}
			fx.ownExit[t.ID] = cur.Add(cur, d.Big("amountOut")) // W4: S1 adds this back
		}
		w.exec.RecordExternal(t, gi.Addr, gi.Owner, d)
	}
}

// positionAmountText renders a position-token amount as underlying value, e.g. "10,000 USDC".
func (w *Watcher) positionAmountText(ctx context.Context, t *config.Target, amt *big.Int, blk uint64) string {
	assets := amt
	if !t.IsAave() {
		if v, err := w.ch.Uint(ctx, &chain.ERC4626ABI, t.Addr(), nil, "convertToAssets", amt); err == nil {
			assets = v
		} else {
			return fmt.Sprintf("%s vault shares", amt)
		}
	}
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(assets), new(big.Float).SetInt(chain.Pow10(t.AssetDecimals))).Float64()
	return signals.FormatAmount(f, t.AssetSymbol)
}

// ---- checks ----

func (w *Watcher) checkAll(ctx context.Context, hdr *types.Header, fx *effects) {
	positions := w.readPositions(ctx, hdr)
	var wg sync.WaitGroup
	w.mu.RLock()
	states := make([]*targetState, 0, len(w.states))
	for _, ts := range w.states {
		states = append(states, ts)
	}
	w.mu.RUnlock()
	for _, ts := range states {
		wg.Add(1)
		go func(ts *targetState) {
			defer wg.Done()
			w.checkTarget(ctx, ts, hdr, fx, positions[ts.t.ID])
		}(ts)
	}
	wg.Wait()
}

func (w *Watcher) checkTarget(ctx context.Context, ts *targetState, hdr *types.Header, fx *effects, pos []executor.Position) {
	obs, ok := w.observe(ctx, ts, hdr, fx, pos)
	if !ok {
		return
	}
	res := ts.eng.Step(obs)
	decidedAt := time.Now()
	blk := hdr.Number.Uint64()
	prev := ts.severity
	changed := res.Severity != prev
	if changed {
		ts.severity = res.Severity
		if res.Severity != signals.SevWatch {
			ts.episode = fmt.Sprintf("%s:%d:%s", ts.t.ID, blk, res.Severity)
		}
	}
	// Decision first, bookkeeping after: the exit must not wait on database writes (W5).
	if res.Severity != signals.SevWatch {
		w.exec.Trigger(executor.Trigger{Target: ts.t, Severity: res.Severity, Reason: res.Reason, Episode: ts.episode,
			DecidedAt: decidedAt, Header: hdr, Positions: pos})
	}
	latest := Latest{TargetID: ts.t.ID, Severity: res.Severity, Reason: res.Reason, UpdatedAt: time.Now().UTC(), Block: blk, Signals: res.Signals}
	w.mu.Lock()
	ts.latest = latest
	w.mu.Unlock()

	if changed {
		w.recordChange(ctx, ts, prev, res, blk, pos)
	}
	if time.Since(ts.lastPush) >= pushEvery || changed {
		ts.lastPush = time.Now()
		w.hub.Publish("signals", latest)
	}
	levels := levelSig(res.Signals)
	if changed || levels != ts.lastLevels || time.Since(ts.lastSnap) >= snapshotEvery {
		ts.lastSnap, ts.lastLevels = time.Now(), levels
		type pt struct {
			ID    string  `json:"id"`
			Level string  `json:"level"`
			Value float64 `json:"value"`
		}
		var pts []pt
		for _, s := range res.Signals {
			pts = append(pts, pt{s.ID, s.Level, s.Value})
		}
		raw, _ := json.Marshal(pts)
		if err := w.st.InsertSnapshot(ctx, ts.t.ID, store.Snapshot{Block: blk, Time: res.Time, Severity: res.Severity, Signals: raw}); err != nil {
			slog.Warn("cannot store signal snapshot", "err", err)
		}
	}
	if time.Since(ts.lastCheck) >= checkEventEvery {
		ts.lastCheck = time.Now()
		tid := ts.t.ID
		w.em.Emit(ctx, store.Event{Kind: "check", Block: blk, TargetID: &tid, Message: fmt.Sprintf("Watching 6 signals on %s: severity %s.", ts.t.Label, res.Severity)})
	}
}

func levelSig(ss []signals.Signal) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(s.Level[:1])
	}
	return b.String()
}

// recordChange stores a severity change with its inputs and a plain-English reason (specs W3),
// pushes it to the UI and alerts the owners of guarded positions.
func (w *Watcher) recordChange(ctx context.Context, ts *targetState, prev string, res signals.Result, blk uint64, pos []executor.Position) {
	inputs, _ := json.Marshal(res.Inputs)
	if _, err := w.st.InsertIncident(ctx, store.Incident{TargetID: ts.t.ID, Block: blk, Time: res.Time, FromSeverity: prev, Severity: res.Severity, Reason: res.Reason, Inputs: inputs}); err != nil {
		slog.Error("cannot store incident", "err", err)
	}
	tid, sev := ts.t.ID, res.Severity
	w.em.Emit(ctx, store.Event{Kind: "severity", Block: blk, TargetID: &tid, Severity: &sev,
		Message: fmt.Sprintf("%s: %s -> %s. %s", ts.t.Label, title(prev), title(res.Severity), res.Reason)})
	slog.Info("severity changed", "target", ts.t.ID, "from", prev, "to", res.Severity, "reason", res.Reason)
	if res.Severity == signals.SevWatch || w.em.Notifier == nil {
		return
	}
	for _, p := range pos {
		if p.Held == nil || p.Held.Sign() == 0 {
			continue
		}
		pl, err := w.pol.For(ctx, p.Guard.Hex(), p.Owner.Hex(), ts.t.ID)
		if err != nil {
			continue
		}
		var action string
		switch policy.Decide(pl, res.Severity) {
		case policy.ExitFull:
			action = "Heimdall is exiting your position now."
		case policy.ExitHalf:
			action = "Heimdall is exiting half of your position now."
		case policy.AskFirst:
			action = "Your policy asks you first: open Heimdall and press Exit now."
		default:
			action = "No automatic exit: your policy only notifies you at this level."
		}
		w.em.Notifier.Enqueue(notify.Message{Owner: p.Owner.Hex(), Kind: "severity_" + res.Severity,
			Subject: fmt.Sprintf("Heimdall: %s risk on %s", title(res.Severity), ts.t.Label),
			Body:    res.Reason + "\n\n" + action})
	}
}
