# Heimdall: final steps to a running demo and a submission

Written 2026-10-02. Everything public (addresses, feeds, RPC URLs, TMX data) is already filled in or listed here. What is left needs **your** accounts or keys: an RPC key, a Telegram bot, a Resend key, a funded Sepolia wallet, and the video.

Run every command in **Git Bash** (not PowerShell): the scripts are bash. Paths are relative to the repo root unless a step says `cd`.

Time budget: about 2–3 hours, plus the video.

---

## 0. What is already done for you

`config/targets.arbitrum-one.json` has no placeholders left. Every address was checked on-chain on 2026-10-02 and has its source URL in the file:

| Field | Value | What it is |
|---|---|---|
| Morpho vault | `0x7e97fa6893871A2751B5fE961978DCCb2c201E65` | Gauntlet USDC Core (gtUSDCc), listed on app.morpho.org, ~767k USDC |
| `ethUsdFeed` | `0x639Fe6ab55C921f74e7fac1ee960C0B6293ba612` | Chainlink ETH / USD |
| `marketFeed` | `0x6ce185860a4963106506C203335A2910413708e9` | Chainlink BTC / USD, used by the oracle of the vault's biggest market (WBTC/USDC, ~73%) |
| `referenceFeed` | `0xd0C7101eACbB49F3deCcCc166d238410D6D46d57` | Chainlink WBTC / USD (separate feed, same collateral) |
| `collateralFeeds` | `0x37833E5b3fbbEd4D613a3e0C354eF91A42B81eeB` | Chainlink USDS / USD (USDS backs the sUSDS market, ~17%) |
| `sim.drainers` | 11 addresses | Largest gtUSDCc holders that are EOAs, ~532k of ~732k shares |
| `demo.usdcSource` | `0x1714400FF23dB4aF24F9fd64e7039e6597f18C2b` | EOA holding ~5.9M USDC |

`config/targets.arbitrum-sepolia.json`: `ethUsdFeed` = `0xd30e2101a97dcbAeBCBC04F14C3f624E67A35165` (Chainlink ETH / USD on Arbitrum Sepolia).

Public RPC URLs (no key, rate-limited, **not archive**):

| Network | URL | Chain id |
|---|---|---|
| Arbitrum One | `https://arb1.arbitrum.io/rpc` | 42161 |
| Arbitrum Sepolia | `https://sepolia-rollup.arbitrum.io/rpc` | 421614 |

They work for a quick try, but the fork makes many requests and the backtest needs archive data, so get a free key (step 1.3).

---

## 1. One-time setup (≈30 min)

### 1.1 Install tools
- **Foundry** (forge, cast, anvil). In Git Bash:
  ```bash
  curl -L https://foundry.paradigm.xyz | bash
  # open a new Git Bash window, then:
  foundryup
  forge --version && anvil --version
  ```
  (Foundryup only works in Git Bash or WSL, not PowerShell.)
- If Foundry is installed but `forge` is not found, add it to PATH: `echo 'export PATH="$HOME/.foundry/bin:$PATH"' >> ~/.bashrc` and open a new Git Bash window.
- **Go 1.26**, **Node 22 + pnpm**, **jq**: already on this machine. Check with `go version; pnpm -v; jq --version`.
- **Postgres 16** (the server will not start without it). Docker is not needed; install Postgres natively, see step 1.4.

### 1.2 Get the contract libraries
```bash
git submodule update --init --recursive
```

### 1.3 Get an RPC key (free)
1. Sign up at https://dashboard.alchemy.com (or QuickNode, a buildathon sponsor).
2. Create an app with **Arbitrum Mainnet** and **Arbitrum Sepolia** enabled.
3. Copy the two HTTPS URLs. They look like `https://arb-mainnet.g.alchemy.com/v2/<KEY>` and `https://arb-sepolia.g.alchemy.com/v2/<KEY>`.
4. Check that the mainnet URL serves old state (needed for the optional backtest):
   ```bash
   cast call 0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9 "totalSupply()(uint256)" \
     --block 417990000 --rpc-url "https://arb-mainnet.g.alchemy.com/v2/<KEY>"
   ```
   A number means archive works. `missing trie node` means it does not (you can still do everything except step 7).

