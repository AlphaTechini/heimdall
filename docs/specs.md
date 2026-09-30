# Heimdall: Strict Requirements (specs.md)

> Audience: coding agents. This file overrides `details.md` and `user_flow.md` if they conflict.
> Keywords: **MUST** / **MUST NOT** are non-negotiable. **SHOULD** is expected unless there is a written reason. **MAY** is optional.
> Every requirement has an ID so tests, commits and reviews can reference it.

---

## 1. Non-negotiables (the product fails without these)

| ID | Requirement |
|---|---|
| **N1** | Funds leaving a Guard **MUST** only ever go to that Guard's `owner`. No function, path, parameter or admin action may send a user's tokens anywhere else. |
| **N2** | The keeper **MUST** have exactly one power: trigger exits (single or batched) whose receiver is the owner. It **MUST NOT** be able to withdraw position tokens, change owner, change policy, or call arbitrary targets. |
| **N3** | The owner **MUST** be able, at any time and without Heimdall's cooperation, to withdraw their position tokens, exit manually, disable the keeper, and pause keeper exits. |
| **N4** | The demo **MUST** exit through **real protocol contracts** (a real ERC-4626 lending vault on Arbitrum One and the real Aave V3 Pool) on an Arbitrum One fork. Mocked exit adapters **MUST NOT** appear anywhere in the demo path. |
| **N5** | Exits **MUST** handle partial liquidity: redeem what is available, **MUST NOT** revert because liquidity is short, and the executor **MUST** retry for the remainder. |
| **N6** | The project **MUST** be deployed and verified on at least one Arbitrum chain (**Arbitrum Sepolia minimum**) before the submission deadline (Oct 4, 2026, 15:59; timezone to be confirmed on HackQuest). |
| **N7** | Every number shown to users, judges or in the video **MUST** come from real data or be clearly labeled "Simulated on an Arbitrum One fork". The backtest **MUST** use real historical on-chain data; if unavailable, drop the backtest rather than approximate. |
| **N8** | Contract addresses of third-party protocols (vaults, Aave V3 Pool, oracles, tokens) **MUST** be taken from official docs or Arbiscan and recorded with their source in `config/`. They **MUST NOT** be guessed or copied from memory. |

## 2. Smart contract requirements

| ID | Requirement |
|---|---|
| C1 | Guards **MUST** be deployed as EIP-1167 minimal-proxy clones of a single implementation by `HeimdallGuardFactory`. Guards **MUST NOT** be upgradeable. |
| C2 | `owner` **MUST** be set once at initialization and be immutable afterwards. Initialization **MUST** be callable only once and only by the factory. |
| C3 | Exit functions **MUST** hardcode `receiver = owner` for every position type (ERC-4626 `redeem(shares, owner, address(this))`; Aave V3 `withdraw(asset, amount, owner)`). |
| C4 | The MVP **MUST NOT** include `delegatecall`, arbitrary external calls, owner-supplied adapter contracts, or `selfdestruct`. |
| C5 | All token transfers **MUST** use `SafeERC20`. All functions that move tokens **MUST** be protected against reentrancy. |
| C6 | `keeperExitBatch` **MUST** be keeper-only, **MUST** continue past a failing item (try/catch), and **MUST** emit a per-item result. |
| C7 | Exits **MUST** emit `Exited` (amount out, amount burned, remaining, `reasonHash`, caller) or `ExitDeferred` when nothing was available. |
| C8 | Disabling the keeper or pausing **MUST** take effect in the same block. |
| C9 | The factory admin **MAY** rotate the global keeper address, but **MUST NOT** have any power over Guard funds, owners or policies. |
| C10 | If the USDG swap stretch goal is built: the router **MUST** be immutable, `minOut` **MUST** be enforced from an independent price reference, and output **MUST** go to `owner`. |
| C11 | Contracts **SHOULD** compile with no warnings under the pinned Solidity version and **SHOULD** follow checks-effects-interactions. |

## 3. Testing policy (minimal, hackathon)

The builder's rule: **keep tests minimal**. Tests exist only where they protect user funds or let the builder run things locally. No Go unit tests and no frontend unit tests are required. Accuracy comes from the verification gate in `docs/instructions.md` (build, typecheck, lint, run, click-through), not from test volume.

| ID | Requirement |
|---|---|
| T1 | A small Foundry unit test file (mocks allowed in `contracts/test/` only) **MUST** prove: exits send funds only to `owner`; a non-owner, non-keeper caller cannot exit, withdraw, pause or change keeper settings; pause/disable blocks the keeper; a partial exit redeems what is available and does not revert. |
| T2 | Two **fork tests** on Arbitrum One **MUST** exist (one real ERC-4626 vault, one real Aave V3 position), gated on `ARBITRUM_ONE_RPC_URL` and skipped when it is unset. The builder runs them locally; they double as the proof that real exits work. |
| T3 | `forge build` and `forge test` (unit) **MUST** pass before any claim that contracts are done. |
| T4 | An invariant/fuzz test **MAY** be added only after every MUST in this file is met. |

## 4. Watcher, signals and executor requirements

