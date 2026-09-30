# Heimdall

**The exit guard for Arbitrum depositors.** Heimdall watches your vault and lending positions on Arbitrum, detects exploit and collapse signals, and automatically moves your money back to your own wallet before the drain finishes, using a priority fee so your exit lands ahead of the crowd.

Built for the Arbitrum Open House Singapore buildathon. **Experimental, unaudited hackathon code. Do not use with funds you cannot afford to lose.**

- Requirements and design: [`docs/`](docs/) (start with [`docs/specs.md`](docs/specs.md); API contract in [`docs/api.md`](docs/api.md))
- Agent instructions: [`CLAUDE.md`](CLAUDE.md)

## The problem

In 2026, Arbitrum protocols lost over $50M to exploits (SlowMist lists 17 incidents through September, plus the ~$24M AFX Trade exploit). Losses land on vault depositors and liquidity providers. Many attacks are not instant: the TMX exploit ran 502 transactions over about 48 hours. Depositors who were watching could have left, but most were not watching, and in a crisis everyone races for the same limited liquidity. Professional tools like Hypernative protect funds and curators, not everyday depositors.

## How it works

1. **Guard.** You create your own small contract (an EIP-1167 clone, not upgradeable) and move your ERC-4626 vault shares or Aave V3 aTokens into it. Only you own it.
2. **Watcher.** The Go service `heimdalld` reads every block and computes six signals per vault: S1 fast outflow, S2 share price drop, S3 oracle deviation, S4 collateral depeg, S5 liquidity squeeze, S6 risky config change. Thresholds live in [`config/signals.json`](config/signals.json). Severity moves Watch → Warning → Critical, and every change is stored with its inputs and a plain-English reason.
3. **Policy.** You choose what happens: on Critical, exit automatically or ask first; on Warning, notify, exit half or exit fully; and a priority tip cap in USD.
4. **Executor.** On an exit decision the keeper submits `exit()` on your Guard with `maxPriorityFeePerGas` sized from severity and your cap (Arbitrum One's per-transaction priority fees). If the vault lacks liquidity, the Guard redeems what is available and never reverts. The executor retries every block for the rest until the position is empty, you stop it, or 30 minutes pass.
5. **Alerts.** Telegram and email (Resend) for Warning, Critical, exit submitted, partial exit and exit complete. A failed alert never delays an exit.

## Safety guarantees (enforced on-chain)

- Funds leaving a Guard go **only to its owner**. Every exit hardcodes the receiver: ERC-4626 `redeem(shares, owner, guard)`, Aave V3 `withdraw(asset, amount, owner)`.
- The keeper has exactly one power: trigger exits to the owner, singly or batched. It cannot withdraw position tokens, change the owner or policy, or call arbitrary targets.
- The owner can at any time, without Heimdall: withdraw the position tokens, exit manually, turn the keeper off, and pause keeper exits (effective in the same block).
- The factory admin can only rotate the global keeper address. It has no power over Guard funds or owners.
- No `delegatecall`, no arbitrary calls, no owner-supplied adapters, no `selfdestruct`; SafeERC20 and reentrancy guards on every token-moving function.

## Limitations (stated openly)

- **Cannot beat an empty vault.** If liquidity is gone, Heimdall can only exit what is available. Partial exits and speed reduce losses; they do not remove them.
- **No protection against single-transaction (atomic) exploits.** A watcher cannot front-run a drain that happens inside one transaction. Heimdall helps with multi-transaction attacks, slow drains, depegs and liquidity crunches.
- **Heimdall cannot trigger an Arbitrum Security Council freeze.** That is a human multisig decision.
- **False positives** cost gas, tip and lost yield, never custody.
- **Keeper centralization:** one operator key in the MVP (it can only send funds home). Gas and tips are paid by the operator.
- On a local fork the tip is attached but the priority auction ordering is not reproduced; on Arbitrum One the tip buys priority.

## Repository layout

```
contracts/  Foundry (Solidity 0.8.28): HeimdallGuard, HeimdallGuardFactory, unit + fork tests, Deploy script
server/     Go 1.26 backend heimdalld: watcher, signals, policy, executor, API + WebSocket, notifications, simulator, backtest
web/        SvelteKit + Svelte 5 + TypeScript + Tailwind (pnpm): Dashboard, Protect, Position detail, Activity, Simulator, Settings
e2e/        Playwright click-through of every page and control (Python)
config/     targets per chain (third-party addresses with sources), signal thresholds
scripts/    local-dev.sh, demo-fork.sh, deploy.sh, gen-abi.sh, cloud-setup.sh (cloud sessions only)
```

## Run locally

Prerequisites: Foundry, Go 1.26, Node 22 + pnpm, Postgres 16 (or `docker compose up postgres`), `jq`.

```bash
git clone --recurse-submodules https://github.com/AlphaTechini/heimdall && cd heimdall
createdb heimdall   # or use the docker-compose Postgres (user/pass heimdall)
```

**A. Demo on an Arbitrum One fork (the real demo path).** Needs your own Arbitrum One RPC URL and the placeholders in `config/targets.arbitrum-one.json` filled in (see Status below).

```bash
export ARBITRUM_ONE_RPC_URL=https://...
bash scripts/demo-fork.sh              # anvil fork (chain id 31337), deploys the factory, seeds Ada and Ben
cd server && <export the env it prints> && export AUTH_SECRET=$(openssl rand -hex 32) && go run ./cmd/heimdalld
cd web && pnpm install && pnpm dev     # http://localhost:5173
```

In MetaMask or Rabby add the network `http://127.0.0.1:8545`, chain id 31337, and import anvil's test account #1 (Ada) or #2 (Ben). Then Dashboard → Protect → Simulator → Fast drain.

**B. Plain local chain with test mocks (no RPC needed).** This is how the cloud build was verified. It is not the demo path (the mocks are test-only).

```bash
bash scripts/local-dev.sh --fresh      # anvil + mock vault, mock Aave pool, mock feeds; writes config/targets.local.json
cd server && <export the env it prints> && export AUTH_SECRET=dev-secret-change-me && go run ./cmd/heimdalld
cd web && pnpm install && pnpm dev
python3 e2e/clickthrough.py            # optional: full click-through with a stub wallet
```

**Checks:** `cd contracts && forge build && forge test` · `cd server && go build ./... && go vet ./...` · `cd web && pnpm check && pnpm lint && pnpm build`.

## Deployments

| Network | HeimdallGuardFactory | HeimdallGuard implementation |
|---|---|---|
| Arbitrum Sepolia | not deployed yet | not deployed yet |
| Arbitrum One (experimental, unaudited) | not deployed yet | not deployed yet |

Demo video: not recorded yet.

## Status

Last updated 2026-09-30 by the cloud build session. The cloud sandbox cannot reach Arbitrum RPC endpoints, so everything below was verified on a **plain local anvil with test mocks** (`scripts/local-dev.sh`). The real demo path (Arbitrum One fork with real Morpho/Aave contracts), deployments and real alerts must be run by the builder (numbered list at the end).

### Done, with evidence (fresh runs in the cloud)

| Area | Command | Result |
|---|---|---|
| Contracts | `cd contracts && forge build && forge test` | compiles; 11 passed, 0 failed, 2 skipped (fork tests skip cleanly without `ARBITRUM_ONE_RPC_URL`) |
| Server | `cd server && go build ./... && go vet ./... && gofmt -l .` | clean, no output |
| Server run | `scripts/local-dev.sh --fresh`, `heimdalld` (DEMO_MODE=true) + Postgres 16 | `/healthz`, `/config`, `/positions`, `/signals`, `/exits`, `/backtests` answer |
| Demo loop (API) | Ada creates a Guard, deposits 10,000 shares; `POST /sim/scenarios/fast-drain/run` | Watch → Warning (5.5%/60s) → Critical (16.5%/60s) → exit submitted (priority tip) → partial exit 6,200 USDC → retry → remaining 3,800 USDC; Ada's wallet 0 → 10,000 USDC; decision to broadcast 3 ms |
| Tip cap (W6) | `/exits/{id}` after fast drain | tip budget is per exit: tx1 exposure $2.00 max ($1.39 paid), tx2 limited to the remaining $0.61 |
| Other scenarios | oracle tampering, collateral depeg, reset | S3 and S4 go Warning → Critical and trigger exits; `/sim/reset` restores chain and DB; `/sim/*` returns 403 with `DEMO_MODE=false` |
| Web | `cd web && pnpm check && pnpm lint && pnpm build` | 0 errors, 0 warnings; lint exit 0; build exit 0 |
| UI click-through | `python3 e2e/clickthrough.py` against the real server + anvil (stub wallet forwarding to anvil) | 48 passed, 0 failed, 240 controls swept, 0 console errors, exit 0; routes at 1440 and 375 px with no horizontal overflow; band sequence Calm → Warning → Critical → Exiting → Home safe; screenshots reviewed |
| Docker | `docker compose config` | valid; images not built (no Docker daemon in the sandbox) |

Also exercised by the agents that built each part (see commit messages): `exit_half` at Warning, `ask_first`, keeper turned off, Pause, stop endpoint, retry timeout, stuck-transaction replacement, Aave partial exit on the mock pool, SIWE negatives (replayed nonce, wrong signer, non-owner), Telegram and Resend against local stubs (failures retried 3 times, never delay an exit), and the backtest command in both modes against the local anvil.

### Not done or not verifiable in the cloud

- **Arbitrum One fork demo** (`scripts/demo-fork.sh`, `heimdalld demo-seed`, fast drain against a real Morpho vault): written, not run. The partial-then-complete exit is tuned on the mock vault; on a real vault the partial exit only shows if the configured `sim.drainers` hold enough shares to pull available liquidity below Ada's position. Tune the drainer list if the exit completes in one go.
- **Fork tests** (`contracts/test/ForkExits.t.sol`): written, skipped here.
- **Deployments** on Arbitrum Sepolia / One: script ready (`scripts/deploy.sh`), not run.
- **Real Telegram and Resend delivery**: code verified against local stubs only.
- **Backtest (TMX)**: the replay command exists (`heimdalld backtest`, docs/api.md §8) but no data was fetched (needs an archive RPC). Per specs N7 the Backtest page stays hidden until a real data file exists.
- `docker build` of `server/` and `web/`.
- `keeperExitBatch` exists on-chain but the executor sends one transaction per Guard.
- No chain-reorg handling in the watcher (fine on anvil; production would need it).
- Assumptions made without the builder: pooled design not built (per-user Guards, specs X3); exit asset is the underlying (no USDG route verified); the fork runs with chain id 31337 so the simulator guard (DEMO_MODE + chain 31337 + anvil client) can tell it apart from Arbitrum One; the deadline timezone is still unconfirmed.

### What the builder must do locally

1. **Tools:** install Foundry, Go 1.26, Node 22 + pnpm, Postgres 16; `git submodule update --init --recursive`.
2. **Fill the placeholders** in `config/targets.arbitrum-one.json`, each with an address and a `source` URL (never from memory, specs N8): a Morpho USDC vault on Arbitrum One (`targets[morpho-usdc].address`, from app.morpho.org + Arbiscan), `ethUsdFeed` (Chainlink ETH/USD from docs.chain.link), the vault's `signals` feeds (market feed, reference feed, collateral feeds), `sim.drainers` (largest share holders of that vault, Arbiscan holders tab) and `demo.usdcSource` (a large USDC holder). `heimdalld demo-seed` prints whatever is still missing.
3. **Fork tests:** `cd contracts && ARBITRUM_ONE_RPC_URL=... FORK_ERC4626_VAULT=<the Morpho vault> forge test --match-contract ForkExits -vv` (proof that real exits work, specs T2).
4. **Demo fork:** `export ARBITRUM_ONE_RPC_URL=...; bash scripts/demo-fork.sh`, then start `heimdalld` with the env it prints plus `AUTH_SECRET=$(openssl rand -hex 32)`, then `cd web && pnpm install && pnpm dev`. Add network `http://127.0.0.1:8545` / chain id 31337 to your wallet and import anvil accounts #1 (Ada) and #2 (Ben). Run Protect → Simulator → Fast drain; if the exit does not go partial first, add more `sim.drainers`. Optionally run `API_URL=http://127.0.0.1:8080 python3 e2e/clickthrough.py` against it.
5. **Telegram bot:** create it with @BotFather, set `TELEGRAM_BOT_TOKEN` and `TELEGRAM_BOT_USERNAME`; link it in Settings and press Send test alert.
6. **Resend:** create an API key and set `RESEND_API_KEY` and `EMAIL_FROM` (a verified domain, e.g. a subdomain of cyberpunkinc.xyz; without one Resend only delivers to the account's own address). Verify your email in Settings and press Send test email.
7. **Deploy and verify on Arbitrum Sepolia:** generate a keeper key, then `PRIVATE_KEY=... KEEPER_ADDRESS=<keeper address> ARBISCAN_API_KEY=... ARBITRUM_SEPOLIA_RPC_URL=... bash scripts/deploy.sh sepolia`. Put the factory and implementation addresses in the Deployments table above. Optional (Q8): `bash scripts/deploy.sh one` (label it experimental and unaudited).
8. **Run against Sepolia (optional):** `heimdalld` with `TARGETS_FILE=../config/targets.arbitrum-sepolia.json`, `FACTORY_ADDRESS=...`, `DEMO_MODE=false`, the keeper key, `START_BLOCK=<deployment block>`.
9. **Backtest (optional, specs D5):** find the TMX pool address, tokens and block range from the Rekt and SlowMist write-ups and Arbiscan, then run `heimdalld backtest --mode balance ...` with an archive RPC (exact command in docs/api.md §8). If the data cannot be fetched, skip it: the page stays hidden.
10. **Confirm the deadline timezone** on HackQuest (Oct 4, 2026, 15:59).
11. **Record the video** (under 4 minutes, following docs/user_flow.md): show the "can only send back to you" guarantee, a working Withdraw / Turn off, the priority tip on the exit transaction, the partial then complete exit, Ada vs Ben, and Arbiscan links to the verified contracts. Add the link to this README, then submit on HackQuest.
