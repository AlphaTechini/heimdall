package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// AddrSrc is an address with the source it was taken from (specs N8). An empty Address is a
// placeholder the builder still has to fill in.
type AddrSrc struct {
	Address string `json:"address"`
	Source  string `json:"source"`
}

// Set reports whether the address is filled in.
func (a AddrSrc) Set() bool { return strings.TrimSpace(a.Address) != "" }

// Addr returns the parsed address.
func (a AddrSrc) Addr() common.Address { return common.HexToAddress(a.Address) }

type CollateralFeed struct {
	Label   string `json:"label"`
	Address string `json:"address"`
	Source  string `json:"source"`
}

type Liquidity struct {
	Mode   string `json:"mode"` // "idle" or "exitable"
	Holder string `json:"holder"`
}

type WatchContract struct {
	Label   string `json:"label"`
	Address string `json:"address"`
}

type TargetSignals struct {
	MarketFeed      AddrSrc          `json:"marketFeed"`
	ReferenceFeed   AddrSrc          `json:"referenceFeed"`
	CollateralFeeds []CollateralFeed `json:"collateralFeeds"`
	Liquidity       Liquidity        `json:"liquidity"`
	WatchContracts  []WatchContract  `json:"watchContracts"`
}

type Sim struct {
	Drainers  []string `json:"drainers"`
	MockVault bool     `json:"mockVault"`
}

type Target struct {
	ID            string        `json:"id"`
	Label         string        `json:"label"`
	Protocol      string        `json:"protocol"`
	Type          string        `json:"type"` // ERC4626 or AAVE_V3
	Address       string        `json:"address"`
	Source        string        `json:"source"`
	AToken        string        `json:"aToken"`
	Asset         string        `json:"asset"`
	AssetSymbol   string        `json:"assetSymbol"`
	AssetDecimals int           `json:"assetDecimals"`
	AssetIsStable bool          `json:"assetIsStable"`
	Signals       TargetSignals `json:"signals"`
	Sim           Sim           `json:"sim"`
}

// Addr is the Guard target address (vault, or underlying asset for Aave).
func (t *Target) Addr() common.Address { return common.HexToAddress(t.Address) }

// AssetAddr is the underlying asset.
func (t *Target) AssetAddr() common.Address { return common.HexToAddress(t.Asset) }

// IsAave reports whether the target is an Aave V3 reserve.
func (t *Target) IsAave() bool { return t.Type == "AAVE_V3" }

// PositionToken is the token a Guard holds: the vault (ERC4626) or the aToken (Aave).
func (t *Target) PositionToken() common.Address {
	if t.IsAave() {
		return common.HexToAddress(t.AToken)
	}
	return t.Addr()
}

// PositionType returns the on-chain enum value used by HeimdallGuard.PositionType.
func (t *Target) PositionType() uint8 {
	if t.IsAave() {
		return 1
	}
	return 0
}

type Explorer struct {
	Name       string `json:"name"`
	TxURL      string `json:"txUrl"`
	AddressURL string `json:"addressUrl"`
}

type Demo struct {
	Ada        string  `json:"ada"`
	Ben        string  `json:"ben"`
	USDCSource AddrSrc `json:"usdcSource"`
}

type Targets struct {
	ChainID  uint64    `json:"chainId"`
	Note     string    `json:"note"`
	Explorer *Explorer `json:"explorer"`
	AaveV3   struct {
		Pool AddrSrc `json:"pool"`
	} `json:"aaveV3"`
	EthUSDFeed AddrSrc  `json:"ethUsdFeed"`
	Targets    []Target `json:"targets"`
	Demo       Demo     `json:"demo"`
}

// Target returns the target with the given id, or nil.
func (t *Targets) Target(id string) *Target {
	for i := range t.Targets {
		if t.Targets[i].ID == id {
			return &t.Targets[i]
		}
	}
	return nil
}

func checkAddr(problems *[]string, what, a string, required bool) {
	a = strings.TrimSpace(a)
	if a == "" {
		if required {
			*problems = append(*problems, what+" is missing")
		}
		return
	}
	if !common.IsHexAddress(a) {
		*problems = append(*problems, fmt.Sprintf("%s is not a valid address: %q", what, a))
	}
}

