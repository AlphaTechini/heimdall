package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/config"
	"github.com/AlphaTechini/heimdall/server/internal/policy"
	"github.com/AlphaTechini/heimdall/server/internal/signals"
	"github.com/AlphaTechini/heimdall/server/internal/store"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := a.ctx(r)
	defer cancel()
	n, err := a.Chain.Eth.BlockNumber(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "The blockchain node is not answering."})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "chainId": a.Chain.ChainID.Uint64(), "block": n})
}

// onFork reports whether the live chain is a local chain that differs from the chain the targets
// file describes (the demo fork: chain id 31337 running with the Arbitrum One targets file).
func (a *API) onFork() bool {
	return a.Chain.ChainID.Uint64() != a.Targets.ChainID
}

func (a *API) chainName() string {
	if a.Chain.ChainID.Uint64() == 31337 {
		if a.onFork() {
			return "Local Arbitrum One fork"
		}
		for _, t := range a.Targets.Targets {
			if t.Sim.MockVault {
				return "Local development chain (test mocks)"
			}
		}
		return "Local Arbitrum One fork"
	}
	switch a.Chain.ChainID.Uint64() {
	case 42161:
		return "Arbitrum One"
	case 421614:
		return "Arbitrum Sepolia"
	}
	return fmt.Sprintf("Chain %s", a.Chain.ChainID)
}

func (a *API) telegramEnabled() bool {
	return a.Telegram != nil && a.Telegram.Enabled() && a.Telegram.Bot != ""
}
func (a *API) emailEnabled() bool { return a.Email != nil && a.Email.Enabled() }

func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	type tgt struct {
		ID            string `json:"id"`
		Label         string `json:"label"`
		Protocol      string `json:"protocol"`
		Type          string `json:"type"`
		Address       string `json:"address"`
		PositionToken string `json:"positionToken"`
		Asset         string `json:"asset"`
		AssetSymbol   string `json:"assetSymbol"`
		AssetDecimals int    `json:"assetDecimals"`
	}
	ts := []tgt{}
	for i := range a.Targets.Targets {
		t := &a.Targets.Targets[i]
		ts = append(ts, tgt{t.ID, t.Label, t.Protocol, t.Type, t.Addr().Hex(), t.PositionToken().Hex(), t.AssetAddr().Hex(), t.AssetSymbol, t.AssetDecimals})
	}
	var bot any
	if a.telegramEnabled() {
		bot = a.Telegram.Bot
	}
	var explorer any
	if a.Targets.Explorer != nil && !a.onFork() { // fork transactions do not exist on Arbiscan
		explorer = a.Targets.Explorer
	}
	writeJSON(w, 200, map[string]any{
		"chainId": a.Chain.ChainID.Uint64(), "chainName": a.chainName(), "demoMode": a.Sim != nil && a.Sim.Enabled(),
		"factory": a.Factory.Hex(), "explorer": explorer,
		"telegramEnabled": a.telegramEnabled(), "telegramBot": bot, "emailEnabled": a.emailEnabled(),
		"defaultTipCapUsd": a.Env.DefaultTipCapUSD, "targets": ts, "signals": signals.Catalog,
	})
}

func (a *API) signalsLatest(w http.ResponseWriter, r *http.Request) {
	t := a.target(w, r.PathValue("targetId"))
	if t == nil {
		return
	}
	l, _ := a.Watcher.Latest(t.ID)
	writeJSON(w, 200, l)
}

func (a *API) signalsHistory(w http.ResponseWriter, r *http.Request) {
	t := a.target(w, r.PathValue("targetId"))
	if t == nil {
		return
	}
	limit := clampInt(r.URL.Query().Get("limit"), 200, 1, 2000)
	ctx, cancel := a.ctx(r)
	defer cancel()
	pts, err := a.Store.Snapshots(ctx, t.ID, limit)
	if err != nil {
		a.dbErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"points": pts, "thresholds": signals.Thresholds(a.Signals)})
}

