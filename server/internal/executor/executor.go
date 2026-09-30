// Package executor turns Critical/Warning decisions into exit transactions signed by the keeper.
//
// Rules (specs W5-W8, N5): one in-flight exit job per (guard, target); a job retries every block
// until the position is empty, the owner stops it, or the timeout passes; the priority tip never
// exceeds the owner's cap; a transaction that is not mined after 3 blocks is replaced with a
// higher tip within the cap; keeper nonces are managed here.
package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/config"
	"github.com/AlphaTechini/heimdall/server/internal/hub"
	"github.com/AlphaTechini/heimdall/server/internal/policy"
	"github.com/AlphaTechini/heimdall/server/internal/signals"
	"github.com/AlphaTechini/heimdall/server/internal/store"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const stuckAfterBlocks = 3

// Position is one guard's holding of a target, read by the watcher.
type Position struct {
	Guard         common.Address
	Owner         common.Address
	Held          *big.Int // position tokens (shares or aTokens)
	KeeperEnabled bool
	Paused        bool
}

// Trigger asks the executor to act on a severity for a target.
type Trigger struct {
	Target    *config.Target
	Severity  string
	Reason    string
	Episode   string
	DecidedAt time.Time
	Header    *types.Header
	Positions []Position
}

type pendingTx struct {
	hash      common.Hash
	nonce     uint64
	gasLimit  uint64
	tip       *big.Int
	maxFee    *big.Int
	sentBlock uint64
	stuckNote bool
}

type job struct {
	mu   sync.Mutex
	busy atomic.Bool
	stop atomic.Bool
	done bool
	gen  uint64

	exit      *store.Exit
	target    *config.Target
	guard     common.Address
	owner     common.Address
	pol       policy.Policy
	severity  string
	reason    string
	reasonH   common.Hash
	decidedAt time.Time
	full      bool
	maxAmount *big.Int // position tokens; MaxUint256 for a full exit
	burned    *big.Int
	totalOut  *big.Int
	deadline  time.Time
	pending   *pendingTx
	attempts  int
	deferNote bool
	partials  int
	startBlk  uint64
	episode   string
}

// Executor submits and tracks exits.
type Executor struct {
	ctx    context.Context
	ch     *chain.Client
	keeper *chain.Keeper
	st     *store.Store
	em     *hub.Emitter
	hub    *hub.Hub
	pol    *policy.Service
	sig    *config.Signals
	feed   *priceFeed

	mu      sync.Mutex
	gen     uint64
	jobs    map[string]*job
	seen    map[string]bool
	knownTx map[common.Hash]bool
	head    *types.Header

	nonceMu sync.Mutex
	nonce   uint64
	nonceOK bool
}

// New creates the executor.
func New(ctx context.Context, ch *chain.Client, keeper *chain.Keeper, st *store.Store, em *hub.Emitter, h *hub.Hub, pol *policy.Service, sig *config.Signals, ethUSD config.AddrSrc) *Executor {
	return &Executor{ctx: ctx, ch: ch, keeper: keeper, st: st, em: em, hub: h, pol: pol, sig: sig,
		feed: newPriceFeed(ch, ethUSD), jobs: map[string]*job{}, seen: map[string]bool{}, knownTx: map[common.Hash]bool{}}
}

func key(guard common.Address, target string) string {
	return strings.ToLower(guard.Hex()) + "|" + target
}

// Active reports whether an exit job is running for the position.
func (x *Executor) Active(guard common.Address, target string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.jobs[key(guard, target)] != nil
}

// KnownTx reports whether the keeper broadcast this transaction hash.
func (x *Executor) KnownTx(h common.Hash) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.knownTx[h]
}

// Stop asks a running job to stop retrying. It reports whether a job existed.
func (x *Executor) Stop(guard common.Address, target string) bool {
	x.mu.Lock()
	j := x.jobs[key(guard, target)]
	x.mu.Unlock()
	if j == nil {
		return false
	}
	j.stop.Store(true)
	if j.busy.CompareAndSwap(false, true) {
		go func() {
			defer j.busy.Store(false)
			x.step(j, x.currentHead())
		}()
	}
	return true
}

