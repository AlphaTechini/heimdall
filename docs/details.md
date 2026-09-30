# Heimdall: End-to-End Project Details

> Audience: coding agents (and the builder). Read with `user_flow.md` (demo flow) and `specs.md` (strict requirements).
> If this file and `specs.md` ever disagree, `specs.md` wins.
> Status: draft v1 (2026-09-30). Items marked **[OPEN]** await the builder's decision; items marked **[VERIFY]** must be checked against primary sources before being relied on.

---

## 1. What Heimdall is

**One line:** Heimdall is the exit guard for Arbitrum depositors. It watches a depositor's vault and lending positions, detects exploit and collapse signals, and automatically moves the depositor's money back to their own wallet before the drain finishes.

**Name:** Heimdall, the Norse watchman who guards the bridge to Asgard, sees and hears everything, and sounds the horn when an attack begins. (Note: "Heimdall" is also the name of a component in Polygon's PoS network. Different chain, different job; use the tagline to keep them apart.)

**Tagline:** "The exit guard for Arbitrum depositors."

**Event:** Arbitrum Open House Singapore online buildathon (HackQuest). Submissions close **Oct 4, 2026, 15:59** (timezone not shown on the page **[VERIFY]**). Projects must be **deployed on an Arbitrum chain**. Judging: smart contract quality and security, product-market fit and retention, innovation, real problem-solving. Extra consideration for **Paxos USDG** integration.

---

## 2. The problem (with evidence)

1. **Arbitrum depositors keep losing money to exploits they could not see coming.**
   - SlowMist lists 17 Arbitrum incidents in 2026 through September. The Block reports a further ~$24.15M AFX Trade bridge exploit (July 22, 2026). Combined, roughly $50M+ in 2026.
   - Most incidents hit small and mid-size protocols, whose users have no professional monitoring.
   - Losses usually land on **liquidity providers and vault depositors**, not traders (e.g., Ostium's OLP vault, July 2026, ~$18M to ~$24M depending on source; GMX V1 GLP, July 2025, $42M).
2. **Many attacks are not instant.** The TMX (TMXTribe) exploit on Arbitrum (January 2026) ran **502 transactions over about 48 hours**, with about 36 hours of active exploitation and no emergency pause. Ostium's attacker sent a small test transaction before the main drain. There is often a window in which a watching depositor could have left.
3. **Knowing is not enough; getting out is hard.** Morpho's own help center explains that depositors "cannot immediately withdraw their full balance" when borrowers are using most of the vault's liquidity. In a crisis, everyone rushes for the same limited liquidity, and the first out wins.
4. **Existing protection is for professionals, not depositors.** Hypernative's automated responses serve funds and curators (e.g., kpk's exit agent during the Resolv exploit, March 2026). Morpho's Sentinel role only moves money back inside a vault. DeFi Saver automation reacts to a borrower's health ratio, not to exploits.

## 3. Target users

| Priority | User | Why they need Heimdall |
|---|---|---|
| Primary | **Everyday depositors in Arbitrum lending vaults** (Morpho, Euler and similar ERC-4626 vaults; Aave V3 suppliers) | They carry exploit and bad-debt risk, have no monitoring, and lose the race to exit. |
| Secondary | **Liquidity providers in perp DEX pools** (GMX GM/GLV, Ostium OLP) | They absorb losses when perp DEXs are exploited. (Roadmap: GMX exits are two-step and not in the MVP.) |
| Secondary | **Small DAOs and funds** holding the same positions | Same risk, no budget for enterprise tools. |
| Channel | **Vault curators, wallets, protocols** | Can offer "Protect with Heimdall" to their own users via the same contracts. |

## 4. Competitive landscape and how Heimdall differs

Prior Arbitrum work (all from **Open House London**, May–June 2026, none shown among Founder House winners):

| Project | What it does | Gap Heimdall fills |
|---|---|---|
| **Aegis** | Stylus risk engine + bounded keeper that exits DeFi/RWA positions on exploit/depeg signals. | Deployed version exits through a **MockExitAdapter**; real adapters unreleased; Forta feed falls back to a stub. Heimdall must exit through **real protocol code** in the demo. |
| **coincoin** | EIP-7702 "firewall" that sweeps wallet tokens and exits Aave V3 into a user-controlled vault on threat signals (on-chain `Drained` logs, scam feeds). | Aave-only (GMX on roadmap), wallet-drain focus, real Aave path only fork-tested. Heimdall targets **protocol-level failures** in lending vaults, handles **partial liquidity**, and uses **priority exits**. |
| **ArbGuard (Dubai)** | One-click EIP-7702 evacuation to a cold wallet. | Manual. Heimdall is automatic. |
| **VELA** | AI yield vault with an emergency exit for its own vault only. | Not for external positions. |
| **Hypernative + kpk** | Commercial, protected kpk's Morpho vaults on Ethereum and Arbitrum. | Only for kpk's vaults; not available to individual depositors. |

**Heimdall's differentiators (must all be visible in the demo):**
1. **Wins the race to the exit**: early detection + **priority exit transactions** + **partial exits with automatic retry** when liquidity is short.
2. **Real exits on real Arbitrum vaults** (ERC-4626 lending vaults such as Morpho/Euler, and Aave V3) on an Arbitrum One fork, with contracts deployed on Arbitrum One and Arbitrum Sepolia.
3. **Protocol-failure signals** grounded in real 2026 incidents (drain, oracle tampering, depeg, share-price drop, risky config changes), not only wallet-drain feeds.
4. **"Your money can only come home"**: the keeper cannot send funds anywhere except the owner.
5. **Backtest on real history**: show when Heimdall's rules would have fired on a real 2026 Arbitrum incident.

## 5. Key concepts (plain English)

- **Guard**: a small personal contract each user owns. The user moves their vault shares or aTokens into it. Heimdall's keeper can tell it to exit, but the money can only go back to the owner.
- **Keeper**: Heimdall's bot that submits exit transactions when the watcher decides to exit. It has exactly one power: trigger an exit to the owner.
- **Watcher**: off-chain service that reads the chain in near real time and computes risk signals.
- **Signal**: a measurable warning sign (e.g., "vault lost 12% of assets in 60s").
- **Severity**: Watch → Warning → Critical.
- **Policy**: the user's rules (e.g., auto-exit on Critical, notify on Warning).
- **Partial exit**: withdraw whatever liquidity is available now; retry for the rest.
- **Priority exit**: attaching a priority fee (`maxPriorityFeePerGas`) so the exit is ordered ahead of others. Arbitrum One switched from Timeboost to **per-transaction priority gas auctions** on Sept 24, 2026 (125 ms bid rounds inside 250 ms blocks).
- **Exit receipt**: an on-chain event recording what triggered an exit.

## 6. Architecture overview

```
                +---------------------------------------------+
                |              Web app (SvelteKit)            |
                | Dashboard · Protect · Activity · Simulator · |
                | Backtest · Settings                          |
                +-------------------+-------------------------+
                                    | HTTPS / WebSocket
                +-------------------v-------------------------+
                |        heimdalld (Go service, one binary)    |
                |  api · watcher · signals · policy · executor |
                |  notify (Telegram+email) · backtest · sim  |
                +----+-------------+----------------+----------+
                     |             |                |
             Postgres |      RPC / WS / sequencer   | signed txs (keeper key)
                     |             feed             |
                +----v----+   +----v----------------v-----------------+
                | storage |   | Arbitrum One (fork for demo) /         |
                +---------+   | Arbitrum Sepolia / Arbitrum One mainnet|
                              |  HeimdallGuardFactory · HeimdallGuard  |
                              |  ERC-4626 vaults · Aave V3 Pool        |
                              +----------------------------------------+
```

**What is on-chain and why** (answers the judges' "does it need to be onchain?"):
- **On-chain**: custody boundary and permissions (Guard), the hard rule that funds can only go to the owner, partial-exit logic, exit receipts. These must be trustless: users must not have to trust Heimdall's servers with their money.
- **Off-chain**: signal computation, policy evaluation, notifications, UI. Speed and flexibility matter, and a wrong decision can only cause a safe exit, never a loss of custody.

## 7. Smart contracts (Solidity, Foundry)

### 7.1 Contracts

1. **`HeimdallGuardFactory`**
   - Deploys one **Guard per user** as an EIP-1167 minimal-proxy clone of a fixed implementation. No upgradeable proxies.
   - Immutable config set at deployment: Guard implementation address, Aave V3 Pool address for the chain (or zero if unsupported), and optional swap router (stretch).
   - Holds the **global keeper address**, changeable only by the factory admin; each Guard owner can **disable the global keeper for their Guard at any time**.
   - `createGuard()` → returns the caller's Guard (one per address; revert if it exists).
   - `keeperExitBatch(ExitRequest[] calldata)` → loops `guard.exit(...)` for many Guards in one transaction (used in a bank-run to exit several users in one priority transaction). Must be keeper-only and must not revert the whole batch because one Guard fails (use try/catch and emit a per-item result).
2. **`HeimdallGuard`** (per user)
   - `owner`: set once at initialization, never changeable.
   - Supported position types (MVP): `ERC4626` (vault shares) and `AAVE_V3` (aTokens). Positions are tracked by the owner's chosen target address (vault address or Aave underlying asset).
   - Owner functions:
     - `deposit(PositionType t, address target, uint256 amount)`: pull shares/aTokens from owner (`safeTransferFrom`).
     - `withdraw(PositionType t, address target, uint256 amount)`: return position tokens (not redeemed) to owner.
     - `exit(...)`: owner can also exit manually.
     - `setKeeperEnabled(bool)`, `setPaused(bool)`: pause blocks keeper exits (owner actions still allowed).
   - Keeper function:
     - `exit(PositionType t, address target, uint256 maxAmount, bytes32 reasonHash)`: redeems as much as currently possible (up to `maxAmount`) **with `receiver = owner` hardcoded**; emits `Exited`.
   - Partial exit logic:
     - ERC-4626: `shares = min(heldShares, vault.maxRedeem(address(this)), sharesFor(maxAmount))`; call `vault.redeem(shares, owner, address(this))`. If 0 available, emit `ExitDeferred` and return (do not revert).
     - Aave V3: `available = min(aTokenBalance, underlying.balanceOf(aToken))`; call `pool.withdraw(asset, amount, owner)`.
   - Events: `Deposited`, `Withdrawn`, `Exited(target, amountOut, sharesOrATokensBurned, remaining, reasonHash, caller)`, `ExitDeferred(target, reasonHash)`, `KeeperToggled`, `Paused`.
   - No arbitrary calls, no `delegatecall`, no owner-supplied adapter contracts in the MVP.
3. **USDG exit (stretch, [OPEN])**: redeem into the Guard, swap via an immutable router with an oracle-checked `minOut`, then transfer to owner. Only if a liquid USDG route exists on the target chain **[VERIFY: USDG on Arbitrum One]**. On Robinhood Chain USDG is native.

### 7.1a GMX GM/GLV pools (roadmap, not MVP)

GMX V2 withdrawals take two steps, which is why they are not in the MVP:
1. **Request**: call `ExchangeRouter.createWithdrawal()` (or the GLV equivalent), sending the GM/GLV tokens plus an **execution fee in ETH**, with `receiver`, minimum output amounts and optional swap paths. Token transfer and request must happen in the same transaction.
2. **Execution**: a GMX keeper executes the request in a later block using oracle prices. Unused execution fee is refunded; failed or cancelled requests refund the tokens.

It can be automated (the Guard would create the request with `receiver = owner` and hold ETH for the execution fee), but completion depends on GMX's own keepers and on GMX not being paused, which is exactly what may happen during an exploit. It also needs refund/cancel handling. Estimated extra effort: 1–2 days. Whether a contract can create withdrawals with a different receiver must be verified in GMX's code **[VERIFY]**.

### 7.2 Security properties to prove with tests

- Tokens leaving a Guard can only go to `owner` (unit test; invariant test optional, see specs T4).
- Keeper can call only `exit` / `keeperExitBatch`; all other state changes are owner-only.
- Disabling the keeper or pausing immediately blocks keeper exits.
- Partial exits never revert because of low liquidity; they redeem what is available.
- Reentrancy protection on all external-token-moving functions; `SafeERC20` everywhere.
- Fork tests against **real** Arbitrum One contracts: at least one ERC-4626 lending vault (e.g., a Morpho vault) and the Aave V3 Pool. Addresses must be taken from official protocol docs or Arbiscan **[VERIFY]**, never guessed.

## 8. Watcher and signals (Go)

### 8.1 Data sources

- **Primary (demo)**: WebSocket subscription to new blocks and logs on the fork (Anvil). No external feed is needed because the demo watches its own node.
- **Production (free)**: the standard Arbitrum sequencer feed via a local Nitro feed relay (free, publishes complete blocks in near real time), plus standard RPC WebSocket subscriptions (`eth_subscribe` to `newHeads` and `logs`). Arbitrum has no public mempool, so the free sequencer feed is the earliest free signal.
- **Not used: Fast Feed.** It is paid (dynamic price, $17 minimum per 24-hour ticket; publishes ordered transactions before the block closes). The builder decided not to use paid feeds. The watcher's data-source layer should be an interface so a faster feed could be plugged in later without changing signal code.
- Price references: Chainlink feeds where available; otherwise a DEX pool price / TWAP **[VERIFY per asset]**.

### 8.2 Monitored objects

- A config file lists **supported targets** per chain (vault address, type, underlying asset, markets and oracles to watch, reference feeds). The MVP does not auto-discover every vault on Arbitrum.

### 8.3 Signals (MVP)

| ID | Signal | How it is computed | Default thresholds (tunable) | Real-world basis |
|---|---|---|---|---|
| S1 | **Fast outflow** | Drop in `totalAssets()` (ERC-4626) or reserve liquidity (Aave) over a rolling time window, ignoring the user's own exits. | Warning ≥ 5% in 60s; Critical ≥ 15% in 60s or ≥ 25% in 10 min; ignore if absolute change < configurable USD floor. | TMX slow drain; AFX; GMX V1 |
| S2 | **Share price drop** | `convertToAssets(1 share)` decreases (normally it only rises). | Critical on any drop ≥ 0.1% (no debounce). | Realized bad debt / theft |
| S3 | **Oracle deviation** | Price used by the vault's markets vs reference price. | Warning ≥ 2%; Critical ≥ 5% for 2 consecutive checks. | Ostium (fraudulent price reports) |
| S4 | **Collateral depeg** | Price of stablecoin collateral in the vault's markets. | Warning < $0.985; Critical < $0.95. | Stream Finance xUSD |
| S5 | **Liquidity squeeze** | Utilization / available liquidity vs user position size. | Warning when available liquidity < 2× user position or utilization ≥ 95%. Escalates other signals. | Morpho liquidity docs |
| S6 | **Risky config change** | Ownership/admin changes, contract upgrades, oracle changes, new market or cap added to a vault, pauses. | Warning by default; Critical when combined with S1, S3 or S4 within 10 min. | Stream curator exposure; admin-key attacks |

**Severity rules**
- **Critical** if: S2 fires; or any single signal hits its Critical threshold (after debounce); or two different signals are at Warning within 5 minutes.
- **Warning** if any signal is at Warning.
- **Watch** otherwise.
- Every severity change is stored with its inputs so the UI can show a human-readable reason.

### 8.4 Policy evaluation

Per Guard position, the user's policy decides the action:
- Critical → `auto_exit` (default) or `ask_first` (send alert with an in-app "Exit now" button).
- Warning → `notify` (default), `exit_half`, or `exit_full`.
- Priority tip cap (USD) per exit.

## 9. Executor (keeper)

- Builds and signs `exit` (or `keeperExitBatch`) transactions with the keeper key.
- **Priority exit**: sets `maxPriorityFeePerGas` from severity and the user's tip cap: `tipPerGas = min(capWei, severityBudgetWei) / estimatedGas`. On Arbitrum One this uses the per-transaction priority gas auction (live since Sept 24, 2026). On a local fork the ordering effect is not reproduced; the demo shows the tip and explains it. Arbitrum Sepolia support **[VERIFY]**. Robinhood Chain uses first-come-first-served ordering, so tips do not help there.
- **Retry loop for partial exits**: after a partial exit, re-attempt every block until the position is zero, the user stops it, or a timeout (default 30 minutes) is reached.
- Idempotency: one in-flight exit per (Guard, target); nonce management for the keeper; replacement transactions with higher tips if stuck.
- Keeper key: environment variable for the hackathon; production would use a KMS/HSM and multiple keepers.
- Gas and tips are paid by the Heimdall operator in the MVP **[OPEN: business model]**.

## 10. Backend API (Go, same binary)

- REST + WebSocket.
- Endpoints (indicative):
  - `GET /positions?address=` supported positions and Guard status
  - `GET /guards/:address`, `GET /guards/:address/activity`
  - `PUT /policies/:guard/:target`
  - `GET /signals/:target` (live risk strip)
  - `WS /stream` (signal updates, incidents, exits)
  - `POST /telegram/link`
  - Demo-only (enabled by `DEMO_MODE=true` and fork chain id): `POST /sim/scenarios/:name/run`, `GET /sim/scenarios`
  - `GET /backtests/:incident`
- Auth: sign-in with Ethereum (SIWE) for policy changes; read endpoints public.

**Postgres tables (minimum):** `users`, `guards`, `positions`, `policies`, `signal_snapshots`, `incidents`, `exits`, `notifications`, `backtest_runs`.

## 11. Web app (SvelteKit, Svelte 5)

- Wallet connection with viem (+ `@wagmi/core` or equivalent), Tailwind for styling.
- Pages:
  1. **Dashboard**: position cards with status (Unprotected / Guarded / Exiting / Exited) and live risk strip.
  2. **Protect** panel: policy choices (see `user_flow.md` Scene 2); creates the Guard and deposits.
  3. **Position detail**: signal history, severity timeline, exit receipts with Arbiscan links.
  4. **Activity**: feed of checks, alerts and exits.
  5. **Incident Simulator** (demo mode only): run scenarios on the fork; clearly labeled "Simulated on an Arbitrum One fork".
  6. **Backtest**: "Replay a real hack" chart.
  7. **Settings**: Telegram link, email (verified), default policy.
- Always show the guarantee: "Heimdall can only send this money back to you."

## 12. Notifications

These alerts go to **Heimdall's users** (depositors), not to the builder.

- **Telegram bot** (MVP): Warning and Critical alerts, exit confirmations, partial-exit progress. User links their Telegram in Settings. Env: `TELEGRAM_BOT_TOKEN`.
- **Email via Resend** (MVP): the same alerts by email, sent from the Go backend with Resend's official Go SDK (`github.com/resend/resend-go`). User adds an email in Settings (verify with a one-time code). Env: `RESEND_API_KEY`, `EMAIL_FROM`.
  - Note: Resend only delivers to arbitrary recipients from a **verified sending domain** (the builder can use a subdomain of cyberpunkinc.xyz). Without one, it can only send to the Resend account's own address, which is fine for the demo.
- Both channels go through one `Notifier` interface so a failure in one does not block the other; failed sends are retried and logged, never blocking an exit.

## 13. Incident Simulator (demo only)

- Runs only when `DEMO_MODE=true` and the chain id is the local fork. Must refuse on any public network.
- Scenarios:
  1. **Fast drain** (default for the video): impersonated accounts withdraw large amounts from the target vault over several blocks, and/or borrowers drain available liquidity, so S1 and S5 fire and the partial-exit path is visible.
  2. **Oracle tampering**: on a demo market whose oracle can be controlled on the fork (e.g., by overriding the feed's storage or pointing a test market at a controllable oracle), push the price away from the reference so S3 fires.
  3. **Collateral depeg**: move the collateral price below the peg so S4 fires.
- Implementation: Anvil cheat methods (`anvil_impersonateAccount`, `anvil_setBalance`, `anvil_setStorageAt`, `evm_mine`) driven from Go or Foundry scripts.
- A second, unprotected demo wallet (Ben) holds the same position so the video can compare outcomes.

## 14. Backtest ("Replay a real hack")

- Goal: show, using **real historical on-chain data**, when Heimdall's rules would have fired during a real 2026 Arbitrum incident and how much was still in the pool at that moment.
- First target: **TMX (January 5–7, 2026)**. Find the exploited contracts and attacker transactions from the Rekt and SlowMist write-ups and Arbiscan **[VERIFY]**. Fetch historical logs/state via an archive RPC (QuickNode is a buildathon sponsor).
- Run the same signal code in "replay" mode over those blocks; output a JSON series for the chart.
- Never invent numbers. If the data cannot be obtained in time, drop the scene rather than approximate.

## 15. Arbitrum features: how Heimdall uses them

| Feature | Status | Use in Heimdall |
|---|---|---|
| **Per-transaction priority gas auctions** (replaced Timeboost) | Live on Arbitrum One since Sept 24, 2026 | **Priority exits**: tip on exit transactions so Heimdall users leave ahead of the crowd. MVP. |
| **Sequencer feed** (free) | Live | Near-real-time watching. Production data source. |
| **Fast Feed** (paid, ordered transactions before block close) | Approved in governance; paid tickets from $17/day | **Not used** (paid). The free sequencer feed and RPC subscriptions are used instead. |
| **Dynamic pricing** | Live on Arbitrum One | Steadier fees in congestion, when exits matter most. Passive benefit; mention in pitch. |
| **Paxos USDG** | Native on Robinhood Chain; Arbitrum One **[VERIFY]** | Optional exit asset (sponsor bonus). Stretch. |
| **Robinhood Chain** | Live since July 1, 2026 | Possible second chain; first-come-first-served ordering (no priority tips). **[OPEN]** |
| **Security Council emergency freeze** | Used April 21, 2026 (Kelp DAO, ~30,766 ETH frozen) | **Not automatable.** It was a human 9-of-12 multisig decision: the Council temporarily upgraded the Ethereum-side Delayed Inbox so a message could impersonate the exploiter, moved the funds to a frozen address, then reverted the upgrade. There are no published request criteria. Heimdall's possible role: an "Incident Pack" export (attacker addresses, tx hashes, timeline) to help humans escalate faster. Roadmap only. |
| **ZK settlement, universal intents, yield-bearing bridge, selective-disclosure privacy** | In development | Not used. Possible future: faster moves to Ethereum after an exit. |
| **EIP-7702** | Live since ArbOS 40 | Alternative onboarding without moving shares (the coincoin approach). Not in MVP **[OPEN]**. |
| **Stylus** | Live (96 KB contracts since ArbOS 61) | Not in MVP. Possible on-chain risk checks later. |

## 16. Deployment

- **Networks**: local Arbitrum One fork (demo), **Arbitrum Sepolia** (qualifies for the buildathon), **Arbitrum One mainnet** (preferred; past winners were live).
- Verify all contracts on Arbiscan (Sepolia and One).
- Config per chain: RPC URLs, keeper key, factory address, supported targets file, Telegram bot token, Resend API key + sender address, Postgres URL, `DEMO_MODE`.
- `docker-compose.yml`: anvil (fork), postgres, server (heimdalld), web.

## 17. Repository structure (suggested)

Frontend, backend and contracts live in **separate top-level folders**, each with its own `.gitignore`. See `docs/instructions.md` for exact setup commands.

```
heimdall/
  contracts/            # Foundry (Solidity)
    src/HeimdallGuard.sol
    src/HeimdallGuardFactory.sol
    src/interfaces/     # IERC4626, IAavePool, IAToken
    test/               # minimal: unit (mocks allowed) + fork tests
    script/Deploy.s.sol
    .gitignore
  server/               # Go 1.26 backend (binary: heimdalld)
    cmd/heimdalld/main.go
    internal/{config,chain,watcher,signals,policy,executor,api,notify,backtest,sim,store}
    go.mod
    Dockerfile
    .dockerignore
    .gitignore
  web/                  # SvelteKit + Svelte 5 + TypeScript + Tailwind (pnpm)
    .gitignore
  config/targets.arbitrum-one.json
  scripts/cloud-setup.sh  # cloud-only toolchain setup
  e2e/                  # Playwright click-through check (Python)
  docs/{user_flow.md,details.md,specs.md,instructions.md,UI_UX.md}
  docker-compose.yml
  README.md
  CLAUDE.md
```

## 18. Build plan (deadline Oct 4, 15:59)

| Day | Focus | Done when |
|---|---|---|
| Sep 30 – Oct 1 | Contracts + minimal tests (unit with mocks; fork tests for ERC-4626 vault + Aave V3 run locally by the builder) | Guard exits positions; funds only go to owner |
| Oct 1 – Oct 2 | Watcher signals S1–S6 + executor with priority tip + partial-exit retry | Fast-drain scenario on fork triggers an automatic exit end-to-end |
| Oct 2 – Oct 3 | Web app: dashboard, protect flow, activity, simulator; Telegram alerts | Demo flow Scenes 1–6 work on the fork |
| Oct 3 | Backtest (TMX), deploy + verify on Arbitrum Sepolia and Arbitrum One | Addresses verified on Arbiscan |
| Oct 4 (morning) | Record video, README, submission | Submitted before 15:59 |

Registration closes **Oct 2, 17:01** (timezone not shown **[VERIFY]**).

## 19. Risks and limitations (state openly in the README)

- **Cannot beat an empty vault**: if liquidity is gone, Heimdall can only exit what is available; partial exits and speed reduce but do not remove losses.
- **False positives**: an unnecessary exit costs gas, tip and lost yield, but never custody.
- **Atomic exploits** (single-transaction drains) cannot be front-run by a watcher; Heimdall helps with multi-transaction attacks, slow drains, depegs and liquidity crunches.
- **Keeper centralization** in the MVP (one operator key); the keeper can only send funds home.
- **Unaudited hackathon code**; mainnet deployment should be labeled experimental.

## 20. Open questions for the builder [OPEN]

1. Per-user Guard (current design) or a pooled **"protected vault token"** (one wrapper per vault, exits everyone in one transaction, composable)?
2. Exit asset: underlying (USDC) only, or also USDG where a route exists?
3. MVP protocols: ERC-4626 vaults + Aave V3 (current), or add GMX GM pools?
4. Chains: Arbitrum One only, or also Robinhood Chain?
5. Who pays gas and priority tips (operator, user-funded tip budget, or a fee)?
6. Watcher language: Go (current plan) or TypeScript?
7. Which real incident to feature in the backtest: TMX (current), Ostium, or Stream?
8. Deploy to Arbitrum One mainnet with small real funds for the demo, or testnet + fork only?
9. EIP-7702 onboarding (no need to move shares) in addition to the Guard?

## 21. Build environment constraints (for coding agents)

- Cloud build sessions **cannot reach Arbitrum RPC endpoints** (network policy blocks them). In the cloud:
  - Write all code and the minimal unit tests; run them locally (no fork) using OpenZeppelin ERC-4626 and minimal mock pool contracts **for tests only**. Run `scripts/cloud-setup.sh` first (see `docs/instructions.md`).
  - Write the Arbitrum One **fork tests** and **deployment scripts**, gated on `ARBITRUM_ONE_RPC_URL` / `ARBITRUM_SEPOLIA_RPC_URL` env vars, and skip them cleanly when the vars are absent.
  - Do not guess third-party addresses; leave clearly marked placeholders in `config/` with a `source` field to fill in.
- The builder runs fork tests, the demo fork, and deployments **locally** with their own RPC URL (e.g., QuickNode or Alchemy free tier).
- Package registries (npm, Go proxy, crates) and GitHub are reachable from the cloud.

## 22. References

- Buildathon rules: https://www.prod.hackquest.io/en/hackathons/Arbitrum-Open-House-Singapore-Online-Buildathon
- SlowMist Arbitrum incidents: https://hacked.slowmist.io/?c=Arbitrum
- AFX Trade exploit (The Block): https://theblock.co/post/409482/arbitrum-protocol-afx-trade-exploit
- TMX exploit (Rekt): https://alpha.rekt.news/tmztribe-rekt
- Ostium post-mortem (The Block): https://theblock.co/post/410122/ostium-post-mortem-exploit
- Stream Finance collapse (Decrypt): https://decrypt.co/347285/stream-finance-stablecoin-plunges-77-protocol-fund-manager-loses-93-million
- Morpho vault liquidity: https://help.morpho.org/en/articles/14779745-morpho-vaults-and-available-liquidity
- Hypernative + kpk (Resolv): https://www.hypernative.io/blog/when-the-resolv-exploit-hit-kpks-depositors-lost-nothing
- Aegis: https://arbitrum-singapore.hackquest.io/projects/Aegis-SlpzyT · https://github.com/big14way/aegis
- coincoin: https://arbitrum-singapore.hackquest.io/projects/coincoin · https://github.com/gamween/coincoin
- ArbGuard (Dubai): https://arbitrum-dubai.hackquest.io/projects/ArbGuard-AI-Powered-Arbitrum-Security-Shield
- Priority gas auctions replace Timeboost: https://cryptobriefing.com/arbitrum-priority-gas-auctions-replace-timeboost/
- Fast Feed AIP: https://forum.arbitrum.foundation/t/constitutional-aip-fast-feed/31003
- Security Council emergency action (Kelp DAO): https://forum.arbitrum.foundation/t/security-council-emergency-action-21-04-2026/30803
- Sequencer feed docs: https://docs.arbitrum.io/run-arbitrum-node/sequencer/read-sequencer-feed
- Arbitrum roadmap (June 2026): https://cryptoadventure.com/arbitrum-targets-compliance-privacy-and-faster-settlement-in-product-roadmap/
- ArbOS 61 Elara: https://docs.arbitrum.io/run-arbitrum-node/arbos-releases/arbos61
- Robinhood Chain docs: https://docs.robinhood.com/chain/
