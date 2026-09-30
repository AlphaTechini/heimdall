# Heimdall

The exit guard for Arbitrum depositors. Heimdall watches your vault and lending positions on Arbitrum, detects exploit and collapse signals, and automatically moves your money back to your own wallet before the drain finishes.

Built for the Arbitrum Open House Singapore buildathon.

- Design and requirements: [`docs/`](docs/) (start with `docs/specs.md`)
- Agent instructions: [`CLAUDE.md`](CLAUDE.md)

## Status

Planning docs, contracts and the web app are committed (see their own status notes). The backend status is below.

### Server (`server/`, binary `heimdalld`)

Done, with evidence from the cloud sandbox (plain anvil + test mocks, Postgres 16):

- `cd server && go build ./... && go vet ./...` clean; `gofmt -l .` empty.
- `bash scripts/local-dev.sh --fresh`, then `heimdalld` with `DEMO_MODE=true`: `GET /healthz`, `/config`, `/positions`, `/signals/{id}` answer.
- Full demo loop on the local mocks: Ada creates a Guard, deposits shares, signs in with SIWE (`cast wallet sign`), saves a policy, then `POST /sim/scenarios/fast-drain/run` gives Watch -> Warning -> Critical, a priority-tip exit of 6,200 USDC (partial), a retry that returns the remaining 3,800 USDC once liquidity comes back, and Ada's wallet USDC goes from 0 to 10,000. Decision to broadcast: 3 ms. Tip x gas limit stays within the USD cap.
- Oracle tampering (S3) and collateral depeg (S4) fire and trigger exits; `POST /sim/reset` restores the chain and clears the rows; every `/sim/*` route answers 403 when `DEMO_MODE=false`.
- Also exercised: `exit_half` at Warning, `ask_first` (alert, then an owner exit is recorded), keeper turned off (no exit, event says why), stop endpoint, retry timeout, stuck-transaction replacement (base-fee spike), WebSocket new-block subscription, Telegram and Resend delivery against local stubs (failed sends are retried 3 times and never delay an exit), the fork-style feed override (`anvil_setCode`), the backtest command against the local anvil.

Not verified in the cloud (no Arbitrum RPC, no Docker): the Arbitrum One fork demo (`scripts/demo-fork.sh`, `heimdalld demo-seed` with real Morpho/Aave), real Telegram and Resend delivery, `docker build server/`, the TMX backtest with an archive RPC. `keeperExitBatch` is not used by the executor yet (one transaction per Guard).

What the builder must do locally:

1. Fill the PLACEHOLDERS in `config/targets.arbitrum-one.json` (each needs an address and a `source` URL): a Morpho USDC vault (`morpho-usdc.address`), `ethUsdFeed`, the vault's `signals` feeds, `sim.drainers` (big share holders of that vault) and `demo.usdcSource` (a big USDC holder). `heimdalld demo-seed` lists whatever is still missing.
2. `export ARBITRUM_ONE_RPC_URL=...` and run `bash scripts/demo-fork.sh`, then start the server with the env it prints.
3. Create the Telegram bot (`TELEGRAM_BOT_TOKEN`, `TELEGRAM_BOT_USERNAME`) and a Resend key with a sender (`RESEND_API_KEY`, `EMAIL_FROM`); set a real `AUTH_SECRET` and a fresh keeper key (`KEEPER_PRIVATE_KEY`, the address must be the factory's keeper).
4. For the Backtest page, run `heimdalld backtest ...` against an archive RPC (see `docs/api.md` section 8); the page stays empty until a real file exists.
5. Deploy and verify the contracts (`scripts/deploy.sh`), run the server with `FACTORY_ADDRESS` set, `docker build server/` (mount `config/`, see the Dockerfile header).