// Reset forgets all jobs and re-reads the nonce (after the simulator reverted the chain).
func (x *Executor) Reset() {
	x.mu.Lock()
	x.gen++
	x.jobs = map[string]*job{}
	x.seen = map[string]bool{}
	x.knownTx = map[common.Hash]bool{}
	x.mu.Unlock()
	x.nonceMu.Lock()
	x.nonceOK = false
	x.nonceMu.Unlock()
}

func (x *Executor) currentHead() *types.Header {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.head
}

// OnBlock advances every job by one block: it checks receipts and retries partial exits.
func (x *Executor) OnBlock(h *types.Header) {
	x.mu.Lock()
	x.head = h
	jobs := make([]*job, 0, len(x.jobs))
	for _, j := range x.jobs {
		jobs = append(jobs, j)
	}
	x.mu.Unlock()
	for _, j := range jobs {
		j := j
		if j.busy.CompareAndSwap(false, true) {
			go func() {
				defer j.busy.Store(false)
				x.step(j, h)
			}()
		}
	}
}

// Trigger evaluates each guard's policy for the severity and starts exit jobs.
func (x *Executor) Trigger(tr Trigger) {
	t := tr.Target
	for _, p := range tr.Positions {
		if p.Held == nil || p.Held.Sign() == 0 {
			continue
		}
		k := key(p.Guard, t.ID)
		ek := k + "|" + tr.Episode
		x.mu.Lock()
		if x.seen[ek] || x.jobs[k] != nil {
			x.mu.Unlock()
			continue
		}
		x.seen[ek] = true
		gen := x.gen
		x.mu.Unlock()

		pol, err := x.pol.For(x.ctx, p.Guard.Hex(), p.Owner.Hex(), t.ID)
		if err != nil {
			slog.Error("cannot read policy; not exiting this position automatically", "guard", p.Guard, "err", err)
			continue
		}
		act := policy.Decide(pol, tr.Severity)
		ev := store.Event{Block: tr.Header.Number.Uint64(), TargetID: &t.ID, Owner: p.Owner.Hex()}
		g := p.Guard.Hex()
		ev.Guard = &g
		sev := tr.Severity
		ev.Severity = &sev
		switch act {
		case policy.None, policy.Notify:
			continue
		case policy.AskFirst:
			ev.Kind = "alert"
			ev.Message = fmt.Sprintf("Critical risk on %s. Your policy asks you first: open Heimdall and press Exit now.", t.Label)
			x.em.Emit(x.ctx, ev)
			continue
		}
		if !p.KeeperEnabled || p.Paused {
			ev.Kind = "exit_failed"
			why := "the keeper is turned off"
			if p.Paused {
				why = "keeper exits are paused"
			}
			ev.Message = fmt.Sprintf("Heimdall did not exit %s: %s for your Guard. You can still exit yourself at any time.", t.Label, why)
			x.em.Emit(x.ctx, ev)
			continue
		}
		j := &job{
			gen: gen, target: t, guard: p.Guard, owner: p.Owner, pol: pol, severity: tr.Severity, reason: tr.Reason,
			reasonH: crypto.Keccak256Hash([]byte(tr.Reason)), decidedAt: tr.DecidedAt, full: act == policy.ExitFull,
			burned: new(big.Int), totalOut: new(big.Int),
			deadline: time.Now().Add(time.Duration(x.sig.ExitRetryTimeoutSec) * time.Second),
			startBlk: tr.Header.Number.Uint64(), episode: tr.Episode, maxAmount: new(big.Int).Set(maxUint256),
		}
		if act == policy.ExitHalf {
			j.maxAmount = new(big.Int).Rsh(p.Held, 1)
			if j.maxAmount.Sign() == 0 {
				continue
			}
		}
		x.mu.Lock()
		if x.jobs[k] != nil {
			x.mu.Unlock()
			continue
		}
		x.jobs[k] = j
		x.mu.Unlock()
		j.busy.Store(true)
		go func(hdr *types.Header) {
			defer j.busy.Store(false)
			x.step(j, hdr)
		}(tr.Header)
	}
}

var maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

func (x *Executor) event(j *job, kind, msg string, blk uint64, tx string) {
	t, g, s := j.target.ID, j.guard.Hex(), j.severity
	e := store.Event{Kind: kind, Block: blk, TargetID: &t, Guard: &g, Severity: &s, Message: msg, Owner: j.owner.Hex()}
	if tx != "" {
		e.TxHash = &tx
	}
	if j.exit != nil {
		id := j.exit.ID
		e.ExitID = &id
	}
	x.em.Emit(x.ctx, e)
}

