// Package config loads heimdalld's configuration: environment variables (docs/api.md §3),
// the targets file (§2) and the signal thresholds file (specs W1).
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// Secret is a string that never prints itself (specs W9: the keeper key must not be logged).
type Secret string

func (s Secret) String() string               { return "[redacted]" }
func (s Secret) GoString() string             { return "[redacted]" }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }

// Reveal returns the underlying value. Use only where the secret is actually needed.
func (s Secret) Reveal() string { return string(s) }

// Env is the parsed environment.
type Env struct {
	HTTPAddr         string
	DatabaseURL      string
	RPCHTTPURL       string
	RPCWSURL         string
	TargetsFile      string
	SignalsFile      string
	FactoryAddress   string
	DeploymentFile   string
	KeeperPrivateKey Secret
	DemoMode         bool
	AuthSecret       Secret
	CORSOrigins      []string
	TelegramToken    Secret
	TelegramBot      string
	ResendAPIKey     Secret
	EmailFrom        string
	DefaultTipCapUSD float64
	StartBlock       *uint64
	BacktestsDir     string
}

// LoadDotEnv reads KEY=VALUE lines from path into the process environment for keys that
// are not already set. A missing file is fine.
func LoadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
}

func get(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// LoadEnv reads and validates the environment. When needServer is false only the RPC and
// file settings are required (used by the demo-seed and backtest subcommands).
func LoadEnv(needServer bool) (*Env, error) {
	var problems []string
	e := &Env{
		HTTPAddr:         get("HTTP_ADDR", ":8080"),
		DatabaseURL:      get("DATABASE_URL", ""),
		RPCHTTPURL:       get("RPC_HTTP_URL", ""),
		RPCWSURL:         get("RPC_WS_URL", ""),
		TargetsFile:      get("TARGETS_FILE", "../config/targets.arbitrum-one.json"),
		SignalsFile:      get("SIGNALS_FILE", "../config/signals.json"),
		FactoryAddress:   get("FACTORY_ADDRESS", ""),
		DeploymentFile:   get("DEPLOYMENT_FILE", ""),
		KeeperPrivateKey: Secret(strings.TrimPrefix(get("KEEPER_PRIVATE_KEY", ""), "0x")),
		AuthSecret:       Secret(get("AUTH_SECRET", "")),
		TelegramToken:    Secret(get("TELEGRAM_BOT_TOKEN", "")),
		TelegramBot:      strings.TrimPrefix(get("TELEGRAM_BOT_USERNAME", ""), "@"),
		ResendAPIKey:     Secret(get("RESEND_API_KEY", "")),
		EmailFrom:        get("EMAIL_FROM", ""),
		BacktestsDir:     get("BACKTESTS_DIR", ""),
	}
	e.CORSOrigins = splitList(get("CORS_ORIGIN",
		"http://localhost:5173,http://127.0.0.1:5173,http://localhost:4173,http://127.0.0.1:4173"))
	demo, err := strconv.ParseBool(get("DEMO_MODE", "false"))
	if err != nil {
		problems = append(problems, "DEMO_MODE must be true or false")
	}
	e.DemoMode = demo
	tip, err := strconv.ParseFloat(get("DEFAULT_TIP_CAP_USD", "2"), 64)
	if err != nil || tip <= 0 || tip > 50 {
		problems = append(problems, "DEFAULT_TIP_CAP_USD must be a number above 0 and at most 50")
		tip = 2
	}
	e.DefaultTipCapUSD = tip
	if v := get("START_BLOCK", ""); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			problems = append(problems, "START_BLOCK must be a block number")
		} else {
			e.StartBlock = &n
		}
	}
	if e.RPCHTTPURL == "" {
		problems = append(problems, "RPC_HTTP_URL is required (for example http://127.0.0.1:8545)")
	}
	if needServer {
		if e.DatabaseURL == "" {
			problems = append(problems, "DATABASE_URL is required (for example postgres://heimdall:heimdall@127.0.0.1:5432/heimdall?sslmode=disable)")
		}
		if e.KeeperPrivateKey == "" {
			problems = append(problems, "KEEPER_PRIVATE_KEY is required: the keeper account that submits exits (hex, 64 characters)")
		} else if len(e.KeeperPrivateKey) != 64 {
			problems = append(problems, "KEEPER_PRIVATE_KEY must be 64 hex characters")
		}
		if e.FactoryAddress != "" && !common.IsHexAddress(e.FactoryAddress) {
			problems = append(problems, "FACTORY_ADDRESS is not a valid address")
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("configuration problems:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return e, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
