// Package backtest replays the production signal code (package signals) over real historical
// blocks fetched from an archive RPC. It never invents data (specs N7): if a block cannot be read,
// the run fails and nothing is written.
package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/config"
	"github.com/AlphaTechini/heimdall/server/internal/signals"
	"github.com/ethereum/go-ethereum/common"
)

// Params configures a replay.
type Params struct {
	Incident string // id, becomes the file name and the /backtests/{id} path
	Title    string
	RPC      string           // archive RPC URL
	Mode     string           // "erc4626" (default) or "balance"
	Target   common.Address   // erc4626: the vault
	Holder   common.Address   // balance: the contract whose token balances are summed
	Tokens   []common.Address // balance: ERC-20 tokens held by Holder
	Source   string           // where the addresses and block range came from (written into note)
	From, To uint64
	Step     uint64
	Stable   bool // the vault asset is a $1 stablecoin, so the USD floor applies
	Out      string
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Point is one replayed block.
type Point struct {
	Block        uint64            `json:"block"`
	Time         time.Time         `json:"time"`
	TotalAssets  string            `json:"totalAssets"`
	SharePrice   string            `json:"sharePrice"`
	OutflowPct   float64           `json:"outflowPct"`
	ShareDropPct float64           `json:"shareDropPct"`
	Severity     string            `json:"severity"`
	Levels       map[string]string `json:"levels"`
}

// Result is the JSON file served by GET /backtests/{id}.
type Result struct {
	ID                                string                        `json:"id"`
	Title                             string                        `json:"title"`
	Incident                          string                        `json:"incident"`
	ChainID                           uint64                        `json:"chainId"`
	Mode                              string                        `json:"mode"`
	Tokens                            []string                      `json:"tokens"`
	Target                            string                        `json:"target"`
	Asset                             string                        `json:"asset"`
	AssetDecimals                     int                           `json:"assetDecimals"`
	FromBlock                         uint64                        `json:"fromBlock"`
	ToBlock                           uint64                        `json:"toBlock"`
	Step                              uint64                        `json:"step"`
	GeneratedAt                       time.Time                     `json:"generatedAt"`
	RPCHost                           string                        `json:"rpcHost"`
	Thresholds                        map[string]map[string]float64 `json:"thresholds"`
	Note                              string                        `json:"note"`
	Points                            []Point                       `json:"points"`
	FirstWarningBlock                 *uint64                       `json:"firstWarningBlock"`
	FirstCriticalBlock                *uint64                       `json:"firstCriticalBlock"`
	PeakTotalAssets                   string                        `json:"peakTotalAssets"`
	TotalAssetsAtFirstCritical        *string                       `json:"totalAssetsAtFirstCritical"`
	PctOfPeakRemainingAtFirstCritical *float64                      `json:"pctOfPeakRemainingAtFirstCritical"`
}

func retry[T any](f func() (T, error)) (T, error) {
	var zero T
	var err error
	for i := 0; i < 4; i++ {
		var v T
		if v, err = f(); err == nil {
			return v, nil
		}
		time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
	}
	return zero, err
}

// Run replays S1 and S2 for an ERC-4626 vault over [From, To].
func Run(ctx context.Context, p Params, sig *config.Signals) (*Result, error) {
	if !idRe.MatchString(p.Incident) {
		return nil, fmt.Errorf("--incident must be lowercase letters, digits, - or _ (for example tmx-2026-01)")
	}
	if p.To < p.From {
		return nil, fmt.Errorf("--to must not be below --from")
	}
	if p.Step == 0 {
		p.Step = 1
	}
	if p.Mode == "" {
		p.Mode = "erc4626"
	}
	if p.Mode != "erc4626" && p.Mode != "balance" {
		return nil, fmt.Errorf("--mode must be erc4626 or balance")
	}
	ch, err := chain.Dial(ctx, p.RPC, "")
	if err != nil {
		return nil, err
	}
	defer ch.Eth.Close()
	at := new(big.Int).SetUint64(p.From)
	var read func(bn *big.Int) (*big.Int, *big.Int, error) // returns totalAssets (or summed balance) and the share price (nil when unavailable)
	var asset common.Address
	var dec int
	var target common.Address
	var tokens []string
	note := ""
	switch p.Mode {
	case "erc4626":
		vault := p.Target
		target = vault
		asset, err = ch.Addr(ctx, &chain.ERC4626ABI, vault, at, "asset")
		if err != nil {
			return nil, fmt.Errorf("the target does not look like an ERC-4626 vault at block %d (is the RPC an archive node?): %w", p.From, err)
		}
		d, err := ch.Uint(ctx, &chain.ERC20ABI, asset, at, "decimals")
		if err != nil {
			return nil, err
		}
		dec = int(d.Int64())
		vdec, err := ch.Uint(ctx, &chain.ERC20ABI, vault, at, "decimals")
		if err != nil {
			return nil, err
		}
		one := chain.Pow10(int(vdec.Int64()))
		tokens = []string{asset.Hex()}
		read = func(bn *big.Int) (*big.Int, *big.Int, error) {
			ta, err := retry(func() (*big.Int, error) { return ch.Uint(ctx, &chain.ERC4626ABI, vault, bn, "totalAssets") })
			if err != nil {
				return nil, nil, fmt.Errorf("cannot read totalAssets: %w", err)
			}
			sp, err := retry(func() (*big.Int, error) { return ch.Uint(ctx, &chain.ERC4626ABI, vault, bn, "convertToAssets", one) })
			if err != nil {
				return nil, nil, fmt.Errorf("cannot read the share price: %w", err)
			}
			return ta, sp, nil
		}
		note = "Signals S1 (outflow) and S2 (share price) replayed with the production signal code over real historical state. S3-S6 need feeds and logs that this replay does not read."
	case "balance":
		if len(p.Tokens) == 0 {
			return nil, fmt.Errorf("--token needs at least one ERC-20 address in --mode balance")
		}
		target = p.Holder
		asset = p.Tokens[0]
		decs := make([]int, len(p.Tokens))
		for i, t := range p.Tokens {
			d, err := ch.Uint(ctx, &chain.ERC20ABI, t, at, "decimals")
			if err != nil {
				return nil, fmt.Errorf("token %s does not look like an ERC-20 at block %d: %w", t.Hex(), p.From, err)
			}
			decs[i] = int(d.Int64())
			tokens = append(tokens, t.Hex())
		}
		dec = decs[0]
		read = func(bn *big.Int) (*big.Int, *big.Int, error) {
			sum := new(big.Int)
			for i, t := range p.Tokens {
				t := t
				v, err := retry(func() (*big.Int, error) { return ch.BalanceOf(ctx, t, p.Holder, bn) })
				if err != nil {
					return nil, nil, fmt.Errorf("cannot read balanceOf(%s) for token %s: %w", p.Holder.Hex(), t.Hex(), err)
				}
				// bring every token to the first token's decimals before summing
				if decs[i] > dec {
					v = new(big.Int).Div(v, chain.Pow10(decs[i]-dec))
				} else if decs[i] < dec {
					v = new(big.Int).Mul(v, chain.Pow10(dec-decs[i]))
				}
				sum.Add(sum, v)
			}
			return sum, nil, nil
		}
		unit := "in token units of the first listed token (no prices are applied; different tokens are added 1:1)"
		if p.Stable {
			unit = "treating every listed token as $1"
		}
		note = "Balance mode: S1 (outflow) is computed from the summed balanceOf(holder) of the listed tokens " + unit + ", with the production signal code. S2 (share price) is unavailable in this mode. S3-S6 are not replayed."
	}
	if p.Source != "" {
		note += " Source: " + p.Source
	}
	eng := signals.NewEngine(sig, signals.Meta{AssetDecimals: dec, AssetIsStable: p.Stable})
	res := &Result{ID: p.Incident, Title: p.Title, Incident: p.Incident, ChainID: ch.ChainID.Uint64(), Mode: p.Mode, Tokens: tokens, Target: target.Hex(), Asset: asset.Hex(),
		AssetDecimals: dec, FromBlock: p.From, ToBlock: p.To, Step: p.Step, GeneratedAt: time.Now().UTC(),
		Thresholds: signals.Thresholds(sig), Points: []Point{}, Note: note}
	if u, err := url.Parse(p.RPC); err == nil {
		res.RPCHost = u.Host
	}
	if res.Title == "" {
		res.Title = p.Incident
	}
	peak := new(big.Int)
	var atCrit *big.Int
	for b := p.From; b <= p.To; b += p.Step {
		bn := new(big.Int).SetUint64(b)
		hdr, err := retry(func() (*struct{ t uint64 }, error) {
			h, err := ch.Eth.HeaderByNumber(ctx, bn)
			if err != nil {
				return nil, err
			}
			return &struct{ t uint64 }{h.Time}, nil
		})
		if err != nil {
			return nil, fmt.Errorf("cannot read block %d: %w", b, err)
		}
		ta, sp, err := read(bn)
		if err != nil {
			return nil, fmt.Errorf("block %d: %w", b, err)
		}
		r := eng.Step(signals.Obs{Block: b, Time: time.Unix(int64(hdr.t), 0).UTC(), TotalAssets: ta, SharePrice: sp})
		if ta.Cmp(peak) > 0 {
			peak = new(big.Int).Set(ta)
		}
		pt := Point{Block: b, Time: time.Unix(int64(hdr.t), 0).UTC(), TotalAssets: ta.String(), SharePrice: spStr(sp),
			OutflowPct: r.Signals[0].Value, ShareDropPct: r.Signals[1].Value, Severity: r.Severity, Levels: map[string]string{}}
		for _, s := range r.Signals {
			pt.Levels[s.ID] = s.Level
		}
		res.Points = append(res.Points, pt)
		bb := b
		if r.Severity != signals.SevWatch && res.FirstWarningBlock == nil {
			res.FirstWarningBlock = &bb
		}
		if r.Severity == signals.SevCritical && res.FirstCriticalBlock == nil {
			res.FirstCriticalBlock = &bb
			atCrit = new(big.Int).Set(ta)
		}
		if (b-p.From)/p.Step%200 == 0 {
			slog.Info("replaying", "block", b, "of", p.To)
		}
	}
	res.PeakTotalAssets = peak.String()
	if atCrit != nil && peak.Sign() > 0 {
		s := atCrit.String()
		res.TotalAssetsAtFirstCritical = &s
		f, _ := new(big.Float).Quo(new(big.Float).SetInt(atCrit), new(big.Float).SetInt(peak)).Float64()
		f *= 100
		res.PctOfPeakRemainingAtFirstCritical = &f
	}
	return res, nil
}

// Write saves the result (creating the directory).
func Write(res *Result, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, append(raw, '\n'), 0o644)
}

func spStr(v *big.Int) string {
	if v == nil {
		return ""
	}
	return v.String()
}