func (x *Executor) publish(j *job) {
	if j.exit == nil {
		return
	}
	e, err := x.st.GetExit(x.ctx, j.exit.ID)
	if err != nil {
		slog.Error("cannot load exit for publishing", "err", err)
		return
	}
	x.hub.Publish("exit", e)
}

func (x *Executor) alive(j *job) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return j.gen == x.gen
}

func (x *Executor) remove(j *job) {
	x.mu.Lock()
	if x.jobs[key(j.guard, j.target.ID)] == j {
		delete(x.jobs, key(j.guard, j.target.ID))
	}
	x.mu.Unlock()
}

// finish closes a job with a final status.
func (x *Executor) finish(j *job, status, msg string, blk uint64) {
	j.done = true
	x.remove(j)
	if !x.alive(j) {
		return
	}
	if j.exit != nil {
		j.exit.Status = status
		b := blk
		j.exit.EndBlock = &b
		j.exit.TotalOut = j.totalOut.String()
		if err := x.st.UpdateExit(x.ctx, j.exit); err != nil {
			slog.Error("cannot update exit", "err", err)
		}
	}
	if msg != "" {
		x.event(j, "exit_failed", msg, blk, "")
	}
	x.publish(j)
}

func (x *Executor) step(j *job, hdr *types.Header) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.done || hdr == nil {
		return
	}
	if !x.alive(j) {
		j.done = true
		return
	}
	head := hdr.Number.Uint64()
	if j.stop.Load() {
		x.finish(j, "stopped", "Retrying stopped at your request. Anything left stays in your Guard; you can exit yourself at any time.", head)
		return
	}
	if time.Now().After(j.deadline) {
		x.finish(j, "timeout", fmt.Sprintf("Stopped retrying after %d minutes. Anything left stays in your Guard; you can exit yourself at any time.", x.sig.ExitRetryTimeoutSec/60), head)
		return
	}
	if j.pending != nil {
		processed := x.checkPending(j, head)
		if j.done {
			return
		}
		if !processed {
			x.maybeReplace(j, hdr)
			return
		}
	}
	if j.attempts > 0 {
		held, err := x.held(j)
		if err != nil {
			slog.Warn("cannot read guard balance; will retry next block", "err", err)
			return
		}
		if held.Sign() == 0 {
			x.finish(j, "complete", "", head)
			return
		}
		ex, err := x.exitable(j)
		if err != nil {
			slog.Warn("cannot read exitable amount; will retry next block", "err", err)
			return
		}
		if ex.Sign() == 0 {
			if !j.deferNote {
				j.deferNote = true
				x.event(j, "exit_deferred", "Nothing can be withdrawn right now (the vault has no free cash). Retrying every block.", head, "")
			}
			return
		}
	}
	x.send(j, hdr)
}

func (x *Executor) held(j *job) (*big.Int, error) {
	return x.ch.Uint(x.ctx, &chain.GuardABI, j.guard, nil, "held", j.target.PositionType(), j.target.Addr())
}

func (x *Executor) exitable(j *job) (*big.Int, error) {
	return x.ch.Uint(x.ctx, &chain.GuardABI, j.guard, nil, "exitable", j.target.PositionType(), j.target.Addr())
}

func (x *Executor) baseFee(hdr *types.Header) (*big.Int, error) {
	if hdr.BaseFee != nil {
		return hdr.BaseFee, nil
	}
	return x.ch.Eth.SuggestGasPrice(x.ctx)
}