// LoadTargets reads and validates a targets file. Targets with an empty address are skipped
// with a warning (they are placeholders, specs N8); a feed with an empty address makes its
// signal report "unavailable".
func LoadTargets(path string) (*Targets, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read targets file %s: %w (set TARGETS_FILE; for local development run scripts/local-dev.sh)", path, err)
	}
	var t Targets
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("targets file %s is not valid JSON: %w", path, err)
	}
	var problems []string
	if t.ChainID == 0 {
		problems = append(problems, "chainId is missing")
	}
	checkAddr(&problems, "aaveV3.pool.address", t.AaveV3.Pool.Address, false)
	checkAddr(&problems, "ethUsdFeed.address", t.EthUSDFeed.Address, false)
	checkAddr(&problems, "demo.ada", t.Demo.Ada, false)
	checkAddr(&problems, "demo.ben", t.Demo.Ben, false)
	checkAddr(&problems, "demo.usdcSource.address", t.Demo.USDCSource.Address, false)
	seen := map[string]bool{}
	kept := t.Targets[:0]
	for i := range t.Targets {
		tg := t.Targets[i]
		w := "target " + tg.ID
		if tg.ID == "" {
			problems = append(problems, fmt.Sprintf("target #%d has no id", i))
			continue
		}
		if seen[tg.ID] {
			problems = append(problems, w+": duplicate id")
			continue
		}
		seen[tg.ID] = true
		if tg.Type != "ERC4626" && tg.Type != "AAVE_V3" {
			problems = append(problems, w+": type must be ERC4626 or AAVE_V3")
			continue
		}
		if strings.TrimSpace(tg.Address) == "" {
			slog.Warn("target skipped: address is a placeholder", "target", tg.ID, "source", tg.Source)
			continue
		}
		checkAddr(&problems, w+" address", tg.Address, true)
		checkAddr(&problems, w+" asset", tg.Asset, true)
		if tg.Type == "AAVE_V3" {
			checkAddr(&problems, w+" aToken", tg.AToken, true)
		}
		if tg.AssetDecimals <= 0 || tg.AssetDecimals > 36 {
			problems = append(problems, w+": assetDecimals must be between 1 and 36")
		}
		if tg.AssetSymbol == "" {
			problems = append(problems, w+": assetSymbol is missing")
		}
		checkAddr(&problems, w+" marketFeed", tg.Signals.MarketFeed.Address, false)
		checkAddr(&problems, w+" referenceFeed", tg.Signals.ReferenceFeed.Address, false)
		for _, f := range tg.Signals.CollateralFeeds {
			checkAddr(&problems, w+" collateral feed "+f.Label, f.Address, false)
		}
		for _, wc := range tg.Signals.WatchContracts {
			checkAddr(&problems, w+" watch contract "+wc.Label, wc.Address, true)
		}
		if m := tg.Signals.Liquidity.Mode; m != "" && m != "idle" && m != "exitable" {
			problems = append(problems, w+": signals.liquidity.mode must be idle or exitable")
		}
		checkAddr(&problems, w+" liquidity holder", tg.Signals.Liquidity.Holder, false)
		for _, d := range tg.Sim.Drainers {
			checkAddr(&problems, w+" sim drainer", d, true)
		}
		if tg.Signals.Liquidity.Mode == "" {
			tg.Signals.Liquidity.Mode = "idle"
		}
		if len(tg.Signals.WatchContracts) == 0 && tg.Type == "ERC4626" {
			tg.Signals.WatchContracts = []WatchContract{{Label: "Vault", Address: tg.Address}}
		}
		if tg.Signals.MarketFeed.Set() != tg.Signals.ReferenceFeed.Set() {
			slog.Warn("oracle signal needs both marketFeed and referenceFeed; it will report unavailable", "target", tg.ID)
		}
		kept = append(kept, tg)
	}
	t.Targets = kept
	if len(problems) > 0 {
		return nil, fmt.Errorf("targets file %s has problems:\n  - %s", path, strings.Join(problems, "\n  - "))
	}
	return &t, nil
}

// FactoryFromDeployment reads {"factory": "0x.."} from a deployment file.
func FactoryFromDeployment(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read deployment file %s: %w", path, err)
	}
	var d struct {
		Factory string `json:"factory"`
	}
	if err := json.Unmarshal(raw, &d); err != nil || !common.IsHexAddress(d.Factory) {
		return "", fmt.Errorf("deployment file %s has no valid \"factory\" address", path)
	}
	return d.Factory, nil
}
