# Heimdall: instructions for coding agents

Heimdall is the exit guard for Arbitrum depositors (Arbitrum Open House Singapore buildathon; submission deadline Oct 4, 2026, 15:59, timezone to confirm).

## Read first, in this order
1. `docs/specs.md`: strict requirements. It overrides everything else.
2. `docs/instructions.md`: environment setup, scaffolding commands, folder layout, verification gate.
3. `docs/details.md`: end-to-end design.
4. `docs/user_flow.md`: the demo flow the product must support.
5. `docs/UI_UX.md`: design direction and the interaction contract for the web app.

## Always
- In cloud sessions, run `bash scripts/cloud-setup.sh && source ~/.heimdall-env` first.
- Separate folders: `contracts/` (Foundry), `server/` (Go 1.26), `web/` (SvelteKit, Svelte 5, TypeScript, Tailwind, **pnpm only**).
- Minimal tests (see specs §3). Accuracy through the verification gate in `docs/instructions.md` §4: no "done" without fresh command output as evidence.
- Every visible UI element must work (see `docs/UI_UX.md` §5). Build only the elements in `docs/UI_UX.md` §6.
- Never guess third-party addresses, ABIs or library APIs: read the source or official docs.
- Never commit secrets. `.gitignore` covers `.env` and `node_modules`; all markdown docs are committed except the local-only guides listed in `.gitignore`.
- Commit and push small working steps. Keep `README.md` → "Status" current, including what the builder must do locally.
