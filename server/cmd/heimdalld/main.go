// heimdalld is the Heimdall backend: watcher, signals, executor, API, notifications and the
// demo simulator in one binary.
//
//	heimdalld                 run the server
//	heimdalld demo-seed       prepare the Arbitrum One fork for the demo
//	heimdalld backtest ...    replay the signals over real historical blocks
package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/api"
	"github.com/AlphaTechini/heimdall/server/internal/backtest"
	"github.com/AlphaTechini/heimdall/server/internal/chain"
	"github.com/AlphaTechini/heimdall/server/internal/config"
	"github.com/AlphaTechini/heimdall/server/internal/executor"
	"github.com/AlphaTechini/heimdall/server/internal/hub"
	"github.com/AlphaTechini/heimdall/server/internal/notify"
	"github.com/AlphaTechini/heimdall/server/internal/policy"
	"github.com/AlphaTechini/heimdall/server/internal/sim"
	"github.com/AlphaTechini/heimdall/server/internal/store"
	"github.com/AlphaTechini/heimdall/server/internal/watcher"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	config.LoadDotEnv(".env")
	level := slog.LevelInfo
	if strings.EqualFold(os.Getenv("LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	cmd := "serve"
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "demo-seed":
		err = demoSeed()
	case "backtest":
		err = runBacktest(os.Args[2:])
	case "help":
		fmt.Println("usage: heimdalld [serve | demo-seed | backtest --incident ID --rpc URL --target ADDR --from N --to N --out FILE]")
	default:
		err = fmt.Errorf("unknown command %q (try: serve, demo-seed, backtest)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "heimdalld:", err)
		os.Exit(1)
	}
}

func serve() error {
	env, err := config.LoadEnv(true)
	if err != nil {
		return err
	}
	tg, err := config.LoadTargets(env.TargetsFile)
	if err != nil {
		return err
	}
	sig, err := config.LoadSignals(env.SignalsFile)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ch, err := chain.Dial(ctx, env.RPCHTTPURL, env.RPCWSURL)
	if err != nil {
		return err
	}
	if ch.ChainID.Uint64() != tg.ChainID {
		return fmt.Errorf("the targets file %s is for chain %d but the RPC is chain %s", env.TargetsFile, tg.ChainID, ch.ChainID)
	}
	factory, err := factoryAddress(env, ch)
	if err != nil {
		return err
	}
	if code, err := ch.Eth.CodeAt(ctx, factory, nil); err != nil {
		return fmt.Errorf("cannot check the factory: %w", err)
	} else if len(code) == 0 {
		return fmt.Errorf("there is no contract at the factory address %s on chain %s; deploy it (scripts/local-dev.sh) or fix FACTORY_ADDRESS", factory.Hex(), ch.ChainID)
	}
	keeper, err := chain.NewKeeper(env.KeeperPrivateKey.Reveal(), ch.ChainID)
	if err != nil {
		return err
	}
	slog.Info("keeper loaded", "address", keeper.Address.Hex())
	if fk, err := ch.Addr(ctx, &chain.FactoryABI, factory, nil, "keeper"); err != nil {
		return fmt.Errorf("cannot read the factory's keeper: %w", err)
	} else if fk != keeper.Address {
		slog.Warn("KEEPER_PRIVATE_KEY is not the factory's keeper: exits will be rejected until the factory admin calls setKeeper", "factoryKeeper", fk.Hex(), "thisKey", keeper.Address.Hex())
	}
	if b, err := ch.Eth.BalanceAt(ctx, keeper.Address, nil); err == nil && b.Sign() == 0 {
		slog.Warn("the keeper account has no ETH for gas; exits will fail until it is funded", "keeper", keeper.Address.Hex())
	}

	st, err := store.Open(ctx, env.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.MarkStaleActiveExits(ctx); err != nil {
		return err
	}

	h := hub.New()
	tgram := notify.NewTelegram(env.TelegramToken.Reveal(), env.TelegramBot)
	if tgram.Enabled() && env.TelegramBot == "" {
		slog.Warn("TELEGRAM_BOT_USERNAME is not set: Telegram linking stays off")
	}
	mail := notify.NewEmail(env.ResendAPIKey.Reveal(), env.EmailFrom)
	disp := notify.NewDispatcher(ctx, st, tgram, mail)
	em := &hub.Emitter{Store: st, Hub: h, Notifier: disp}
	pol := policy.NewService(st, env.DefaultTipCapUSD)
	ex := executor.New(ctx, ch, keeper, st, em, h, pol, sig, tg.EthUSDFeed)
	if !tg.EthUSDFeed.Set() {
		slog.Warn("no ethUsdFeed in the targets file: exits will be sent without a priority tip (a price is never invented)")
	}
	w := watcher.New(env, tg, sig, ch, st, em, h, pol, ex, factory)
	if err := w.LoadGuards(ctx); err != nil {
		return err
	}
	sm := sim.New(ctx, env, tg, ch, st, w, ex, pol, h, em, factory)
	if err := sm.Init(ctx); err != nil {
		return err
	}
	if sm.Enabled() {
		slog.Warn("DEMO MODE: the incident simulator is enabled (local anvil only)")
	}

	if tgram.Enabled() && env.TelegramBot != "" {
		go tgram.Poll(ctx, func(c context.Context, code string, chatID int64, username string) string {
			addr, ok, err := st.ConsumeTelegramLink(c, strings.ToUpper(code))
			if err != nil {
				slog.Error("telegram link failed", "err", err)
				return "Something went wrong linking your account. Please try again."
			}
			if !ok {
				return "That link code is not valid or has expired. Get a new one in Heimdall > Settings."
			}
			if err := st.SetTelegram(c, addr, chatID, username); err != nil {
				slog.Error("telegram link failed", "err", err)
				return "Something went wrong linking your account. Please try again."
			}
			return "Linked. Heimdall will send alerts for wallet " + addr[:6] + ".." + addr[len(addr)-4:] + " here."
		})
	}

	authKey := []byte(env.AuthSecret.Reveal())
	if len(authKey) == 0 {
		authKey = make([]byte, 32)
		if _, err := rand.Read(authKey); err != nil {
			return err
		}
		slog.Warn("AUTH_SECRET is not set: using a random one, so sign-ins end when the server restarts")
	}
	a := api.New(api.Deps{Env: env, Targets: tg, Signals: sig, Chain: ch, Store: st, Watcher: w, Executor: ex, Policy: pol,
		Hub: h, Notifier: disp, Telegram: tgram, Email: mail, Sim: sm, Factory: factory}, authKey)

	go w.Run(ctx)

	srv := &http.Server{Addr: env.HTTPAddr, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("heimdalld is up", "addr", env.HTTPAddr, "chainId", ch.ChainID, "factory", factory.Hex(), "targets", len(tg.Targets), "demoMode", sm.Enabled())
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("cannot serve on %s: %w", env.HTTPAddr, err)
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}
	return nil
}

func factoryAddress(env *config.Env, ch *chain.Client) (common.Address, error) {
	if env.FactoryAddress != "" {
		return common.HexToAddress(env.FactoryAddress), nil
	}
	path := env.DeploymentFile
	if path == "" {
		path = filepath.Join("..", "contracts", "deployments", ch.ChainID.String()+".json")
	}
	f, err := config.FactoryFromDeployment(path)
	if err != nil {
		return common.Address{}, fmt.Errorf("no factory address: set FACTORY_ADDRESS, or DEPLOYMENT_FILE to a deployment JSON (%w)", err)
	}
	return common.HexToAddress(f), nil
}

func demoSeed() error {
	env, err := config.LoadEnv(false)
	if err != nil {
		return err
	}
	tg, err := config.LoadTargets(env.TargetsFile)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ch, err := chain.Dial(ctx, env.RPCHTTPURL, "")
	if err != nil {
		return err
	}
	if err := sim.Seed(ctx, ch, tg, func(f string, a ...any) { fmt.Printf(f+"\n", a...) }); err != nil {
		return err
	}
	fmt.Println("Demo seeded.")
	return nil
}

func runBacktest(args []string) error {
	fs := flag.NewFlagSet("backtest", flag.ContinueOnError)
	incident := fs.String("incident", "", "incident id, for example tmx-2026-01 (becomes the file name)")
	title := fs.String("title", "", "title shown on the Backtest page")
	rpc := fs.String("rpc", "", "archive RPC URL for the chain the incident happened on")
	target := fs.String("target", "", "the ERC-4626 vault to replay (address)")
	from := fs.Uint64("from", 0, "first block")
	to := fs.Uint64("to", 0, "last block")
	step := fs.Uint64("step", 1, "replay every Nth block")
	stable := fs.Bool("stable", false, "the vault's asset is a $1 stablecoin (applies the USD floor of signal S1)")
	out := fs.String("out", "", "output file, for example config/backtests/tmx-2026-01.json")
	signalsFile := fs.String("signals", "", "signals thresholds file (default SIGNALS_FILE or ../config/signals.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var missing []string
	if *incident == "" {
		missing = append(missing, "--incident")
	}
	if *rpc == "" {
		missing = append(missing, "--rpc")
	}
	if !common.IsHexAddress(*target) {
		missing = append(missing, "--target (a vault address)")
	}
	if *to == 0 || *to < *from {
		missing = append(missing, "--from and --to (block numbers, --to >= --from)")
	}
	if *out == "" {
		missing = append(missing, "--out")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing or invalid: %s", strings.Join(missing, ", "))
	}
	sf := *signalsFile
	if sf == "" {
		sf = os.Getenv("SIGNALS_FILE")
	}
	if sf == "" {
		sf = "../config/signals.json"
	}
	sig, err := config.LoadSignals(sf)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	res, err := backtest.Run(ctx, backtest.Params{Incident: *incident, Title: *title, RPC: *rpc, Target: common.HexToAddress(*target),
		From: *from, To: *to, Step: *step, Stable: *stable, Out: *out}, sig)
	if err != nil {
		return err
	}
	if err := backtest.Write(res, *out); err != nil {
		return err
	}
	if url := os.Getenv("DATABASE_URL"); url != "" {
		if st, err := store.Open(ctx, url); err == nil {
			_, _ = st.Pool.Exec(ctx, `INSERT INTO backtest_runs (incident_id,target,from_block,to_block,out_path) VALUES ($1,$2,$3,$4,$5)`, *incident, *target, int64(*from), int64(*to), *out)
			st.Close()
		}
	}
	fmt.Printf("Wrote %d points to %s\n", len(res.Points), *out)
	return nil
}