func clampInt(s string, def, lo, hi int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func (a *API) guard(w http.ResponseWriter, r *http.Request) {
	ga, ok := parseAddr(r.PathValue("guard"))
	if !ok {
		writeErr(w, 400, "That is not a valid Guard address.")
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	owner, err := a.guardOwner(r, ga)
	if err != nil {
		if errors.Is(err, errNotFound) {
			writeErr(w, 404, "That address is not a Heimdall Guard.")
			return
		}
		a.rpcErr(w, err)
		return
	}
	enabled, err1 := a.Chain.Bool(ctx, &chain.GuardABI, ga, nil, "keeperEnabled")
	paused, err2 := a.Chain.Bool(ctx, &chain.GuardABI, ga, nil, "paused")
	if err1 != nil || err2 != nil {
		a.rpcErr(w, errors.Join(err1, err2))
		return
	}
	var created uint64
	if g, err := a.Store.GetGuard(ctx, ga.Hex()); err == nil {
		created = g.CreatedBlock
	}
	writeJSON(w, 200, map[string]any{"guard": ga.Hex(), "owner": owner.Hex(), "keeperEnabled": enabled, "paused": paused, "createdBlock": created})
}

// guardOwner returns the on-chain owner of a Heimdall guard, registering the guard if it is new
// to us. errNotFound means the address is not a guard created by the factory.
func (a *API) guardOwner(r *http.Request, ga common.Address) (common.Address, error) {
	ctx, cancel := a.ctx(r)
	defer cancel()
	ok, err := a.Chain.Bool(ctx, &chain.FactoryABI, a.Factory, nil, "isGuard", ga)
	if err != nil {
		return common.Address{}, err
	}
	if !ok {
		return common.Address{}, errNotFound
	}
	owner, err := a.Chain.Addr(ctx, &chain.GuardABI, ga, nil, "owner")
	if err != nil {
		return common.Address{}, err
	}
	if _, _, err := a.Watcher.EnsureGuard(ctx, owner); err != nil {
		return owner, err
	}
	return owner, nil
}

func (a *API) activity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	owner := ""
	if s := q.Get("address"); s != "" {
		ad, ok := parseAddr(s)
		if !ok {
			writeErr(w, 400, "Enter a valid wallet address (0x...).")
			return
		}
		owner = ad.Hex()
	}
	target := q.Get("targetId")
	if target != "" && a.Targets.Target(target) == nil {
		writeErr(w, 404, fmt.Sprintf("Unknown position %q.", target))
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	evs, err := a.Store.Events(ctx, owner, target, clampInt(q.Get("limit"), 50, 1, 200))
	if err != nil {
		a.dbErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"events": evs})
}

func (a *API) exit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, 400, "That is not a valid exit id.")
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	e, err := a.Store.GetExit(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "No exit with that id.")
		return
	}
	if err != nil {
		a.dbErr(w, err)
		return
	}
	writeJSON(w, 200, e)
}

func (a *API) exits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	guard := ""
	if s := q.Get("guard"); s != "" {
		ga, ok := parseAddr(s)
		if !ok {
			writeErr(w, 400, "That is not a valid Guard address.")
			return
		}
		guard = ga.Hex()
	}
	target := q.Get("targetId")
	if target != "" && a.Targets.Target(target) == nil {
		writeErr(w, 404, fmt.Sprintf("Unknown position %q.", target))
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	list, err := a.Store.ListExits(ctx, guard, target, clampInt(q.Get("limit"), 50, 1, 200))
	if err != nil {
		a.dbErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"exits": list})
}

func jsonArgs(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		switch x := v.(type) {
		case *big.Int:
			out[k] = x.String()
		case common.Address:
			out[k] = x.Hex()
		case [32]byte:
			out[k] = common.Hash(x).Hex()
		case []byte:
			out[k] = "0x" + common.Bytes2Hex(x)
		default:
			out[k] = x
		}
	}
	return out
}

