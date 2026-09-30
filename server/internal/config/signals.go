package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Signals holds every signal threshold (specs W1: nothing is hardcoded in the watcher).
type Signals struct {
	S1 struct {
		WarnPct       float64 `json:"warnPct"`
		CritPct       float64 `json:"critPct"`
		WindowSec     int     `json:"windowSec"`
		CritLongPct   float64 `json:"critLongPct"`
		LongWindowSec int     `json:"longWindowSec"`
		USDFloor      float64 `json:"usdFloor"`
	} `json:"s1"`
	S2 struct {
		CritDropPct float64 `json:"critDropPct"`
	} `json:"s2"`
	S3 struct {
		WarnPct float64 `json:"warnPct"`
		CritPct float64 `json:"critPct"`
	} `json:"s3"`
	S4 struct {
		WarnBelow float64 `json:"warnBelow"`
		CritBelow float64 `json:"critBelow"`
	} `json:"s4"`
	S5 struct {
		WarnLiquidityMultiple float64 `json:"warnLiquidityMultiple"`
		WarnUtilizationPct    float64 `json:"warnUtilizationPct"`
	} `json:"s5"`
	S6 struct {
		CritComboWindowSec int `json:"critComboWindowSec"`
	} `json:"s6"`
	DebounceChecks       int `json:"debounceChecks"`
	TwoWarningsWindowSec int `json:"twoWarningsWindowSec"`
	Tip                  struct {
		CriticalBudgetPct float64 `json:"criticalBudgetPct"`
		WarningBudgetPct  float64 `json:"warningBudgetPct"`
	} `json:"tip"`
	ExitRetryTimeoutSec int `json:"exitRetryTimeoutSec"`
}

// LoadSignals reads and validates the thresholds file.
func LoadSignals(path string) (*Signals, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read signals file %s: %w (set SIGNALS_FILE)", path, err)
	}
	var s Signals
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("signals file %s is not valid: %w", path, err)
	}
	var p []string
	pos := func(name string, v float64) {
		if v <= 0 {
			p = append(p, name+" must be above 0")
		}
	}
	pos("s1.warnPct", s.S1.WarnPct)
	pos("s1.critPct", s.S1.CritPct)
	pos("s1.windowSec", float64(s.S1.WindowSec))
	pos("s1.critLongPct", s.S1.CritLongPct)
	pos("s1.longWindowSec", float64(s.S1.LongWindowSec))
	pos("s2.critDropPct", s.S2.CritDropPct)
	pos("s3.warnPct", s.S3.WarnPct)
	pos("s3.critPct", s.S3.CritPct)
	pos("s4.warnBelow", s.S4.WarnBelow)
	pos("s4.critBelow", s.S4.CritBelow)
	pos("s5.warnLiquidityMultiple", s.S5.WarnLiquidityMultiple)
	pos("s5.warnUtilizationPct", s.S5.WarnUtilizationPct)
	pos("s6.critComboWindowSec", float64(s.S6.CritComboWindowSec))
	pos("debounceChecks", float64(s.DebounceChecks))
	pos("twoWarningsWindowSec", float64(s.TwoWarningsWindowSec))
	pos("tip.criticalBudgetPct", s.Tip.CriticalBudgetPct)
	pos("tip.warningBudgetPct", s.Tip.WarningBudgetPct)
	pos("exitRetryTimeoutSec", float64(s.ExitRetryTimeoutSec))
	if s.S1.WarnPct >= s.S1.CritPct {
		p = append(p, "s1.warnPct must be below s1.critPct")
	}
	if s.S3.WarnPct >= s.S3.CritPct {
		p = append(p, "s3.warnPct must be below s3.critPct")
	}
	if s.S4.CritBelow >= s.S4.WarnBelow {
		p = append(p, "s4.critBelow must be below s4.warnBelow")
	}
	if s.Tip.CriticalBudgetPct > 100 || s.Tip.WarningBudgetPct > 100 {
		p = append(p, "tip budgets are percentages of the user's cap and cannot exceed 100")
	}
	if len(p) > 0 {
		return nil, fmt.Errorf("signals file %s has problems:\n  - %s", path, strings.Join(p, "\n  - "))
	}
	return &s, nil
}
