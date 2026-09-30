# Heimdall: UI/UX Rules for Coding Agents

> Read with `docs/user_flow.md` (what screens exist and what happens on them) and `docs/specs.md` (wins on conflicts).
> Sections 2–4 adapt Anthropic's "frontend-design" skill (github.com/anthropics/skills, Apache-2.0, condensed and modified for this project). Sections 5–7 are project rules.
> Two goals, in order: **(1) every visible element works; (2) the design looks deliberate, not generated.**

---

## 1. The brief

- **Subject:** a guard that watches your money in Arbitrum lending vaults and gets it out when an attack starts. Named after Heimdall, the Norse watchman at the bridge to Asgard who sees everything and sounds the horn.
- **Audience:** crypto-native depositors, not security experts. They want plain answers.
- **Primary job of the UI:** in one glance, answer "Is my money safe? If not, is Heimdall handling it?" Then make turning protection on feel simple and trustworthy.
- **Secondary job (demo):** make a live attack and an automatic exit easy to follow on camera.

## 2. Design direction (proposal: review it before building)

This is a starting proposal. Do the two-pass review in §3 before writing code, and say in the commit message what you changed and why.

**Concept: "the watchtower at first light".** A cool, clear, daylight interface (not the dark crypto-dashboard default). Calm and quiet everywhere, with the boldness spent in one place: the **horn band**.

**The one memorable element: the horn band.** Each position card has a full-width status band across its top. It shows the status word large in the expanded width of the typeface ("Calm", "Warning", "Critical", "Exiting", "Home safe") and changes color with severity. When status escalates, the band does a single sweep animation (respecting `prefers-reduced-motion`). Nothing else on the page animates by itself.

**Color tokens (define as CSS variables / Tailwind theme):**

| Token | Hex | Use |
|---|---|---|
| `frost` | `#EEF3F5` | Page background |
| `ink` | `#1B2A33` | Primary text |
| `granite` | `#6B7C86` | Secondary text, borders |
| `fjord` | `#1F5F8B` | Primary actions, links, "Home safe" |
| `calm` | `#3E8E6E` | Calm status only |
| `amber` | `#C9831A` | Warning status only |
| `ember` | `#B8372B` | Critical / Exiting status only |

Severity colors appear **only** where they mean severity (horn band, risk strip, alerts). Check contrast: text on each band color must meet WCAG AA.

**Type:** one family, **Archivo** (variable, has a width axis), from `@fontsource-variable/archivo`. Normal width for body and UI; expanded width, heavier weight for the horn band's status word and page titles. Amounts and addresses use `font-variant-numeric: tabular-nums` (no monospace font for data labels). Clear type scale; line length under 80 characters.

**Layout:** left-aligned. Dashboard = one column of position cards (the main content) and, on wide screens, a right rail with the Activity feed. Mobile stacks everything in one column. Only position cards are "cards"; everything else is plain sections and lists.

**Avoid in this project:** gradients as decoration; identical rounded cards for every section; the same soft shadow everywhere; ALL-CAPS labels; eyebrow labels above headings; middle-dot meta strings ("A · B · C"); "→" in button text; single highlighted words in headlines; fake stats, testimonial or "trusted by" blocks.

## 3. Design process (two passes)

1. **Plan:** write a compact token system (4–6 named colors, typefaces and roles, layout concept with a small ASCII wireframe per page, 3 principles). Start from §2.
2. **Review against the brief:** for each choice, ask whether it is specific to Heimdall or just the default you would produce for any dashboard. Revise the generic parts, then build.
3. **Critique while building:** take screenshots (Playwright) and look at them. Before finishing, remove one decoration you don't need.

Known "generated design" tells to avoid unless the brief asks for them: warm cream background with a terracotta accent; near-black with one acid-green or vermilion accent; broadsheet hairline layouts; the SaaS card kit (identical rounded cards, one radius, soft grey shadow, gradient washes); template chrome (tracked ALL-CAPS eyebrows, middle-dot meta strings, "WORD — fragment" labels, near-black standing in for black, monospace for small data labels, "→" on buttons).

## 4. Writing in the interface

- Words exist to help people understand and act. Plain English, sentence case, active voice, no filler.
- Name things the way users think: "Turn off protection", not "Disable keeper". "Exit now", not "Execute".
- A button says exactly what happens ("Activate protection", "Withdraw to my wallet"), and the same action keeps the same name through the flow (button "Activate protection" → toast "Protection active").
- Errors say what happened and how to fix it, never apologize and are never vague ("Your wallet is on Ethereum. Switch to Arbitrum One to continue." with a **Switch network** button).
- Empty states invite the next action ("No supported positions in this wallet. Deposit into a supported vault, then come back." with the list of supported vaults).
- Always visible on protected positions: **"Heimdall can only send this money back to you."**

## 5. The interaction contract (non-negotiable)

These rules exist because generated UIs often ship controls that look real but do nothing.