func (x *Executor) send(j *job, hdr *types.Header) {
	head := hdr.Number.Uint64()
	first := j.exit == nil
	txMax := new(big.Int).Set(j.maxAmount)
	if !j.full {
		txMax.Sub(j.maxAmount, j.burned)
		if txMax.Sign() <= 0 {
			x.finish(j, "complete", "", head)
			return
		}
	}
	data, err := chain.PackExit(j.target.PositionType(), j.target.Addr(), txMax, j.reasonH)
	if err != nil {
		x.fail(j, head, "Could not build the exit transaction: "+err.Error())
		return
	}
	guard := j.guard
	gas, err := x.ch.Eth.EstimateGas(x.ctx, ethereum.CallMsg{From: x.keeper.Address, To: &guard, Data: data})
	if err != nil {
		if chain.IsRevert(err) {
			x.fail(j, head, "The Guard refused the exit (the keeper may have been turned off or paused): "+shortErr(err))
			return
		}
		slog.Warn("gas estimate failed; using a fixed limit", "err", err)
		gas = 500_000
	}
	gasLimit := gas * 13 / 10
	plan := planTip(x.ctx, x.feed, x.sig, j.severity, j.pol.TipCapUSD, j.pol.PriorityExit, gasLimit)
	bf, err := x.baseFee(hdr)
	if err != nil {
		x.fail(j, head, "Cannot read the current gas price: "+shortErr(err))
		return
	}
	maxFee := new(big.Int).Add(new(big.Int).Mul(bf, big.NewInt(2)), plan.tipPerGas)

	x.nonceMu.Lock()
	nonce, err := x.nextNonceLocked()
	if err != nil {
		x.nonceMu.Unlock()
		x.fail(j, head, "Cannot read the keeper's nonce: "+shortErr(err))
		return
	}
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: x.ch.ChainID, Nonce: nonce, GasTipCap: plan.tipPerGas, GasFeeCap: maxFee, Gas: gasLimit, To: &guard, Data: data})
	signed, err := x.keeper.Sign(tx)
	if err != nil {
		x.nonceMu.Unlock()
		x.fail(j, head, "Cannot sign the exit transaction: "+shortErr(err))
		return
	}
	x.mu.Lock()
	x.knownTx[signed.Hash()] = true
	x.mu.Unlock()
	err = x.ch.Eth.SendTransaction(x.ctx, signed)
	sentAt := time.Now()
	if err != nil {
		x.nonceOK = false
	} else {
		x.nonce = nonce + 1
	}
	x.nonceMu.Unlock()
	j.attempts++
	if err != nil {
		x.fail(j, head, "The exit transaction could not be sent: "+shortErr(err))
		return
	}
	ms := int(sentAt.Sub(j.decidedAt).Milliseconds())
	tipUSD := 0.0
	if plan.tipPerGas.Sign() > 0 {
		tipUSD = weiToUSD(new(big.Int).Mul(plan.tipPerGas, new(big.Int).SetUint64(gasLimit)), plan.eth)
	}
	if first {
		j.exit = &store.Exit{Guard: j.guard.Hex(), Owner: j.owner.Hex(), TargetID: j.target.ID, Trigger: "auto", Reason: j.reason,
			ReasonHash: j.reasonH.Hex(), Severity: j.severity, Status: "active", Episode: j.episode, DecidedAt: j.decidedAt,
			DecisionToBroadcastMs: &ms, TipCapUSD: j.pol.TipCapUSD, TipNote: plan.note, TotalOut: "0", StartBlock: head}
		if err := x.st.InsertExit(x.ctx, j.exit); err != nil {
			slog.Error("cannot store exit", "err", err)
		}
	}
	j.pending = &pendingTx{hash: signed.Hash(), nonce: nonce, gasLimit: gasLimit, tip: plan.tipPerGas, maxFee: maxFee, sentBlock: head}
	if err := x.st.InsertExitTx(x.ctx, j.exit.ID, store.ExitTx{Hash: signed.Hash().Hex(), Nonce: nonce, SentBlock: head,
		MaxPriorityFeePerGasWei: plan.tipPerGas.String(), MaxFeePerGasWei: maxFee.String(), GasLimit: gasLimit, TipUSD: tipUSD, Result: "pending"}); err != nil {
		slog.Error("cannot store exit transaction", "err", err)
	}
	if first {
		msg := fmt.Sprintf("Exit submitted for %s: priority tip up to $%.2f (your cap is $%.2f), sent in block %d.", j.target.Label, tipUSD, j.pol.TipCapUSD, head)
		if plan.tipPerGas.Sign() == 0 {
			msg = fmt.Sprintf("Exit submitted for %s in block %d (no priority tip: %s).", j.target.Label, head, plan.note)
		}
		x.event(j, "exit_submitted", msg, head, signed.Hash().Hex())
		slog.Info("exit broadcast", "guard", j.guard, "target", j.target.ID, "decisionToBroadcastMs", ms, "tipPerGasWei", plan.tipPerGas)
	}
	x.publish(j)
}