func (a *API) tx(w http.ResponseWriter, r *http.Request) {
	hs := r.PathValue("hash")
	if len(hs) != 66 || !strings.HasPrefix(hs, "0x") {
		writeErr(w, 400, "That is not a valid transaction hash.")
		return
	}
	h := common.HexToHash(hs)
	ctx, cancel := a.ctx(r)
	defer cancel()
	tx, pending, err := a.Chain.Eth.TransactionByHash(ctx, h)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			writeErr(w, 404, "Transaction not found on this chain.")
			return
		}
		a.rpcErr(w, err)
		return
	}
	if pending {
		writeErr(w, 404, "That transaction has not been mined yet.")
		return
	}
	rc, err := a.Chain.Eth.TransactionReceipt(ctx, h)
	if err != nil {
		a.rpcErr(w, err)
		return
	}
	from, _ := types.Sender(types.LatestSignerForChainID(a.Chain.ChainID), tx)
	status := "success"
	if rc.Status != 1 {
		status = "reverted"
	}
	logs := []map[string]any{}
	for _, l := range rc.Logs {
		var name any
		var args any
		if l.Address == a.Factory {
			if d, err := chain.DecodeFactory(*l); err == nil {
				name, args = d.Name, jsonArgs(d.Args)
			}
		} else if _, ok := a.Watcher.Guard(l.Address); ok {
			if d, err := chain.DecodeGuard(*l); err == nil {
				name, args = d.Name, jsonArgs(d.Args)
			}
		}
		logs = append(logs, map[string]any{"address": l.Address.Hex(), "name": name, "args": args})
	}
	var to any
	if tx.To() != nil {
		to = tx.To().Hex()
	}
	eff := "0"
	if rc.EffectiveGasPrice != nil {
		eff = rc.EffectiveGasPrice.String()
	}
	writeJSON(w, 200, map[string]any{
		"hash": h.Hex(), "blockNumber": rc.BlockNumber.Uint64(), "from": from.Hex(), "to": to, "status": status,
		"gasUsed": strconv.FormatUint(rc.GasUsed, 10), "effectiveGasPrice": eff, "maxPriorityFeePerGas": tx.GasTipCap().String(), "logs": logs,
	})
}

// ---- positions ----

type position struct {
	TargetID              string         `json:"targetId"`
	Status                string         `json:"status"`
	WalletAmount          string         `json:"walletAmount"`
	WalletPositionTokens  string         `json:"walletPositionTokens"`
	GuardedAmount         string         `json:"guardedAmount"`
	GuardedPositionTokens string         `json:"guardedPositionTokens"`
	ExitableAmount        string         `json:"exitableAmount"`
	ReturnedAmount        string         `json:"returnedAmount"`
	Severity              string         `json:"severity"`
	Policy                *policy.Policy `json:"policy"`
	LastExit              *store.Exit    `json:"lastExit"`
}