### 1.4 The two settings files

Heimdall has two settings files. They are plain text, one `NAME=value` per line, and both stay off GitHub.

| File | Who reads it | What goes in it |
|---|---|---|
| `server/.env` (in the repo, git-ignored) | the Go server, every time it starts | database, alerts, keeper key, which config files to use (section 3.3) |
| `C:\Users\rehob\.heimdall-env` (your home folder) | the bash scripts (`demo-fork.sh`, `deploy.sh`) and the commands in this guide | your two Alchemy RPC URLs |

Why `.env` and not `.ini`: `.env` is the usual format for app settings and secrets. It is the same `NAME=value` lines as an `.ini` file, just without `[sections]`; the server reads it with a few lines of code (`server/internal/config/env.go`). Why a second file: the scripts are bash, and bash loads settings by running a file of `export NAME=value` lines (`source ~/.heimdall-env`). Keeping the RPC URLs in your home folder also keeps your Alchemy key out of the repo.

Create `.heimdall-env` once. Either open Notepad, paste the two lines below with your key in place of `<KEY>`, and save as `C:\Users\rehob\.heimdall-env` (Save as type: All files):
```bash
export ARBITRUM_ONE_RPC_URL="https://arb-mainnet.g.alchemy.com/v2/<KEY>"
export ARBITRUM_SEPOLIA_RPC_URL="https://arb-sepolia.g.alchemy.com/v2/<KEY>"
```
or run this in Git Bash (it writes the lines between the two `EOF` markers into the file):
```bash
cat > ~/.heimdall-env <<'EOF'
export ARBITRUM_ONE_RPC_URL="https://arb-mainnet.g.alchemy.com/v2/<KEY>"
export ARBITRUM_SEPOLIA_RPC_URL="https://arb-sepolia.g.alchemy.com/v2/<KEY>"
EOF
```
The scripts load it on their own. For the plain commands in this guide, run `source ~/.heimdall-env` first in that Git Bash window.

Postgres (once). In PowerShell:
```powershell
winget install -e --id PostgreSQL.PostgreSQL.16
```
The installer asks for a password for the `postgres` superuser; remember it. It installs as a Windows service that starts automatically. Then create Heimdall's user and database (Git Bash):
```bash
"/c/Program Files/PostgreSQL/16/bin/psql.exe" -U postgres -h 127.0.0.1   -c "CREATE USER heimdall WITH PASSWORD 'heimdall';" -c "CREATE DATABASE heimdall OWNER heimdall;"
```
Check: `"/c/Program Files/PostgreSQL/16/bin/psql.exe" "postgres://heimdall:heimdall@127.0.0.1:5432/heimdall" -c "select 1"` prints `1`.

No install alternative (what this project uses): **Supabase**. Project → Connect → **Session pooler** connection string (port 5432), with `?sslmode=require`, as `DATABASE_URL` in `server/.env`. Do not use the transaction pooler (port 6543): the migrations need a session-level advisory lock. The server creates its tables on first start and turns on row-level security for each of them (migration `002_rls.sql`), so Supabase's public Data API cannot read them; the server connects as the table owner and is not affected.

---

## 2. Fork tests: prove real exits work (≈5 min)

```bash
source ~/.heimdall-env
cd contracts
forge build
FORK_ERC4626_VAULT=0x7e97fa6893871A2751B5fE961978DCCb2c201E65 \
  forge test --match-contract ForkExits -vv
cd ..
```
**Pass** = the ForkExits tests show `[PASS]`, none `[SKIP]`. Save the output (it is evidence for the README).

---

## 3. Alerts: Telegram and Resend (≈20 min)

### 3.1 Telegram bot
1. In Telegram, open **@BotFather**, send `/newbot`, pick a name and a username ending in `bot` (for example `heimdall_guard_bot`).
2. BotFather replies with a token like `123456789:AA...`. Keep it secret.