func (x *Executor) nextNonceLocked() (uint64, error) {
	if !x.nonceOK {
		n, err := x.ch.Eth.PendingNonceAt(x.ctx, x.keeper.Address)
		if err != nil {
			return 0, err
		}
		x.nonce, x.nonceOK = n, true
	}
	return x.nonce, nil
}

// fail records a failed job (before or after the first broadcast).
func (x *Executor) fail(j *job, blk uint64, msg string) {
	slog.Warn("exit failed", "guard", j.guard, "target", j.target.ID, "reason", msg)
	if j.exit == nil {
		ms := int(time.Since(j.decidedAt).Milliseconds())
		j.exit = &store.Exit{Guard: j.guard.Hex(), Owner: j.owner.Hex(), TargetID: j.target.ID, Trigger: "auto", Reason: j.reason,
			ReasonHash: j.reasonH.Hex(), Severity: j.severity, Status: "failed", Episode: j.episode, DecidedAt: j.decidedAt, DecisionToBroadcastMs: &ms,
			TipCapUSD: j.pol.TipCapUSD, TotalOut: "0", StartBlock: blk}
		if err := x.st.InsertExit(x.ctx, j.exit); err != nil {
			slog.Error("cannot store exit", "err", err)
		}
	}
	if j.attempts <= 1 && j.totalOut.Sign() == 0 {
		x.finish(j, "failed", msg+" You can still exit yourself at any time.", blk)
		return
	}
	// A retry failed after earlier progress: report it and keep trying on the next block.
	x.event(j, "exit_failed", msg+" Will try again next block.", blk, "")
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// checkPending reads the receipt of the in-flight transaction. It returns true when the
// transaction was mined and processed.
func (x *Executor) checkPending(j *job, head uint64) bool {
	p := j.pending
	rc, err := x.ch.Eth.TransactionReceipt(x.ctx, p.hash)
	if err != nil {
		if !errors.Is(err, ethereum.NotFound) {
			slog.Warn("cannot read receipt", "tx", p.hash, "err", err)
		}
		return false
	}
	blk := rc.BlockNumber.Uint64()
	j.pending = nil
	hashS := p.hash.Hex()
	tipUSD := 0.0
	if p.tip.Sign() > 0 {
		if eth, err := x.feed.get(x.ctx); err == nil {
			tipUSD = weiToUSD(new(big.Int).Mul(p.tip, new(big.Int).SetUint64(rc.GasUsed)), eth)
		}
	}
	upd := store.ExitTx{Hash: hashS, Block: &blk, TipUSD: tipUSD, AmountOut: "0", Burned: "0", Remaining: "0", Result: "reverted"}
	if rc.Status != 1 {
		if err := x.st.UpdateExitTx(x.ctx, upd); err != nil {
			slog.Error("cannot update exit transaction", "err", err)
		}
		x.fail(j, blk, "The exit transaction was rejected by the Guard (the keeper may have been turned off or paused).")
		return true
	}
	var exited *chain.Decoded
	for _, l := range rc.Logs {
		if l.Address != j.guard {
			continue
		}
		d, err := chain.DecodeGuard(*l)
		if err != nil || d.Addr("target") != j.target.Addr() {
			continue
		}
		switch d.Name {
		case "Exited":
			exited = d
		}
	}
	sym, dec := j.target.AssetSymbol, j.target.AssetDecimals
	amt := func(v *big.Int) string { return signals.FormatAmount(toFloat(v, dec), sym) }
	switch {
	case exited != nil:
		out, burned, rem := exited.Big("amountOut"), exited.Big("burned"), exited.Big("remaining")
		j.totalOut.Add(j.totalOut, out)
		j.burned.Add(j.burned, burned)
		j.deferNote = false
		upd.Result, upd.AmountOut, upd.Burned, upd.Remaining = "exited", out.String(), burned.String(), rem.String()
		if err := x.st.UpdateExitTx(x.ctx, upd); err != nil {
			slog.Error("cannot update exit transaction", "err", err)
		}
		j.exit.TotalOut = j.totalOut.String()
		complete := rem.Sign() == 0 || (!j.full && j.burned.Cmp(j.maxAmount) >= 0)
		if complete {
			msg := fmt.Sprintf("Exit complete: %s returned to your wallet.", amt(j.totalOut))
			if j.partials > 0 {
				msg = fmt.Sprintf("Remaining %s withdrawn at block %d. Exit complete: %s returned to your wallet.", amt(out), blk, amt(j.totalOut))
			}
			x.finish(j, "complete", "", blk)
			x.event(j, "exit_complete", msg, blk, hashS)
			return true
		}
		j.partials++
		j.deferNote = true // the partial message already says we keep retrying
		x.event(j, "exit_partial", fmt.Sprintf("Partial exit: %s withdrawn (all available). Retrying for the rest every block.", amt(out)), blk, hashS)
		if err := x.st.UpdateExit(x.ctx, j.exit); err != nil {
			slog.Error("cannot update exit", "err", err)
		}
		x.publish(j)
	default:
		upd.Result = "deferred"
		if err := x.st.UpdateExitTx(x.ctx, upd); err != nil {
			slog.Error("cannot update exit transaction", "err", err)
		}
		if !j.deferNote {
			j.deferNote = true
			x.event(j, "exit_deferred", "Nothing could be withdrawn yet (the vault has no free cash). Retrying every block.", blk, hashS)
		}
		x.publish(j)
	}
	return true
}

func toFloat(v *big.Int, dec int) float64 {
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(v), new(big.Float).SetInt(chain.Pow10(dec))).Float64()
	return f
}