// toAssets converts position tokens to underlying units (ERC-4626 convertToAssets; Aave is 1:1).
func (a *API) toAssets(r *http.Request, t *config.Target, tokens *big.Int) (*big.Int, error) {
	if t.IsAave() || tokens.Sign() == 0 {
		return tokens, nil
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	return a.Chain.Uint(ctx, &chain.ERC4626ABI, t.Addr(), nil, "convertToAssets", tokens)
}

func (a *API) positions(w http.ResponseWriter, r *http.Request) {
	addr, ok := parseAddr(r.URL.Query().Get("address"))
	if !ok {
		writeErr(w, 400, "Enter a valid wallet address (0x...).")
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	predicted, err := a.Chain.Addr(ctx, &chain.FactoryABI, a.Factory, nil, "predictGuard", addr)
	if err != nil {
		a.rpcErr(w, err)
		return
	}
	guard, exists, err := a.Watcher.EnsureGuard(ctx, addr)
	if err != nil {
		a.rpcErr(w, err)
		return
	}
	resp := map[string]any{"address": addr.Hex(), "guardPredicted": predicted.Hex(), "guard": nil, "keeperEnabled": false, "paused": false}
	if exists {
		resp["guard"] = guard.Hex()
		en, e1 := a.Chain.Bool(ctx, &chain.GuardABI, guard, nil, "keeperEnabled")
		pa, e2 := a.Chain.Bool(ctx, &chain.GuardABI, guard, nil, "paused")
		if e1 != nil || e2 != nil {
			a.rpcErr(w, errors.Join(e1, e2))
			return
		}
		resp["keeperEnabled"], resp["paused"] = en, pa
	}
	out := []position{}
	for i := range a.Targets.Targets {
		t := &a.Targets.Targets[i]
		p := position{TargetID: t.ID, Severity: a.Watcher.Severity(t.ID), WalletAmount: "0", WalletPositionTokens: "0",
			GuardedAmount: "0", GuardedPositionTokens: "0", ExitableAmount: "0", ReturnedAmount: "0"}
		wt, err := a.Chain.BalanceOf(ctx, t.PositionToken(), addr, nil)
		if err != nil {
			a.rpcErr(w, err)
			return
		}
		wa, err := a.toAssets(r, t, wt)
		if err != nil {
			a.rpcErr(w, err)
			return
		}
		p.WalletPositionTokens, p.WalletAmount = wt.String(), wa.String()
		gt, guardedAssets := new(big.Int), new(big.Int)
		if exists {
			gt, err = a.Chain.Uint(ctx, &chain.GuardABI, guard, nil, "held", t.PositionType(), t.Addr())
			if err != nil {
				a.rpcErr(w, err)
				return
			}
			ga, err := a.toAssets(r, t, gt)
			if err != nil {
				a.rpcErr(w, err)
				return
			}
			p.GuardedPositionTokens, p.GuardedAmount = gt.String(), ga.String()
			guardedAssets = ga
			ex, err := a.Chain.Uint(ctx, &chain.GuardABI, guard, nil, "exitable", t.PositionType(), t.Addr())
			if err != nil {
				a.rpcErr(w, err)
				return
			}
			exa, err := a.toAssets(r, t, ex)
			if err != nil {
				a.rpcErr(w, err)
				return
			}
			p.ExitableAmount = exa.String()
			if sum, err := a.Store.SumReturned(ctx, guard.Hex(), t.ID); err == nil {
				p.ReturnedAmount = sum.String()
			}
			if le, err := a.Store.LastExit(ctx, guard.Hex(), t.ID); err == nil {
				p.LastExit = le
			}
			if pol, err := a.Policy.Stored(ctx, guard.Hex(), t.ID); err == nil {
				p.Policy = pol
			}
		}
		switch {
		case exists && a.Executor.Active(guard, t.ID):
			p.Status = "exiting"
		// Shares worth 0 of the asset are rounding dust a vault leaves after a full exit.
		case gt.Sign() > 0 && (guardedAssets.Sign() > 0 || p.LastExit == nil):
			p.Status = "guarded"
		case p.LastExit != nil && p.LastExit.Status != "failed":
			p.Status = "exited"
		default:
			p.Status = "unprotected"
		}
		if wt.Sign() == 0 && gt.Sign() == 0 && p.LastExit == nil {
			continue
		}
		out = append(out, p)
	}
	resp["positions"] = out
	writeJSON(w, 200, resp)
}

// ---- backtests ----

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (a *API) backtestDir() string {
	if a.Env.BacktestsDir != "" {
		return a.Env.BacktestsDir
	}
	return filepath.Join(filepath.Dir(a.Env.TargetsFile), "backtests")
}

func (a *API) backtests(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	files, _ := filepath.Glob(filepath.Join(a.backtestDir(), "*.json"))
	sort.Strings(files)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(f), ".json")
		if !idRe.MatchString(id) {
			continue
		}
		out = append(out, map[string]any{"id": id, "title": m["title"], "incident": m["incident"], "fromBlock": m["fromBlock"], "toBlock": m["toBlock"], "generatedAt": m["generatedAt"]})
	}
	writeJSON(w, 200, map[string]any{"backtests": out})
}

func (a *API) backtest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !idRe.MatchString(id) {
		writeErr(w, 404, "No backtest with that id.")
		return
	}
	raw, err := os.ReadFile(filepath.Join(a.backtestDir(), id+".json"))
	if err != nil {
		writeErr(w, 404, "No backtest data for that incident yet.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}