| ID | Requirement |
|---|---|
| W1 | The watcher **MUST** implement signals S1–S6 as defined in `details.md` §8.3, with thresholds loaded from config (not hardcoded). |
| W2 | S2 (share price drop) **MUST** escalate to Critical immediately. Other Critical triggers **MUST** require 2 consecutive confirming checks (debounce). |
| W3 | Every severity change **MUST** be stored with its inputs and a human-readable reason, and pushed to the UI. |
| W4 | The watcher **MUST** ignore balance changes caused by Heimdall's own exits when computing outflow (S1). |
| W5 | From a Critical decision to a signed exit transaction being broadcast **SHOULD** take under 1 second on the fork (measure and display it). |
| W6 | The executor **MUST** set `maxPriorityFeePerGas` from severity and the user's tip cap and **MUST NOT** exceed the user's cap. |
| W7 | The executor **MUST** retry partial exits every block until the position is empty, the user stops it, or the timeout (default 30 min) passes. |
| W8 | The executor **MUST** allow at most one in-flight exit per (Guard, target), manage keeper nonces, and replace stuck transactions with a higher tip within the cap. |
| W9 | The keeper private key **MUST** be read from environment/secret storage and **MUST NOT** be committed, logged or sent to the frontend. |

## 5. Demo, simulator and backtest requirements

| ID | Requirement |
|---|---|
| D1 | The Incident Simulator **MUST** run only when `DEMO_MODE=true` **and** the chain id is the local fork. It **MUST** refuse on any public network. |
| D2 | The simulator UI and every simulated result **MUST** carry the label "Simulated on an Arbitrum One fork". |
| D3 | The demo **MUST** include the "Fast drain" scenario showing Watch → Warning → Critical → partial exit → full exit, and a side-by-side with an unprotected wallet holding the same position. |
| D4 | The demo **SHOULD** include the Oracle tampering and Collateral depeg scenarios (available for Q&A even if not in the video). |
| D5 | The backtest **SHOULD** replay a real 2026 Arbitrum incident (TMX first) using archive data and the same signal code as production. Numbers **MUST** follow N7. |
| D6 | The video **MUST** show the "can only send back to you" guarantee, a working owner withdraw/turn-off, the priority tip on the exit transaction, and Arbiscan links to verified contracts. |

## 6. Web app requirements

| ID | Requirement |
|---|---|
| U1 | The dashboard **MUST** show each supported position with status (Unprotected / Guarded / Exiting / Exited) and a live risk strip for S1–S6. |
| U2 | The Protect panel **MUST** offer the policy choices in `user_flow.md` Scene 2 with the documented defaults, and **MUST** show that the safe address is the user's own wallet (not editable in the MVP). |
| U3 | Every exit **MUST** be shown with its trigger reason, amounts, blocks and Arbiscan (or fork explorer) links. |
| U4 | Policy changes **MUST** require wallet signature (SIWE or equivalent). |
| U5 | The UI **MUST** use plain English (no unexplained jargon) and **SHOULD** work on a laptop screen for recording. |
| U6 | Telegram **and email (Resend, from the Go backend)** alerts **MUST** be sent for Warning, Critical, exit submitted, partial exit, and exit completed. A failed notification **MUST NOT** delay or block an exit. |

## 7. Must NOT do (scope guards)

| ID | Rule |
|---|---|
| X1 | **MUST NOT** claim Heimdall can trigger or request an Arbitrum Security Council freeze. It cannot; that is a human multisig decision. |
| X2 | **MUST NOT** claim protection against single-transaction (atomic) exploits. |
| X3 | **MUST NOT** pool user funds in a shared contract in the MVP (per-user Guards only), unless the builder explicitly approves the pooled design. |
| X4 | **MUST NOT** use Timeboost or express-lane APIs; Arbitrum One replaced Timeboost with per-transaction priority gas auctions on Sept 24, 2026. |
| X5 | **MUST NOT** add features beyond this spec before N1–N8 and all MUST items are done ("double down on the foundation before adding more features"). |
| X6 | **MUST NOT** present Heimdall as audited or production-safe; label mainnet deployments "experimental, unaudited". |
| X7 | **MUST NOT** depend on paid data feeds (e.g., Arbitrum Fast Feed). Use free RPC subscriptions and the free sequencer feed; keep the data source behind an interface. |
| X8 | **MUST NOT** use mock protocol contracts outside unit/invariant tests. Mocks are allowed in `test/` only; fork tests and the demo use real contracts (see N4). |

## 8. Definition of done (submission checklist)

- [ ] N1–N8 satisfied.
- [ ] `forge build` + unit `forge test` green in the cloud (T1, T3); fork tests (T2) written and run locally by the builder.
- [ ] Contracts deployed and verified on Arbitrum Sepolia (and Arbitrum One if [OPEN] Q8 is approved); addresses in README.
- [ ] Watcher detects S1–S6 on the fork; Fast-drain scenario runs end-to-end with a partial then complete exit.
- [ ] Priority tip visible on exit transactions and capped by policy.
- [ ] Dashboard, Protect, Activity, Simulator, Settings pages working; Backtest page working or removed per N7.
- [ ] Telegram and email (Resend) alerts working.
- [ ] README: problem, how it works, safety guarantees, limitations, addresses, how to run locally, demo video link.
- [ ] Demo video follows `user_flow.md` and is under 4 minutes.
- [ ] Submitted on HackQuest before Oct 4, 2026, 15:59 (timezone confirmed).
