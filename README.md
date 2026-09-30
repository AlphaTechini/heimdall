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

Interim (2026-09-30, build in progress): contracts, server and web are built and verified on a plain local anvil with test mocks (`forge test`: 11 passed, 2 fork tests skipped without RPC; `go build`/`go vet` clean; `pnpm check`/`lint`/`build` clean; fast-drain demo loop returns 10,000 USDC to Ada via a partial then complete exit). The end-to-end click-through and the final builder checklist are being finished; see the next commit.
