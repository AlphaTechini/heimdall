// Package signals computes the six risk signals S1-S6 and the per-target severity
// (details.md §8.3, specs W1-W4). It is pure: an Engine is fed a sequence of observations
// (Obs) and returns a Result for each. The live watcher and the backtest replay both use it.
package signals

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/config"
)

// Signal levels.
const (
	LevelOK          = "ok"
	LevelWarning     = "warning"
	LevelCritical    = "critical"
	LevelUnavailable = "unavailable"
)

// Severities (per target).
const (
	SevWatch    = "watch"
	SevWarning  = "warning"
	SevCritical = "critical"
)

// Info is the label and tooltip of a signal (docs/api.md §4; the web shows these exact texts).
type Info struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Tooltip string `json:"tooltip"`
}

// Catalog lists the signals in display order.
var Catalog = []Info{
	{"S1", "Outflow", "How fast money is leaving the vault."},
	{"S2", "Share price", "Vault shares should only gain value. A drop means a loss inside the vault."},
	{"S3", "Oracle", "Whether the price the protocol uses matches an independent price."},
	{"S4", "Collateral peg", "Whether stablecoins backing the loans still trade near $1."},
	{"S5", "Liquidity", "Whether there is enough cash in the vault for you to leave."},
	{"S6", "Config changes", "Admin, owner, upgrade or pause changes on the protocol."},
}

// Meta describes the target for formatting and the USD floor.
type Meta struct {
	Noun          string // "Vault" (default) or "Aave reserve": used in S1's sentence
	Label         string
	AssetSymbol   string
	AssetDecimals int
	AssetIsStable bool
}

// NamedPrice is a collateral price in USD.
type NamedPrice struct {
	Label string
	Price float64
}

// LiqSample is available exit liquidity against a position size, both in asset base units.
type LiqSample struct {
	Available *big.Int
	Position  *big.Int
}

// S6Event is a risky config change seen on a watched contract.
type S6Event struct {
	Name  string // e.g. OwnershipTransferred
	Label string // watched contract label
	Block uint64
}

// Obs is everything the engine needs from one check. Nil pointers mean "not available".
type Obs struct {
	Block uint64
	Time  time.Time

	TotalAssets *big.Int // S1 base: totalAssets() (ERC-4626) or reserve cash (Aave), base units
	OwnExitOut  *big.Int // amountOut of Heimdall's own exits since the previous check (S1 add-back, W4)
	SharePrice  *big.Int // S2: convertToAssets(1 share) or Aave liquidityIndex

	MarketPrice *float64 // S3
	RefPrice    *float64
	Collateral  []NamedPrice // S4

	Liq         []LiqSample // S5
	Utilization *float64    // S5, percent (nil when unknown)

	S6 []S6Event // config changes since the previous check
}