1. **Every visible interactive element must do something real and observable.** Buttons, links, inputs, toggles, selects, tabs, search and filter fields all must work end to end. If a feature is not built, **do not render its control**. No "coming soon", no dead nav items, no decorative inputs.
2. **Only build the elements listed in §6.** Do not add extra widgets, stats, charts, filters, search bars, avatars, notification bells or settings that are not listed. If you believe an element is missing, add it to §6 in the same commit with a one-line reason.
3. **If you add a search or filter, it must actually filter** the visible data, handle no-results with an empty state, and be clearable.
4. **Every action has four states:** idle, pending (with what is happening: "Waiting for wallet signature", "Confirming on Arbitrum"), success (what changed), and error (what went wrong + how to fix). Double-submits are prevented while pending.
5. **Disabled controls explain why** (tooltip or helper text), and there is a visible path to enable them.
6. **Wallet states are handled everywhere:** not installed, not connected, wrong network (offer "Switch network"), connected, user rejected the request (show a calm message, keep the form state).
7. **Data states are handled everywhere:** loading (skeleton for the known layout, not spinners everywhere), empty, error with retry, and live updates that do not cause layout jumps.
8. **Links go somewhere real.** Explorer links open the correct transaction/address page in a new tab. Internal links route to existing pages.
9. **Forms validate inline** (e.g., the tip cap must be a positive number within limits) and never lose user input on error.
10. **Keyboard and screen readers:** every control reachable by Tab, visible focus ring, labels on inputs, `aria-live` for status changes in the horn band and Activity feed.
11. **Responsive** down to 375 px wide with no horizontal scroll. Respect `prefers-reduced-motion`.
12. **Demo-only UI** (Incident Simulator) appears only in demo mode and is labeled "Simulated on an Arbitrum One fork".

## 6. Element inventory (build exactly these)

**Global**
- Header: product name, Connect wallet button / connected address menu (copy address, disconnect), network indicator with Switch network when wrong.
- Navigation: Dashboard, Activity, Backtest (only if built with real data), Settings, Simulator (demo mode only).

**Dashboard**
- Position cards (one per supported position): horn band with status word, protocol + asset + amount, live risk strip (6 signals with short plain-English labels and tooltips), status (Unprotected / Guarded / Exiting / Exited).
- Card actions by status: **Protect** (Unprotected); **View details**, **Turn off protection**, **Withdraw to my wallet** (Guarded); **View exit** (Exited).
- Guarantee line on Guarded cards.
- Activity rail (wide screens): latest 10 events with time and link to the full Activity page.
- Empty state (no supported positions) listing supported vaults.

**Protect panel** (drawer or modal)
- Policy controls from `docs/user_flow.md` Scene 2 (Critical action, Warning action, send-to asset, priority exit toggle + tip cap input, safe address shown read-only).
- Two-step progress: "Create your Guard" → "Move your position into it", each with pending/success/error and retry.
- Buttons: **Activate protection**, **Cancel**.

**Position detail**
- Horn band, amounts, current policy with **Edit policy**.
- Signal history (simple timeline or chart of the 6 signals with thresholds).
- Exit receipts (amount, reason, blocks, explorer links).
- Actions: **Exit now**, **Pause Heimdall** / **Resume**, **Turn off protection**, **Withdraw to my wallet**.

**Activity**
- Chronological list of checks, severity changes, alerts and exits, with explorer links.
- One filter control: by position (All / each position). It must work.

**Simulator** (demo mode only)
- Scenario select (Fast drain, Oracle tampering, Collateral depeg), **Run scenario**, **Reset fork**, live progress log, label "Simulated on an Arbitrum One fork".

**Backtest** (only with real data)
- Incident select (only incidents that have real data loaded), chart of real outflows with the marker where Heimdall's rules fire, one-sentence caption, data source link.

**Settings**
- Telegram: **Connect Telegram** (deep link + code), connected state, **Disconnect**, **Send test alert**.
- Email: email input, **Send code**, code input, **Verify**, verified state, **Send test email**.
- Default policy for new positions (same controls as the Protect panel), **Save**.

## 7. UI verification (required before saying the UI is done)

Write `e2e/clickthrough.py` (Python Playwright, headless Chromium; browsers are preinstalled, do not run `playwright install`). It must:
1. Start or connect to the running web app (and server) and visit **every route** in §6.
2. On each page, wait for `networkidle`, take a full-page screenshot to `e2e/screenshots/`, and collect console errors.
3. Find every visible `button`, `a`, `input`, `select`, `[role=tab]`, `[role=switch]` and exercise it (click, type a valid value, choose an option). For each one, assert something observable changed: URL, DOM content, a request was made, a dialog opened, or a validation message appeared.
4. Fail if any element produces no observable change, any page has console errors, or any link is broken.
5. Use a mocked injected wallet provider (a small `window.ethereum` stub that returns a fixed address and chain id) so wallet-gated screens can be reached without a real wallet.

Run it, fix what fails, and **look at the screenshots** yourself against §2–§5 before claiming the UI is done. Report the result with evidence (per `docs/instructions.md` §4).