// maybeReplace bumps the tip of a transaction that has not been mined after 3 blocks, within
// the owner's cap (specs W8).
func (x *Executor) maybeReplace(j *job, hdr *types.Header) {
	p := j.pending
	head := hdr.Number.Uint64()
	if p == nil || head < p.sentBlock+stuckAfterBlocks {
		return
	}
	plan := planTip(x.ctx, x.feed, x.sig, signals.SevCritical, j.pol.TipCapUSD, j.pol.PriorityExit, p.gasLimit)
	minTip := new(big.Int).Add(new(big.Int).Div(new(big.Int).Mul(p.tip, big.NewInt(11)), big.NewInt(10)), big.NewInt(1))
	if plan.capPerGas.Cmp(minTip) < 0 {
		if !p.stuckNote {
			p.stuckNote = true
			x.event(j, "exit_submitted", "The exit transaction is waiting: the priority tip is already at your cap, so it cannot be raised further.", head, p.hash.Hex())
		}
		return
	}
	newTip := new(big.Int).Add(new(big.Int).Div(new(big.Int).Mul(p.tip, big.NewInt(13)), big.NewInt(10)), big.NewInt(1))
	if newTip.Cmp(plan.capPerGas) > 0 {
		newTip = new(big.Int).Set(plan.capPerGas)
	}
	bf, err := x.baseFee(hdr)
	if err != nil {
		return
	}
	maxFee := new(big.Int).Add(new(big.Int).Mul(bf, big.NewInt(2)), newTip)
	if floor := new(big.Int).Add(new(big.Int).Div(new(big.Int).Mul(p.maxFee, big.NewInt(11)), big.NewInt(10)), big.NewInt(1)); maxFee.Cmp(floor) < 0 {
		maxFee = floor
	}
	data, err := chain.PackExit(j.target.PositionType(), j.target.Addr(), func() *big.Int {
		if j.full {
			return new(big.Int).Set(maxUint256)
		}
		return new(big.Int).Sub(j.maxAmount, j.burned)
	}(), j.reasonH)
	if err != nil {
		return
	}
	guard := j.guard
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: x.ch.ChainID, Nonce: p.nonce, GasTipCap: newTip, GasFeeCap: maxFee, Gas: p.gasLimit, To: &guard, Data: data})
	signed, err := x.keeper.Sign(tx)
	if err != nil {
		return
	}
	x.mu.Lock()
	x.knownTx[signed.Hash()] = true
	x.mu.Unlock()
	if err := x.ch.Eth.SendTransaction(x.ctx, signed); err != nil {
		slog.Warn("replacement transaction was not accepted", "err", err)
		return
	}
	if err := x.st.UpdateExitTx(x.ctx, store.ExitTx{Hash: p.hash.Hex(), AmountOut: "0", Burned: "0", Remaining: "0", Result: "replaced"}); err != nil {
		slog.Error("cannot update exit transaction", "err", err)
	}
	tipUSD := 0.0
	if plan.eth.Answer != nil {
		tipUSD = weiToUSD(new(big.Int).Mul(newTip, new(big.Int).SetUint64(p.gasLimit)), plan.eth)
	}
	if err := x.st.InsertExitTx(x.ctx, j.exit.ID, store.ExitTx{Hash: signed.Hash().Hex(), Nonce: p.nonce, SentBlock: head,
		MaxPriorityFeePerGasWei: newTip.String(), MaxFeePerGasWei: maxFee.String(), GasLimit: p.gasLimit, TipUSD: tipUSD, Result: "pending"}); err != nil {
		slog.Error("cannot store replacement transaction", "err", err)
	}
	x.event(j, "exit_submitted", fmt.Sprintf("The exit transaction was not mined after %d blocks; replaced it with a higher priority tip ($%.2f, cap $%.2f).", stuckAfterBlocks, tipUSD, j.pol.TipCapUSD), head, signed.Hash().Hex())
	j.pending = &pendingTx{hash: signed.Hash(), nonce: p.nonce, gasLimit: p.gasLimit, tip: newTip, maxFee: maxFee, sentBlock: head}
	x.publish(j)
}

