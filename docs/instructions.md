# Heimdall: Build Instructions for Coding Agents

> Read after `docs/specs.md` (wins on conflicts) and `docs/details.md`. The UI rules are in `docs/UI_UX.md`.
> The builder's style: hackathon speed, **minimal tests**, but **few errors**. Accuracy comes from the verification gate below, not from test volume.

---

## 1. Environment (cloud sessions)

Run this first, every new session:

```bash
bash scripts/cloud-setup.sh && source ~/.heimdall-env
```

It gives you **Go 1.26** (built from source, ~5 min the first time), **forge / anvil / cast** (from npm) and **solc** (solc-js behind a wrapper), and sets `GOPROXY=direct`, `GOSUMDB=off`, `GOTOOLCHAIN=local`, `FOUNDRY_SOLC`, `FOUNDRY_OFFLINE=true`. Put `source ~/.heimdall-env` before any `go`/`forge`/`anvil` command in a new shell.

Known cloud limits (do not try to work around them):
- **No Arbitrum RPC access** (mainnet and Sepolia are blocked). You cannot run fork tests, the demo fork, or deployments. Write them, gate them on env vars, and leave them for the builder.
- **No Google Fonts**. Use self-hosted fonts from npm (`@fontsource-variable/...`).
- Package installs: npm registry works (use **pnpm**), Go modules work via `GOPROXY=direct`, GitHub `git clone` works.
- Playwright for Python and Chromium are preinstalled (`PLAYWRIGHT_BROWSERS_PATH=/opt/pw-browsers`); do not run `playwright install`.

Local development by the builder does not use `cloud-setup.sh`; they have Go 1.26, pnpm and native Foundry.

## 2. Repository layout (separate folders)

```
contracts/   Foundry (Solidity 0.8.28)
server/      Go 1.26 backend (heimdalld)  + Dockerfile + .dockerignore + .gitignore
web/         SvelteKit + Svelte 5 + TypeScript + Tailwind (pnpm only) + .gitignore
e2e/         Playwright click-through check (Python)
config/      targets.arbitrum-one.json (addresses with `source` fields)
scripts/     cloud-setup.sh
docs/        specs.md, details.md, user_flow.md, instructions.md, UI_UX.md
```

Frontend code never goes in `server/`, backend code never goes in `web/`.

## 3. Scaffolding (non-interactive commands only)

Never run a command that waits for interactive input. If a CLI prompts, cancel it and pass the missing flag.

### 3.1 Web (SvelteKit)

From the repo root:

```bash
pnpm dlx sv create web --template minimal --types ts \
  --add tailwindcss="plugins:typography,forms" eslint prettier \
  --install pnpm
```

If the CLI rejects the combined plugin syntax, run the create command with `--add tailwindcss eslint prettier`, then inside `web/` run `pnpm dlx sv add tailwindcss="plugins:typography,forms"`. If that also fails, `pnpm add -D @tailwindcss/typography @tailwindcss/forms` and register both with `@plugin` lines in `src/app.css` (Tailwind v4).