### 3.2 Resend (email)
1. Sign up at https://resend.com → **API Keys** → **Create API key** (permission: Sending access). Copy it (`re_...`).
2. Sender address, pick one:
   - **With a domain (recommended):** **Domains** → **Add domain** → a subdomain you own (for example `mail.cyberpunkinc.xyz`) → add the DNS records Resend shows (TXT/MX) at your DNS host → wait for **Verified**. Then `EMAIL_FROM="Heimdall <alerts@mail.cyberpunkinc.xyz>"`.
   - **No domain:** `EMAIL_FROM="Heimdall <onboarding@resend.dev>"`. Resend then only delivers to the email you signed up with, which is fine for the demo if Ada's email is yours.

### 3.3 Fill in `server/.env`

Your `server/.env` started as a copy of `server/.env.example`; keep it that way and change values in a text editor. Every line, and what it should be for the fork demo:

| Line | Value for the fork demo | Status |
|---|---|---|
| `DATABASE_URL` | your Supabase session-pooler URL with `?sslmode=require` | **set by you**, tested OK |
| `HTTP_ADDR` | `:8080` | already right |
| `CORS_ORIGIN` | leave as is | already right |
| `RPC_HTTP_URL` | `http://127.0.0.1:8545`: the local fork, not Alchemy (the server talks to anvil, anvil talks to Alchemy) | already right |
| `RPC_WS_URL` | empty | already right |
| `START_BLOCK` | empty | already right |
| `TARGETS_FILE` | `../config/targets.arbitrum-one.json` | filled in 2026-10-02 |
| `SIGNALS_FILE` | `../config/signals.json` | already right |
| `BACKTESTS_DIR` | empty | already right |
| `FACTORY_ADDRESS` | empty (read from `DEPLOYMENT_FILE`) | already right |
| `DEPLOYMENT_FILE` | `../contracts/deployments/31337.json` (written by demo-fork.sh) | already right |
| `KEEPER_PRIVATE_KEY` | anvil test key #9 (public, fork only) | filled in 2026-10-02 |
| `DEMO_MODE` | `true` (turns on the simulator; it only works on the local fork) | filled in 2026-10-02 |
| `AUTH_SECRET` | a random 64-character string | filled in 2026-10-02 (generated) |
| `TELEGRAM_BOT_TOKEN` | the token from BotFather (3.1) | **you** |
| `TELEGRAM_BOT_USERNAME` | your bot's username, e.g. `heimdall_guard_bot` | **you** |
| `RESEND_API_KEY` | `re_...` from Resend (3.2) | **you** |
| `EMAIL_FROM` | `Heimdall <alerts@mail.yourdomain>` or `Heimdall <onboarding@resend.dev>` | **you** |
| `DEFAULT_TIP_CAP_USD` | `2` | already right |
| `LOG_LEVEL` | `info` (`debug` when something is wrong) | already right |

The Alchemy URLs do **not** go in `server/.env`; they go in `~/.heimdall-env` (1.4). Restart the server after any change to `server/.env`.

---

## 4. Run the demo on the Arbitrum One fork (≈30 min)

You need three Git Bash windows.

**Window 1: fork + contracts + seed**
```bash
source ~/.heimdall-env
bash scripts/demo-fork.sh
```
It starts anvil (a local copy of Arbitrum One, chain id 31337), deploys the factory, and gives Ada and Ben 10,000 USDC each in the Gauntlet vault (Ada also gets Aave). It ends by printing a block of `export` lines. If it fails, read `.local/anvil-fork.log`.

**Window 2: server**
`server/.env` already has everything (3.3), so you do not need the `export` lines demo-fork.sh prints. Just:
```bash
cd server
go run ./cmd/heimdalld
```
Check: `curl http://127.0.0.1:8080/healthz` answers.