// RecordExternal stores an exit that the executor did not start: one the owner ran themselves
// (trigger "owner") or a keeper transaction from an earlier run (trigger "manual_keeper").
func (x *Executor) RecordExternal(t *config.Target, guard, owner common.Address, d *chain.Decoded) {
	txh := d.Log.TxHash
	if x.KnownTx(txh) {
		return
	}
	if id, err := x.st.ExitIDByTx(x.ctx, txh.Hex()); err != nil || id != 0 {
		return
	}
	caller := d.Addr("caller")
	trigger, reason := "manual_keeper", "Exit started by the Heimdall keeper"
	if caller == owner {
		trigger, reason = "owner", "Exit started by you"
	}
	blk := d.Log.BlockNumber
	now := time.Now().UTC()
	out, burned, rem := "0", "0", "0"
	result := "deferred"
	if d.Name == "Exited" {
		out, burned, rem, result = d.Big("amountOut").String(), d.Big("burned").String(), d.Big("remaining").String(), "exited"
	}
	e := &store.Exit{Guard: guard.Hex(), Owner: owner.Hex(), TargetID: t.ID, Trigger: trigger, Reason: reason,
		ReasonHash: d.Hash("reasonHash").Hex(), Severity: "watch", Status: "complete", DecidedAt: now, TipCapUSD: 0, TotalOut: out, StartBlock: blk}
	e.EndBlock = &blk
	if err := x.st.InsertExit(x.ctx, e); err != nil {
		slog.Error("cannot store external exit", "err", err)
		return
	}
	if err := x.st.InsertExitTx(x.ctx, e.ID, store.ExitTx{Hash: txh.Hex(), SentBlock: blk, MaxPriorityFeePerGasWei: "0", MaxFeePerGasWei: "0", Result: result}); err != nil {
		slog.Error("cannot store external exit transaction", "err", err)
	}
	if err := x.st.UpdateExitTx(x.ctx, store.ExitTx{Hash: txh.Hex(), Block: &blk, AmountOut: out, Burned: burned, Remaining: rem, Result: result}); err != nil {
		slog.Error("cannot update external exit transaction", "err", err)
	}
	amt := signals.FormatAmount(toFloat(d.Big("amountOut"), t.AssetDecimals), t.AssetSymbol)
	who := "You exited"
	if trigger != "owner" {
		who = "The Heimdall keeper exited"
	}
	kind, msg := "exit_complete", fmt.Sprintf("%s %s from %s yourself. It went to your wallet.", who, amt, t.Label)
	if trigger != "owner" {
		msg = fmt.Sprintf("%s %s from %s. It went to your wallet.", who, amt, t.Label)
	}
	if d.Name != "Exited" {
		kind, msg = "exit_deferred", fmt.Sprintf("An exit on %s found nothing to withdraw yet (the vault has no free cash).", t.Label)
	}
	g, tid, txs := guard.Hex(), t.ID, txh.Hex()
	x.em.Emit(x.ctx, store.Event{Kind: kind, Block: blk, TargetID: &tid, Guard: &g, Message: msg, TxHash: &txs, ExitID: &e.ID, Owner: owner.Hex()})
	if ex, err := x.st.GetExit(x.ctx, e.ID); err == nil {
		x.hub.Publish("exit", ex)
	}
}
