# Heimdall: interfaces shared by contracts, server and web (api.md)

> Written by the orchestrating agent so `contracts/`, `server/` and `web/` can be built in parallel against one contract.
> `docs/specs.md` still wins on any conflict. If an implementation must deviate, update this file in the same change.

---

## 1. Solidity interface (contracts/)

```solidity
enum PositionType { ERC4626, AAVE_V3 }            // uint8 on the wire: 0, 1

struct ExitRequest {
    address guard;
    PositionType positionType;
    address target;        // ERC4626: vault address. AAVE_V3: underlying asset (e.g. USDC)
    uint256 maxAmount;     // in position-token units (vault shares / aTokens); type(uint256).max = everything
    bytes32 reasonHash;    // keccak256(utf8 reason text); server keeps the text
}
```

### HeimdallGuardFactory (Ownable2Step; owner = "admin")
- `constructor(address admin, address keeper, address aavePool)`; deploys the `HeimdallGuard` implementation in the constructor. `aavePool` may be `address(0)` (Aave unsupported on that chain).
- `implementation() view returns (address)` (immutable), `aavePool() view returns (address)` (immutable)
- `keeper() view returns (address)`; `setKeeper(address)` onlyOwner (C9: the only admin power)
- `guardOf(address owner) view returns (address)` (zero if none)
- `createGuard() returns (address guard)`: clone (EIP-1167) + `initialize(msg.sender)`; reverts if the caller already has one
- `keeperExitBatch(ExitRequest[] calldata reqs)`: keeper-only; try/catch each `guard.exit(...)`; never reverts because one item fails
- Events: `GuardCreated(address indexed owner, address indexed guard)`, `KeeperUpdated(address indexed previousKeeper, address indexed newKeeper)`, `BatchExitResult(uint256 indexed index, address indexed guard, address indexed target, bool success, bytes reason)`

### HeimdallGuard (one per user, clone, not upgradeable)
- `factory() view returns (address)` (immutable in the implementation), `owner() view returns (address)`, `keeperEnabled() view returns (bool)` (true after init), `paused() view returns (bool)`
- `initialize(address owner_)`: only the factory, only once
- Owner only: `deposit(PositionType t, address target, uint256 amount)`, `withdraw(PositionType t, address target, uint256 amount)`, `setKeeperEnabled(bool)`, `setPaused(bool)`
- `exit(PositionType t, address target, uint256 maxAmount, bytes32 reasonHash) returns (uint256 amountOut, uint256 burned, uint256 remaining)`: callable by the owner (always), the keeper (`msg.sender == IFactory(factory).keeper()`) or the factory (batch path); keeper/factory calls revert when `!keeperEnabled || paused`. Receiver is always `owner`. Partial liquidity: redeems what is available; if nothing is available emits `ExitDeferred` and returns zeros (no revert).
- Views: `positionToken(PositionType t, address target) returns (address)` (vault itself, or the Aave aToken), `positionBalance(PositionType t, address target) returns (uint256)`
- Events: `Deposited(uint8 indexed positionType, address indexed target, uint256 amount)`, `Withdrawn(uint8 indexed positionType, address indexed target, uint256 amount)`, `Exited(address indexed target, uint256 amountOut, uint256 burned, uint256 remaining, bytes32 reasonHash, address indexed caller)`, `ExitDeferred(address indexed target, bytes32 reasonHash)`, `KeeperToggled(bool enabled)`, `Paused(bool paused)`

ABIs are never hand-written: `scripts/sync-abis.sh` copies them from `contracts/out/` into `server/internal/chain/abi/*.json` and generates `web/src/lib/abi.ts`.

---

## 2. HTTP API (server/, consumed by web/)

Base path `/api`. JSON everywhere. Amounts are **decimal strings of raw integer units** plus `decimals` (the client formats). Addresses are checksummed. Times are RFC3339 UTC. Errors: HTTP 4xx/5xx with `{"error": "plain-English message"}`.
CORS: allow `WEB_ORIGIN` (env), headers `Authorization, Content-Type`.

### Auth (SIWE, EIP-4361)
- `GET /api/auth/nonce` → `{ "nonce": "..." }`
- `POST /api/auth/verify` `{ "message": "<EIP-4361 text>", "signature": "0x..." }` → `{ "token": "...", "address": "0x...", "expiresAt": "..." }`. Server checks signature (personal_sign), nonce (single use), domain = `SIWE_DOMAIN`, chain id, expiry.
- Authenticated calls send `Authorization: Bearer <token>`.

### Read
- `GET /api/healthz` → `{ "ok": true, "chainId": 31337, "block": 123, "db": "ok" }`
- `GET /api/config` → `{ "chainId", "chainName", "explorerTxUrl": "https://arbiscan.io/tx/" | "" , "explorerAddressUrl", "factoryAddress", "keeperAddress", "demoMode": bool, "backtestAvailable": bool, "usdgRouteAvailable": false, "defaultPolicy": Policy, "limits": { "maxTipCapUsd": 50 } }`
- `GET /api/targets` → `[Target]` where `Target = { "id", "label", "protocol", "type": "erc4626"|"aave_v3", "positionType": 0|1, "address", "positionToken", "asset": { "address", "symbol", "decimals" }, "shareDecimals" }`
- `GET /api/positions?address=0x..` → `{ "address", "guard": "0x.."|null, "keeperEnabled": bool|null, "paused": bool|null, "positions": [Position] }`
  `Position = { "targetId", "walletBalance", "walletAssets", "guardBalance", "guardAssets", "decimals", "shareDecimals", "status": "unprotected"|"guarded"|"exiting"|"exited", "severity": "watch"|"warning"|"critical", "policy": Policy|null, "lastExit": ExitReceipt|null }`
  (a target is listed when the wallet or its Guard holds it, or it has exits; `walletAssets`/`guardAssets` are in underlying units)