**Window 3: web app**
```bash
cd web
pnpm install
pnpm dev
```
Open the URL it prints (http://localhost:5173).

**Wallet (MetaMask or Rabby)**
1. Add a network: name `Heimdall fork`, RPC `http://127.0.0.1:8545`, chain id `31337`, symbol `ETH`.
2. Import these two **public anvil test keys** (they hold nothing real; use a separate browser profile if you prefer):
   - Ada (#1, `0x7099…79C8`): `0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d`
   - Ben (#2, `0x3C44…93BC`): `0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a`

**Try the demo flow (docs/user_flow.md), as Ada**
1. Connect and sign in. The dashboard shows the Gauntlet vault and Aave positions.
2. **Settings** → link Telegram (opens your bot, press Start) → **Send test alert**. Add your email, verify it → **Send test email**.
3. **Protect** the vault position → **Activate**, sign. Card shows **Guarded**.
4. Try **Withdraw** and **Turn off** once to see they work, then protect again.
5. **Simulator** → **Fast drain** → **Run**. Expect: Watch → Warning → Critical, Telegram + email alert, exit with priority tip, then a partial exit and the rest on a retry.
6. Compare Ada (USDC back in wallet) with Ben (stuck).
7. **Reset** in the simulator to go back to the start.

**If the exit completes in one go (no partial step):** the drainers did not pull enough liquidity. The vault had about 470k USDC withdrawable on 2026-10-02 and the drainers hold ~532k, so it should go partial; if it does not, add more holders to `sim.drainers` (Morpho app → vault → Depositors, or https://arbiscan.io/token/0x7e97fa6893871A2751B5fE961978DCCb2c201E65#balances; plain wallets only), restart the server, Reset, run again.

**Optional automated check** (with the server and fork running):
```bash
API_URL=http://127.0.0.1:8080 python e2e/clickthrough.py
```

To start over: stop everything, `pkill anvil` (or close window 1), run `scripts/demo-fork.sh` again.

---

## 5. Deploy and verify on Arbitrum Sepolia (≈20 min)

1. **Two new wallets** (deployer and keeper). Never reuse anvil keys here:
   ```bash
   cast wallet new   # deployer: note address + private key
   cast wallet new   # keeper:   note address + private key
   ```
2. **Sepolia ETH for the deployer** (~0.01 ETH is plenty). Faucets: https://www.alchemy.com/faucets/arbitrum-sepolia or https://faucet.quicknode.com/arbitrum/sepolia. Or bridge Sepolia ETH at https://bridge.arbitrum.io.
3. **Arbiscan key:** https://etherscan.io/myapikey → create a key (Etherscan V2 keys work for Arbiscan).
4. Deploy:
   ```bash
   source ~/.heimdall-env
   PRIVATE_KEY=<deployer key> KEEPER_ADDRESS=<keeper address> ARBISCAN_API_KEY=<key> \
     bash scripts/deploy.sh sepolia
   ```
5. Get the addresses:
   ```bash
   cat contracts/deployments/421614.json
   ```
   Open `https://sepolia.arbiscan.io/address/<factory>` and check the **Contract** tab has a green check (verified). Do the same for `<implementation>`.
6. Put both addresses in **README.md → Deployments** (the Arbitrum Sepolia row), as Arbiscan links.

Optional (specs Q8): `bash scripts/deploy.sh one` deploys to Arbitrum One mainnet, needs real ETH, asks you to type `deploy`. Label it "experimental, unaudited" in the README.

---

## 6. Update the README and push

In `README.md` → **Status**: move the fork tests, fork demo, Telegram/Resend and Sepolia deployment from "Not done" to "Done, with evidence" with the command and result you saw. Then:
```bash
git add README.md contracts/deployments/421614.json
git commit -m "Sepolia deployment, fork demo verified"
git push
```
Never add `server/.env` or `~/.heimdall-env` (secrets).

---

## 7. Optional: TMX backtest (≈15 min, needs the archive RPC from 1.3)

Already worked out for you (2026-10-02) from the Rekt write-up and the attackers' transactions:

| Item | Value | How it was found |
|---|---|---|
| Exploited pool (GMX-fork Vault) | `0x67af542d497e302f576d5aabd60bc39c3e8e1cb4` | `vault()` of the TMX Router `0x18f340f493f37869fcdbb9565767b00f18b9e425`, which the attackers called 253 times |
| Tokens it holds | WETH `0x82aF49447D8a07e3bd95BD0d56f35241523fBab1`, WBTC `0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f`, USDC `0xaf88d065e77c8cC2239327C5EDb3A432268e5831`, USDT `0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9`, `0x2bcC6D6CdBbDC0a4071e48bb3b969b06B3330c07` (9 decimals) | the Vault's whitelisted tokens |
| Attackers | `0x763a67E4418278f84c04383071fC00165C112661`, `0x16Ed3AFf3255FDDB44dAa73B4dE06f0c2E15288d` | Rekt |
| Attack transactions | blocks 417,530,153 (Jan 3) to 418,024,624 (Jan 5, 03:19 UTC); the main burst is 417,991,135–418,024,624 (Jan 5, 01:00–03:19 UTC) | Blockscout tx lists of both attackers |

Main burst only (fast, ~1,200 points):
```bash
source ~/.heimdall-env
cd server
go run ./cmd/heimdalld backtest --incident tmx-2026-01 --title "TMX, January 2026" \
  --mode balance --holder 0x67af542d497e302f576d5aabd60bc39c3e8e1cb4 \
  --token 0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9,0xaf88d065e77c8cC2239327C5EDb3A432268e5831,0x82aF49447D8a07e3bd95BD0d56f35241523fBab1,0x2f2a2543B76A4166549F7aaB2e75Bef0aefC5B0f \
  --rpc "$ARBITRUM_ONE_RPC_URL" --from 417980000 --to 418030000 --step 40 \
  --source "https://alpha.rekt.news/tmztribe-rekt, https://arbiscan.io/address/0x67af542d497e302f576d5aabd60bc39c3e8e1cb4, https://arbiscan.io/address/0x763a67E4418278f84c04383071fC00165C112661" \
  --out ../config/backtests/tmx-2026-01.json
cd ..
```
Then open the **Backtest** page in the app (no restart needed). If it says it cannot read a block, your RPC is not archive: skip this step, the page stays hidden. If it works, commit `config/backtests/tmx-2026-01.json`.

---

## 8. Deadline, video, submit

1. **Deadline:** open the buildathon page on HackQuest and check the timezone of "Oct 4, 2026, 15:59". If it is UTC+8 (Singapore), that is 07:59 UTC. Write the confirmed time in `CLAUDE.md` and the README.
2. **Record the video (under 4 minutes)** following `docs/user_flow.md`. Shot list:
   - Dashboard with Ada's positions (0:00–0:20)
   - Protect → the "can only send this money back to you" line → Activate (0:20–0:50)
   - Withdraw / Turn off work (quick, 0:50–1:05)
   - Simulator → Fast drain: bands go Warning → Critical, phone shows the Telegram alert, inbox shows the email (1:05–2:00)
   - Exit transaction with the priority tip, partial exit then the rest (2:00–2:40)
   - Ada vs Ben comparison (2:40–3:05)
   - Backtest page, only if step 7 worked (3:05–3:25)
   - Arbiscan: verified factory and implementation on Sepolia (3:25–3:50)
   Say once: "simulated on an Arbitrum One fork; on Arbitrum One the tip buys priority".
3. Upload (YouTube unlisted or Loom), put the link in README.md ("Demo video:"), commit, push.
4. Submit on HackQuest: repo URL, video link, Sepolia contract links.

---

## Quick troubleshooting

| Problem | Fix |
|---|---|
| `forge: command not found` | Open a new Git Bash window after `foundryup`, or add `~/.foundry/bin` to PATH. |
| demo-fork.sh: "Something already answers at 127.0.0.1:8545" | `pkill anvil` (or close the old window), run again. |
| demo-fork.sh: USDC source "holds only X USDC" | Pick another big holder from https://arbiscan.io/token/0xaf88d065e77c8cC2239327C5EDb3A432268e5831#balances (a plain wallet, not a contract), put it in `demo.usdcSource`. |
| Fork is slow or errors with 429 | You are on the public RPC. Use your Alchemy/QuickNode URL. |
| Wallet shows wrong nonce / stuck tx after restarting the fork | MetaMask → Settings → Advanced → Clear activity tab data. |
| Server: cannot reach Postgres | Start the service: PowerShell as admin, `Start-Service postgresql-x64-16`. Check `DATABASE_URL` in `server/.env`. |
| No Telegram message | Did you press Start in the bot chat from the Settings link? Is the token in `server/.env`? Restart the server after editing `.env`. |
| No email | Without a verified domain, Resend only sends to your Resend account email. |
| deploy.sh: verification failed | Re-run the same command; Arbiscan sometimes needs a minute after deployment. |