// Signal is one signal's state (docs/api.md §5).
type Signal struct {
	ID     string  `json:"id"`
	Level  string  `json:"level"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Detail string  `json:"detail"`
}

// Result is one check's outcome.
type Result struct {
	Block    uint64
	Time     time.Time
	Signals  []Signal
	Severity string
	Reason   string
	Inputs   map[string]any
}

type point struct {
	t time.Time
	v float64
}

// Engine keeps the rolling state for one target.
type Engine struct {
	cfg  *config.Signals
	meta Meta

	assets   []point // S1: totalAssets plus own exits added back, whole asset units
	cumOwn   float64
	prices   []point // S2
	crit     map[string]int
	lastWarn map[string]time.Time
	lastS6   time.Time
	s6Recent []S6Event
}

// NewEngine creates an engine for a target.
func NewEngine(cfg *config.Signals, meta Meta) *Engine {
	return &Engine{cfg: cfg, meta: meta, crit: map[string]int{}, lastWarn: map[string]time.Time{}}
}

func units(x *big.Int, dec int) float64 {
	if x == nil {
		return 0
	}
	f, _ := new(big.Float).Quo(new(big.Float).SetInt(x), new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dec)), nil))).Float64()
	return f
}

// FormatAmount renders 6200.5 as "6,200.5 USDC" (up to 2 decimals).
func FormatAmount(v float64, symbol string) string {
	s := fmt.Sprintf("%.2f", v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	whole, frac, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(whole, "-")
	whole = strings.TrimPrefix(whole, "-")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	if symbol != "" {
		out += " " + symbol
	}
	return out
}

func pct(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", v), "0"), ".")
}

func maxSince(pts []point, since time.Time) float64 {
	m := 0.0
	for _, p := range pts {
		if !p.t.Before(since) && p.v > m {
			m = p.v
		}
	}
	return m
}

func prune(pts []point, since time.Time) []point {
	i := 0
	for i < len(pts) && pts[i].t.Before(since) {
		i++
	}
	return pts[i:]
}

// Thresholds returns the numbers the UI draws as lines (docs/api.md history "thresholds").
func Thresholds(c *config.Signals) map[string]map[string]float64 {
	return map[string]map[string]float64{
		"S1": {"warning": c.S1.WarnPct, "critical": c.S1.CritPct},
		"S2": {"critical": c.S2.CritDropPct},
		"S3": {"warning": c.S3.WarnPct, "critical": c.S3.CritPct},
		"S4": {"warning": c.S4.WarnBelow, "critical": c.S4.CritBelow},
		"S5": {"warning": c.S5.WarnUtilizationPct},
	}
}

func rank(l string) int {
	switch l {
	case LevelCritical:
		return 2
	case LevelWarning:
		return 1
	}
	return 0
}

// Step feeds one observation and returns the signals and the target severity.
func (e *Engine) Step(o Obs) Result {
	c := e.cfg
	now := o.Time
	dec := e.meta.AssetDecimals
	sym := e.meta.AssetSymbol
	raw := map[string]Signal{}
	inputs := map[string]any{"block": o.Block}

	// ---- S1 outflow ----
	s1 := Signal{ID: "S1", Level: LevelUnavailable, Unit: fmt.Sprintf("%%/%ds", c.S1.WindowSec), Detail: "Outflow could not be read on this check."}
	if o.TotalAssets != nil {
		if o.OwnExitOut != nil {
			e.cumOwn += units(o.OwnExitOut, dec)
		}
		adj := units(o.TotalAssets, dec) + e.cumOwn
		e.assets = append(e.assets, point{now, adj})
		e.assets = prune(e.assets, now.Add(-time.Duration(c.S1.LongWindowSec)*time.Second))
		drop := func(win int) (float64, float64) {
			m := maxSince(e.assets, now.Add(-time.Duration(win)*time.Second))
			if m <= 0 || adj >= m {
				return 0, 0
			}
			abs := m - adj
			if e.meta.AssetIsStable && abs < c.S1.USDFloor {
				return 0, abs // below the USD floor: ignored
			}
			return abs / m * 100, abs
		}
		sp, sabs := drop(c.S1.WindowSec)
		lp, labs := drop(c.S1.LongWindowSec)
		inputs["S1"] = map[string]any{"assetsNow": adj, "dropShortPct": sp, "dropLongPct": lp, "ownExitsAddedBack": e.cumOwn}
		s1.Level, s1.Value = LevelOK, sp
		switch {
		case sp >= c.S1.CritPct:
			s1.Level = LevelCritical
		case lp >= c.S1.CritLongPct:
			s1.Level, s1.Value, s1.Unit = LevelCritical, lp, fmt.Sprintf("%%/%ds", c.S1.LongWindowSec)
		case sp >= c.S1.WarnPct:
			s1.Level = LevelWarning
		}
		switch s1.Level {
		case LevelOK:
			s1.Detail = fmt.Sprintf("Outflow is normal: %s%% of assets left in %ds.", pct(sp), c.S1.WindowSec)
		default:
			win, p, abs, thr := c.S1.WindowSec, sp, sabs, c.S1.WarnPct
			if s1.Level == LevelCritical {
				thr = c.S1.CritPct
				if sp < c.S1.CritPct {
					win, p, abs, thr = c.S1.LongWindowSec, lp, labs, c.S1.CritLongPct
				}
			}
			word := "Warning"
			if s1.Level == LevelCritical {
				word = "Critical"
			}
			noun := e.meta.Noun
			if noun == "" {
				noun = "Vault"
			}
			s1.Detail = fmt.Sprintf("%s lost %s%% of assets (%s) in %ds (%s at %s%%)", noun, pct(p), FormatAmount(abs, sym), win, word, pct(thr))
		}
	}
	raw["S1"] = s1

	// ---- S2 share price ----
	s2 := Signal{ID: "S2", Level: LevelUnavailable, Unit: "%", Detail: "Share price could not be read on this check."}
	if o.SharePrice != nil && o.SharePrice.Sign() > 0 {
		p := units(o.SharePrice, dec)
		e.prices = append(e.prices, point{now, p})
		e.prices = prune(e.prices, now.Add(-time.Duration(c.S1.LongWindowSec)*time.Second))
		m := maxSince(e.prices, time.Time{})
		d := 0.0
		if m > 0 && p < m {
			d = (m - p) / m * 100
		}
		inputs["S2"] = map[string]any{"price": p, "recentHigh": m, "dropPct": d}
		s2.Level, s2.Value = LevelOK, d
		s2.Detail = "Share price is steady."
		if d >= c.S2.CritDropPct {
			s2.Level = LevelCritical
			s2.Detail = fmt.Sprintf("Share price fell %s%% (Critical at %s%%): the vault lost value.", pct2(d), pct2(c.S2.CritDropPct))
		}
	}
	raw["S2"] = s2

	// ---- S3 oracle ----
	s3 := Signal{ID: "S3", Level: LevelUnavailable, Unit: "%", Detail: "No price feed is configured for this vault, so the oracle check is off."}
	if o.MarketPrice != nil && o.RefPrice != nil && *o.RefPrice > 0 {
		dev := math.Abs(*o.MarketPrice-*o.RefPrice) / *o.RefPrice * 100
		inputs["S3"] = map[string]any{"market": *o.MarketPrice, "reference": *o.RefPrice, "deviationPct": dev}
		s3.Level, s3.Value = LevelOK, dev
		s3.Detail = fmt.Sprintf("Protocol price differs from the reference by %s%%.", pct(dev))
		switch {
		case dev >= c.S3.CritPct:
			s3.Level = LevelCritical
			s3.Detail = fmt.Sprintf("Protocol price is %s%% away from the independent price (Critical at %s%%).", pct(dev), pct(c.S3.CritPct))
		case dev >= c.S3.WarnPct:
			s3.Level = LevelWarning
			s3.Detail = fmt.Sprintf("Protocol price is %s%% away from the independent price (Warning at %s%%).", pct(dev), pct(c.S3.WarnPct))
		}
	}
	raw["S3"] = s3

	// ---- S4 collateral peg ----
	s4 := Signal{ID: "S4", Level: LevelUnavailable, Unit: "USD", Detail: "No collateral price feed is configured for this vault, so the peg check is off."}
	if len(o.Collateral) > 0 {
		low := o.Collateral[0]
		for _, cp := range o.Collateral[1:] {
			if cp.Price < low.Price {
				low = cp
			}
		}
		inputs["S4"] = map[string]any{"lowest": low.Label, "price": low.Price}
		s4.Level, s4.Value = LevelOK, low.Price
		s4.Detail = fmt.Sprintf("%s trades at $%.4f.", low.Label, low.Price)
		switch {
		case low.Price < c.S4.CritBelow:
			s4.Level = LevelCritical
			s4.Detail = fmt.Sprintf("%s trades at $%.4f, far below $1 (Critical below $%.3f).", low.Label, low.Price, c.S4.CritBelow)
		case low.Price < c.S4.WarnBelow:
			s4.Level = LevelWarning
			s4.Detail = fmt.Sprintf("%s trades at $%.4f, below its peg (Warning below $%.3f).", low.Label, low.Price, c.S4.WarnBelow)
		}
	}
	raw["S4"] = s4

	// Debounce (W2): every Critical except S2 must be confirmed on debounceChecks consecutive checks.
	eff := map[string]Signal{}
	base := map[string]string{} // details without the "Confirming" note, for one-sentence reasons
	for _, id := range []string{"S1", "S2", "S3", "S4"} {
		s := raw[id]
		base[id] = s.Detail
		if s.Level == LevelCritical && id != "S2" {
			e.crit[id]++
			if e.crit[id] < c.DebounceChecks {
				s.Level = LevelWarning
				s.Detail += fmt.Sprintf(" Confirming (%d of %d checks).", e.crit[id], c.DebounceChecks)
			}
		} else {
			e.crit[id] = 0
		}
		eff[id] = s
	}
	for _, id := range []string{"S1", "S3", "S4"} {
		if rank(eff[id].Level) >= 1 {
			e.lastWarn[id] = now
		}
	}

	// ---- S5 liquidity ----
	s5 := Signal{ID: "S5", Level: LevelUnavailable, Unit: "% used", Detail: "Liquidity could not be read on this check."}
	if len(o.Liq) > 0 || o.Utilization != nil {
		s5.Level = LevelOK
		s5.Detail = "There is enough cash in the vault to exit."
		if o.Utilization != nil {
			s5.Value = *o.Utilization
			inputs["S5"] = map[string]any{"utilizationPct": *o.Utilization}
		}
		var why []string
		if o.Utilization != nil && *o.Utilization >= c.S5.WarnUtilizationPct {
			why = append(why, fmt.Sprintf("%s%% of the vault's money is in use (Warning at %s%%)", pct(*o.Utilization), pct(c.S5.WarnUtilizationPct)))
		}
		for _, l := range o.Liq {
			if l.Available == nil || l.Position == nil || l.Position.Sign() == 0 {
				continue
			}
			av, pos := units(l.Available, dec), units(l.Position, dec)
			if av < c.S5.WarnLiquidityMultiple*pos {
				why = append(why, fmt.Sprintf("only %s can be withdrawn right now against a position of %s", FormatAmount(av, sym), FormatAmount(pos, sym)))
				break
			}
		}
		if len(why) > 0 {
			s5.Level = LevelWarning
			s5.Detail = "Liquidity is tight: " + strings.Join(why, "; ") + "."
		}
	}
	eff["S5"] = s5
	base["S5"] = s5.Detail
	if rank(s5.Level) >= 1 {
		e.lastWarn["S5"] = now
	}

	// ---- S6 config changes ----
	s6 := Signal{ID: "S6", Level: LevelOK, Unit: "changes", Detail: "No admin, owner, upgrade or pause changes seen."}
	if len(o.S6) > 0 {
		e.lastS6 = now
		e.s6Recent = append(e.s6Recent, o.S6...)
		if len(e.s6Recent) > 20 {
			e.s6Recent = e.s6Recent[len(e.s6Recent)-20:]
		}
	}
	comboWin := time.Duration(c.S6.CritComboWindowSec) * time.Second
	if !e.lastS6.IsZero() && now.Sub(e.lastS6) <= comboWin {
		last := e.s6Recent[len(e.s6Recent)-1]
		s6.Level, s6.Value = LevelWarning, float64(len(e.s6Recent))
		s6.Detail = fmt.Sprintf("%s on %s at block %d.", spaced(last.Name), last.Label, last.Block)
		for _, id := range []string{"S1", "S3", "S4"} {
			if w, ok := e.lastWarn[id]; ok && now.Sub(w) <= comboWin {
				s6.Level = LevelCritical
				s6.Detail += fmt.Sprintf(" Combined with a %s warning in the last %d minutes.", strings.ToLower(labelOf(id)), c.S6.CritComboWindowSec/60)
				break
			}
		}
	} else {
		e.s6Recent = nil
	}
	base["S6"] = s6.Detail
	if s6.Level == LevelCritical {
		e.crit["S6"]++
		if e.crit["S6"] < c.DebounceChecks {
			s6.Level = LevelWarning
			s6.Detail += fmt.Sprintf(" Confirming (%d of %d checks).", e.crit["S6"], c.DebounceChecks)
		}
	} else {
		e.crit["S6"] = 0
	}
	eff["S6"] = s6
	if rank(s6.Level) >= 1 {
		e.lastWarn["S6"] = now
	}

	// ---- severity ----
	res := Result{Block: o.Block, Time: now, Inputs: inputs}
	for _, info := range Catalog {
		res.Signals = append(res.Signals, eff[info.ID])
	}
	var critical, warning []Signal
	for _, s := range res.Signals {
		switch s.Level {
		case LevelCritical:
			critical = append(critical, s)
		case LevelWarning:
			warning = append(warning, s)
		}
	}
	twoWin := time.Duration(c.TwoWarningsWindowSec) * time.Second
	var recent []string
	for id, t := range e.lastWarn {
		if now.Sub(t) <= twoWin {
			recent = append(recent, id)
		}
	}
	sort.Strings(recent)
	switch {
	case len(critical) > 0:
		res.Severity = SevCritical
		res.Reason = "Critical: " + critical[0].Detail
	case len(recent) >= 2 && len(warning) > 0:
		res.Severity = SevCritical
		var parts []string
		for _, id := range recent {
			parts = append(parts, labelOf(id))
		}
		d := warning[0].Detail
		if b, ok := base[warning[0].ID]; ok {
			d = b
		}
		res.Reason = fmt.Sprintf("Critical: two different warnings within %d minutes (%s). %s", c.TwoWarningsWindowSec/60, strings.Join(parts, " and "), d)
	case len(warning) > 0:
		res.Severity = SevWarning
		res.Reason = "Warning: " + warning[0].Detail
	default:
		res.Severity = SevWatch
		res.Reason = "All signals are normal."
	}
	inputs["levels"] = levelMap(res.Signals)
	return res
}

func levelMap(ss []Signal) map[string]string {
	m := map[string]string{}
	for _, s := range ss {
		m[s.ID] = s.Level
	}
	return m
}

func labelOf(id string) string {
	for _, i := range Catalog {
		if i.ID == id {
			return i.Label
		}
	}
	return id
}

func pct2(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

// spaced turns OwnershipTransferred into "Ownership transferred".
func spaced(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
			b.WriteRune(r + 32)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