Rules for `web/`:
- **pnpm only** (never npm or yarn). Commit `pnpm-lock.yaml`.
- **Svelte 5 runes only** (`$state`, `$derived`, `$effect`, `$props`). No legacy `export let` or `$:` syntax.
- TypeScript everywhere (`lang="ts"`); `pnpm check` (svelte-check) must report 0 errors.
- ESLint + Prettier from the scaffold: `pnpm lint` and `pnpm format`.
- Chain access with **viem**; wallet via the injected EIP-1193 provider (MetaMask/Rabby). Add a wallet library only if strictly needed.
- Fonts from `@fontsource-variable/*` (see `docs/UI_UX.md`).
- `web/.gitignore` must include `.env` and `node_modules` (keep the scaffold's other defaults).

### 3.2 Server (Go)

```bash
mkdir -p server && cd server && go mod init github.com/AlphaTechini/heimdall/server
```

- `go.mod` **must** say `go 1.26`.
- Keep dependencies few and boring: `github.com/ethereum/go-ethereum` (ethclient, ABI), `github.com/jackc/pgx/v5` (Postgres), `github.com/resend/resend-go/v2` (email), stdlib `net/http` routing (Go 1.22+ patterns), a small WebSocket library (`github.com/coder/websocket`). Telegram via its plain HTTP Bot API (no SDK).
- Single binary `heimdalld` at `cmd/heimdalld/main.go`; packages under `internal/` as in `docs/details.md` §17.
- `server/Dockerfile`: multi-stage, `golang:1.26-alpine` builder → small runtime image (`gcr.io/distroless/static` or `alpine`), non-root user, `CGO_ENABLED=0`.
- `server/.dockerignore`: `.env`, `*.env`, `.git`, `bin/`, `tmp/`, `*.md`.
- `server/.gitignore`: `.env` (plus the built binary, e.g. `/heimdalld`, `/bin`).
- Config only via env vars; ship `server/.env.example` listing every variable.

### 3.3 Contracts (Foundry)

```bash
source ~/.heimdall-env
forge init contracts --no-git        # if the flag is unsupported, init in a temp dir and copy
cd contracts
forge install foundry-rs/forge-std OpenZeppelin/openzeppelin-contracts --no-git
```

If `forge install` fails, add `@openzeppelin/contracts` with pnpm inside `contracts/` and use remappings. Pin `solc = "0.8.28"` in `foundry.toml` for local builds (in the cloud, `FOUNDRY_SOLC` overrides it). `contracts/.gitignore`: `.env`, `out/`, `cache/`, `broadcast/*/31337/`.

### 3.4 Git ignore policy (builder's rule)

Ignore only secrets and dependencies/build output: `.env` files and `node_modules` (plus toolchain build dirs listed above). **All docs and markdown files are committed.** Provide `.env.example` files instead of secrets.

## 4. The verification gate (do this before claiming anything works)

Adapted from the "verification-before-completion" skill (obra/superpowers, MIT).

**Iron law: no completion claim without fresh verification evidence.** If you did not run the check in this step, you cannot say it passes.

For every claim:
1. **Identify** the command that proves it.
2. **Run** the full command, fresh.
3. **Read** the whole output: exit code, error and warning counts.
4. **Verify** the output actually confirms the claim. If not, report the real state.
5. Only then state the claim, quoting the evidence (e.g., "`pnpm check`: 0 errors, 0 warnings").

Red flags that mean stop and verify: "should work", "probably", "looks right", "Done!" before running anything, trusting a subagent's report without checking the diff, partial checks.

### 4.1 Required checks per area

| Area | Commands that must pass before moving on |
|---|---|
| Contracts | `forge build` (0 errors), `forge test` (unit tests green; fork tests skipped cleanly when `ARBITRUM_ONE_RPC_URL` is unset) |
| Server | `go build ./...`, `go vet ./...`, then actually start `heimdalld` against a local `anvil` (no fork) and a Postgres, and call `GET /healthz` and one real endpoint |
| Web | `pnpm check` (0 errors), `pnpm lint` (0 errors), `pnpm build` (exit 0) |
| UI behavior | `e2e/clickthrough.py` passes (see `docs/UI_UX.md` §7): every page loads, every interactive element does something observable, 0 console errors; review the screenshots yourself |
| Docker | `docker build server/` if Docker is available; otherwise say it was not verified |
| Requirements | Re-read `docs/specs.md`; make a checklist of every MUST touched by the change; mark each done with evidence or list it as a gap |

### 4.2 Accuracy habits (to reduce the builder's debugging)

- **Never write from memory what you can read.** Before using a library API, open its source in the Go module cache (`$(go env GOMODCACHE)`) or `node_modules` and confirm the exact function signature. This matters most for go-ethereum, viem, Svelte 5 and SvelteKit.
- **Never invent addresses, ABIs or event signatures.** Addresses go in `config/` with a `source` field; unknown ones stay as clearly marked placeholders.
- ABIs for Heimdall contracts come from `contracts/out/` (generated), never hand-written copies.
- Keep types shared and generated where possible (e.g., the frontend imports ABIs from a generated file).
- After finishing a module, re-read the related spec section line by line and grep your code for `TODO`, `FIXME`, `placeholder`, `mock` and `console.log`. Anything left must be intentional and listed in the README Status.
- Handle errors on every path (RPC failures, reverted txs, user rejecting a wallet prompt, empty DB). An unhandled error path is a bug even if the happy path works.
- Prefer the simplest thing that satisfies the spec. No speculative abstractions, no features not in the spec.

## 5. Order of work

Follow `docs/details.md` §18: contracts → server (watcher, signals, executor, API, notifications) → web (following `docs/UI_UX.md`) → simulator + backtest → deployment scripts → README. Commit and push after each working step (small commits, clear messages). Never commit secrets.

## 6. End-of-session report (in `README.md` → "Status")

Keep an up-to-date **Status** section with:
- What is done, each item with the verification evidence (command + result).
- What is not done or not verified in the cloud.
- **What the builder must do locally**, step by step: set RPC URLs, run fork tests, run the demo fork, fill contract addresses, create the Telegram bot and Resend key, deploy and verify on Arbitrum Sepolia (and Arbitrum One if chosen), record the video.
