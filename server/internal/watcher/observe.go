package watcher

import (
	"context"
	"log/slog"
	"math/big"
	"strings"
	"sync"

	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/executor"
	"github.com/AlphaTechini/heimdall/server/internal/signals"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// readPositions reads every known guard's holding of every target at the block.
func (w *Watcher) readPositions(ctx context.Context, hdr *types.Header) map[string][]executor.Position {
	blk := hdr.Number
	guards := w.guardList()
	out := map[string][]executor.Position{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, g := range guards {
		wg.Add(1)
		go func(g guardInfo) {
			defer wg.Done()
			var enabled, paused bool
			var err error
			if enabled, err = w.ch.Bool(ctx, &chain.GuardABI, g.Addr, blk, "keeperEnabled"); err != nil {
				slog.Debug("cannot read keeperEnabled", "guard", g.Addr, "err", err)
				return
			}
			if paused, err = w.ch.Bool(ctx, &chain.GuardABI, g.Addr, blk, "paused"); err != nil {
				slog.Debug("cannot read paused", "guard", g.Addr, "err", err)
				return
			}
			for i := range w.targets.Targets {
				t := &w.targets.Targets[i]
				held, err := w.ch.Uint(ctx, &chain.GuardABI, g.Addr, blk, "held", t.PositionType(), t.Addr())
				if err != nil {
					slog.Debug("cannot read guard position", "guard", g.Addr, "target", t.ID, "err", err)
					continue
				}
				mu.Lock()
				out[t.ID] = append(out[t.ID], executor.Position{Guard: g.Addr, Owner: g.Owner, Held: held, KeeperEnabled: enabled, Paused: paused})
				mu.Unlock()
			}
		}(g)
	}
	wg.Wait()
	return out
}

func (w *Watcher) warnOnce(ts *targetState, key string, err error) {
	if ts.readWarned[key] {
		return
	}
	ts.readWarned[key] = true
	slog.Warn("cannot read signal input; that signal reports unavailable until it works", "target", ts.t.ID, "input", key, "err", err)
}

// observe reads the target's state at the block and assembles the engine's observation.
// A failed read leaves that input nil, so the matching signal reports "unavailable"
// instead of a made-up value. ok is false when nothing at all could be read.
func (w *Watcher) observe(ctx context.Context, ts *targetState, hdr *types.Header, fx *effects, pos []executor.Position) (signals.Obs, bool) {
	t := ts.t
	blk := hdr.Number
	obs := signals.Obs{Block: blk.Uint64(), Time: chain.HeaderTime(hdr), S6: fx.s6[t.ID]}
	obs.OwnExitOut = fx.ownExit[t.ID]
	if obs.OwnExitOut == nil {
		obs.OwnExitOut = new(big.Int)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var total, liqAvail *big.Int
	run := func(name string, f func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f(); err != nil {
				mu.Lock()
				w.warnOnce(ts, name, err)
				mu.Unlock()
			}
		}()
	}

	if t.IsAave() {
		aToken := common.HexToAddress(t.AToken)
		run("reserveCash", func() error {
			v, err := w.ch.BalanceOf(ctx, t.AssetAddr(), aToken, blk)
			if err == nil {
				mu.Lock()
				obs.TotalAssets, liqAvail = v, v
				mu.Unlock()
			}
			return err
		})
		run("aTokenSupply", func() error {
			v, err := w.ch.Uint(ctx, &chain.ERC20ABI, aToken, blk, "totalSupply")
			if err == nil {
				mu.Lock()
				total = v
				mu.Unlock()
			}
			return err
		})
		if w.pool != (common.Address{}) {
			run("liquidityIndex", func() error {
				r, err := w.ch.AaveReserve(ctx, w.pool, t.AssetAddr(), blk)
				if err == nil && r.LiquidityIndex.Sign() > 0 {
					mu.Lock()
					obs.SharePrice = r.LiquidityIndex
					mu.Unlock()
				}
				return err
			})
		}
	} else {
		if ts.vaultDec == 0 {
			d, err := w.ch.Uint(ctx, &chain.ERC20ABI, t.Addr(), blk, "decimals")
			if err != nil {
				w.warnOnce(ts, "vaultDecimals", err)
				ts.vaultDec = t.AssetDecimals
			} else {
				ts.vaultDec = int(d.Int64())
			}
		}
		run("totalAssets", func() error {
			v, err := w.ch.Uint(ctx, &chain.ERC4626ABI, t.Addr(), blk, "totalAssets")
			if err == nil {
				mu.Lock()
				obs.TotalAssets, total = v, v
				mu.Unlock()
			}
			return err
		})
		run("sharePrice", func() error {
			v, err := w.ch.Uint(ctx, &chain.ERC4626ABI, t.Addr(), blk, "convertToAssets", chain.Pow10(ts.vaultDec))
			if err == nil {
				mu.Lock()
				obs.SharePrice = v
				mu.Unlock()
			}
			return err
		})
		if t.Signals.Liquidity.Mode != "exitable" {
			holder := t.Addr()
			if t.Signals.Liquidity.Holder != "" {
				holder = common.HexToAddress(t.Signals.Liquidity.Holder)
			}
			run("liquidity", func() error {
				v, err := w.ch.BalanceOf(ctx, t.AssetAddr(), holder, blk)
				if err == nil {
					mu.Lock()
					liqAvail = v
					mu.Unlock()
				}
				return err
			})
		}
	}

	// S3 / S4 feeds.
	if t.Signals.MarketFeed.Set() && t.Signals.ReferenceFeed.Set() {
		run("marketFeed", func() error {
			p, err := w.ch.LatestPrice(ctx, t.Signals.MarketFeed.Addr(), blk)
			if err == nil {
				f := p.Float()
				mu.Lock()
				obs.MarketPrice = &f
				mu.Unlock()
			}
			return err
		})
		run("referenceFeed", func() error {
			p, err := w.ch.LatestPrice(ctx, t.Signals.ReferenceFeed.Addr(), blk)
			if err == nil {
				f := p.Float()
				mu.Lock()
				obs.RefPrice = &f
				mu.Unlock()
			}
			return err
		})
	}
	for _, cf := range t.Signals.CollateralFeeds {
		if cf.Address == "" {
			continue
		}
		cf := cf
		run("collateral "+cf.Label, func() error {
			p, err := w.ch.LatestPrice(ctx, common.HexToAddress(cf.Address), blk)
			if err == nil {
				mu.Lock()
				obs.Collateral = append(obs.Collateral, signals.NamedPrice{Label: cf.Label, Price: p.Float()})
				mu.Unlock()
			}
			return err
		})
	}

	// S5 in "exitable" mode: what each guard could withdraw right now against its position.
	var liq []signals.LiqSample
	if !t.IsAave() && t.Signals.Liquidity.Mode == "exitable" {
		for _, p := range pos {
			if p.Held == nil || p.Held.Sign() == 0 {
				continue
			}
			p := p
			run("exitable", func() error {
				ex, err := w.ch.Uint(ctx, &chain.GuardABI, p.Guard, blk, "exitable", t.PositionType(), t.Addr())
				if err != nil {
					return err
				}
				exA, err := w.ch.Uint(ctx, &chain.ERC4626ABI, t.Addr(), blk, "convertToAssets", ex)
				if err != nil {
					return err
				}
				posA, err := w.ch.Uint(ctx, &chain.ERC4626ABI, t.Addr(), blk, "convertToAssets", p.Held)
				if err != nil {
					return err
				}
				mu.Lock()
				liq = append(liq, signals.LiqSample{Available: exA, Position: posA})
				mu.Unlock()
				return nil
			})
		}
	}
	// Largest guarded position in underlying units, for the idle-mode S5 comparison.
	var maxPos *big.Int
	if t.Signals.Liquidity.Mode != "exitable" || t.IsAave() {
		for _, p := range pos {
			if p.Held == nil || p.Held.Sign() == 0 {
				continue
			}
			p := p
			run("position", func() error {
				v := p.Held
				if !t.IsAave() {
					var err error
					if v, err = w.ch.Uint(ctx, &chain.ERC4626ABI, t.Addr(), blk, "convertToAssets", p.Held); err != nil {
						return err
					}
				}
				mu.Lock()
				if maxPos == nil || v.Cmp(maxPos) > 0 {
					maxPos = v
				}
				mu.Unlock()
				return nil
			})
		}
	}
	wg.Wait()

	if obs.TotalAssets == nil && obs.SharePrice == nil {
		return obs, false
	}
	if liqAvail != nil {
		liq = append(liq, signals.LiqSample{Available: liqAvail, Position: maxPos})
		if total != nil && total.Sign() > 0 {
			u, _ := new(big.Float).Quo(new(big.Float).SetInt(new(big.Int).Sub(total, liqAvail)), new(big.Float).SetInt(total)).Float64()
			u *= 100
			if u < 0 {
				u = 0
			}
			obs.Utilization = &u
		}
	}
	obs.Liq = liq
	return obs, true
}