- `GET /api/signals/{targetId}` → `SignalState = { "targetId", "severity", "reason", "changedAt", "lastCheckAt", "block", "signals": [Signal] }`, `Signal = { "id": "S1".."S6", "label", "level": "ok"|"warning"|"critical"|"na", "value": number|null, "unit": "%"|"usd"|"x"|"", "threshold": string, "detail": string }`
- `GET /api/signals/{targetId}/history?limit=200` → `[{ "at", "block", "severity", "reason", "signals": [Signal] }]` (severity changes + sampled snapshots, newest first)
- `GET /api/activity?address=0x..&target={targetId}&limit=50` → `[Activity]`, `Activity = { "id", "at", "kind": "check"|"severity"|"alert"|"exit_submitted"|"exit_partial"|"exit_completed"|"exit_deferred"|"guard_created"|"deposit"|"withdraw"|"keeper_toggled"|"paused"|"policy", "targetId"|null, "message": "plain English", "txHash"|null, "block"|null }`
- `GET /api/guards/{guard}/exits` → `[ExitReceipt]`, `ExitReceipt = { "targetId", "txHash", "block", "amountOut", "burned", "remaining", "decimals", "reason", "reasonHash", "tipWei", "maxPriorityFeePerGas", "decisionToBroadcastMs", "at" }`

### Write (Bearer token; the token's address must own the Guard)
- `PUT /api/policies/{guard}/{targetId}` body `Policy` → `Policy`
  `Policy = { "critical": "auto_exit"|"ask_first", "warning": "notify"|"exit_half"|"exit_full", "sendTo": "usdc", "priority": bool, "tipCapUsd": number }` (validation: tipCapUsd > 0 and ≤ limits.maxTipCapUsd when priority is true)
- `GET /api/me` → `{ "address", "telegram": { "linked": bool, "username"|null }, "email": { "address"|null, "verified": bool }, "defaultPolicy": Policy }`
- `PUT /api/me/default-policy` body `Policy` → `Policy`
- `POST /api/telegram/link` → `{ "code", "deepLink": "https://t.me/<bot>?start=<code>" }` (503 with a clear error if `TELEGRAM_BOT_TOKEN` is unset)
- `DELETE /api/telegram` → `{ "ok": true }`; `POST /api/telegram/test` → `{ "ok": true }`
- `POST /api/email/code` `{ "email" }` → `{ "ok": true }`; `POST /api/email/verify` `{ "code" }` → `{ "ok": true }`; `POST /api/email/test` → `{ "ok": true }` (503 if `RESEND_API_KEY` unset)

Owner-only on-chain actions (Protect, Exit now, Pause/Resume, Turn off protection, Withdraw) are **wallet transactions sent by the web app**, not API calls (specs N3: they must work without Heimdall).

### Demo only (DEMO_MODE=true and the node is a local anvil fork; see §4)
- `GET /api/sim/scenarios` → `{ "enabled": bool, "reason": string, "label": "Simulated on an Arbitrum One fork", "scenarios": [{ "name": "fast-drain"|"oracle-tampering"|"collateral-depeg", "label", "description" }] }`
- `POST /api/sim/scenarios/{name}/run` → `{ "runId" }` (409 if a run is active)
- `POST /api/sim/reset` → `{ "ok": true }` (reverts the fork to the snapshot taken after demo setup; clears demo rows)
- `GET /api/sim/runs/{runId}` → `{ "runId", "scenario", "state": "running"|"done"|"failed", "log": [{ "at", "block", "message" }] }`
- `GET /api/sim/compare` → `{ "label": "Simulated on an Arbitrum One fork", "protected": { "name": "Ada", "address", "startAssets", "nowAssets", "decimals" }, "unprotected": { "name": "Ben", ... }, "timeline": { "firstSignalBlock", "exitSubmittedBlock", "exitConfirmedBlock", "drainFinishedBlock" } }` (nulls when not yet known)

### Backtest (only real data; §N7)
- `GET /api/backtests` → `[{ "id", "label", "chain", "source" }]` (only incidents with loaded real data; empty list hides the page)
- `GET /api/backtests/{id}` → `{ "id", "label", "source", "series": [{ "block", "at", "totalAssets", "severity" }], "firstFire": { "block", "at", "pctRemaining" }|null, "caption" }`

### WebSocket `GET /api/stream?address=0x..`
Server → client messages `{ "type": "signals", "data": SignalState }`, `{ "type": "activity", "data": Activity }`, `{ "type": "positions", "data": <same as GET /api/positions> }` (sent when the address's positions change), `{ "type": "sim", "data": { "runId", "at", "block", "message", "state" } }`. Client reconnects with backoff.

---

## 3. Reason hashes
`reasonHash = keccak256(bytes(reason))`, reason like `"Critical: vault lost 18.2% of assets in 48s (threshold 15% in 60s)"`. Stored in `exits.reason`.

## 4. Local fork conventions
- Demo fork: `anvil --fork-url $ARBITRUM_ONE_RPC_URL --chain-id 31337 --block-time 1`. The fork keeps Arbitrum One contracts but reports chain id 31337, so it can never be confused with a public network.
- The simulator is enabled only if `DEMO_MODE=true`, `eth_chainId == 31337` and `web3_clientVersion` starts with `anvil`.
- Demo wallets (anvil default accounts, never used on public networks): Ada = account #1, Ben = account #2, keeper = account #3, deployer/admin = account #0.
