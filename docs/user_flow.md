# Heimdall: Demo Flow (Happy Path)

> Audience: coding agents building the demo, and the person recording the demo video.
> Companion files: `details.md` (full end-to-end design) and `specs.md` (strict requirements).
> This file describes the **demo flow**, starting from the dashboard (no landing page in the video).

---

## 0. One-line summary

Heimdall watches a depositor's Arbitrum vault positions, detects exploit and collapse signals, and automatically moves the depositor's money back to their own wallet before the drain finishes, jumping the exit queue with a priority fee when it matters.

## 1. Demo setup (before recording)

| Item | Setup |
|---|---|
| Chain for the live demo | Local **Arbitrum One fork** (Anvil) so real protocol code (Morpho/Euler ERC-4626 vaults, Aave V3) runs, and an attack can be simulated safely. |
| Chain for the "it's real" moment | Heimdall contracts **deployed and verified on Arbitrum Sepolia** (required by the buildathon) and, if approved (see `details.md` §20 Q8), **also on Arbitrum One mainnet**. Show Arbiscan links at the end. |
| Demo wallets | **Wallet A ("Ada")**: protected by Heimdall. **Wallet B ("Ben")**: same position, not protected. Both hold the same deposit in the same vault so the video can compare outcomes side by side. |
| Positions | Ada and Ben each hold ~10,000 USDC deposited in the same real ERC-4626 lending vault on the fork (a Morpho vault on Arbitrum One). Ada also holds ~5,000 USDC in Aave V3 (to show two protocols). |
| Attack script | A fork-only "Incident Simulator" that can run pre-scripted scenarios against the vault (see step 5). Clearly labeled "Simulated on an Arbitrum One fork". |
| Alerts | Telegram bot and email (Resend) connected to Ada's account so the phone notification and inbox alert can be shown on screen. |

Target video length: 3 to 4 minutes.

---

## 2. Scene 1: Dashboard (Ada is connected)

**What the viewer sees**
- Header: "Heimdall: the exit guard for Arbitrum depositors".
- Card per detected position, pulled from Ada's wallet:
  - "Morpho vault (USDC) · 10,000 USDC · **Unprotected**" with a **Protect** button.
  - "Aave V3 (USDC) · 5,000 USDC · **Unprotected**" with a **Protect** button.
- Each card shows a small live **risk strip**: oracle deviation, outflow rate, collateral peg, liquidity (utilization), config changes. All green.

**Narration (suggested)**: "In 2026, Arbitrum protocols lost over $50M to hacks. Most depositors found out after the money was gone. Heimdall is the guard that gets you out first."

---

## 3. Scene 2: Turn on protection

1. Ada clicks **Protect** on the Morpho position.
2. A **Protection Policy** panel opens with plain-English choices (defaults pre-filled):
   - When risk is **Critical**: `Exit automatically` (default) / `Ask me first`.
   - When risk is **Warning**: `Notify me` (default) / `Exit half` / `Exit fully`.
   - **Send my money to**: `My wallet, as USDC` (default) / `My wallet, swapped to USDG` (only shown if a USDG route exists on the chain).
   - **Speed**: `Priority exit, pay up to $X extra` (default on, small cap). Tooltip: "Arbitrum lets you bid for priority on each transaction. During a bank-run, first out wins."
   - **Safe address**: shows Ada's own wallet address (locked; cannot be changed to any other address in the MVP).
3. Ada clicks **Activate**. The wallet asks her to sign:
   - Tx 1: create her personal **Guard** (her own small contract; only she owns it).
   - Tx 2: move her vault shares into her Guard (approval + deposit, batched where possible).
4. Card flips to **Guarded** (shield icon), showing: "Heimdall can only send this money back to you. You can withdraw or turn this off anytime."

**Must show on screen**: the "can only send back to you" guarantee, and a **Withdraw / Turn off** button that works.

---

## 4. Scene 3: Guarded state (short)

- Risk strip is live and updating (sequencer feed, near-real-time).
- An **Activity** feed shows "Watching: 5 signals · last check 0.3s ago".
- Ben's card (shown in a split view or a second tab) displays the same vault, **Unprotected**.

---

## 5. Scene 4: The attack (Incident Simulator)

1. Presenter opens the **Incident Simulator** panel (visible only in demo mode on the fork).
2. Selects scenario **"Fast drain"**, modeled on real 2026 Arbitrum incidents (a slow, multi-transaction drain like TMX in January 2026), and clicks **Run**.
   - The script starts a sequence of attacker transactions that pull liquidity out of the vault block by block.
3. On Ada's dashboard, in real time:
   - **Outflow rate** turns amber then red.
   - **Liquidity** drops (utilization climbing) because money is leaving fast.
   - Status moves **Watch → Warning → Critical**, with a timestamped reason for each step, e.g. "Vault lost 18% of assets in 4 blocks (threshold 10%)".
4. Telegram alert pops on Ada's phone, and the same alert lands in her email: "⚠️ Critical risk on your Morpho USDC vault. Exiting now."

(Alternative scenarios available in the simulator, pick one for the video, keep others for Q&A: **Oracle tampering** modeled on Ostium July 2026, **Collateral depeg** modeled on Stream Finance November 2025.)

---

## 6. Scene 5: Heimdall exits

1. Activity feed: "Exit submitted · priority tip applied · block N+1". (On the local fork the tip is attached but the auction ordering is not reproduced; the narration should say "on Arbitrum One this tip buys priority".)
2. If the vault does not have enough free liquidity for the full amount, the feed shows: "Partial exit: 6,200 USDC withdrawn (all available). Retrying for the rest every block." Then "Remaining 3,800 USDC withdrawn at block N+3."
   (If full liquidity is available, it exits in one go. The simulator should be tuned so the partial-then-complete path is visible, because that is the differentiator.)
3. Ada's card: **Exited · 10,000 USDC returned to your wallet** with links to the exit transactions.
4. Heimdall's contract emits an **on-chain exit receipt** (event) recording the trigger reason hash, amount, and blocks; the dashboard shows it.

---

## 7. Scene 6: The comparison

- Split screen: **Ada: 9,990 USDC safe** (minus gas and tip) vs **Ben: position now worth X** (after the simulated drain completes).
- Timeline chart: first signal → exit submitted → exit confirmed → drain finished. Caption: "Heimdall exited 2 blocks after the first signal. The drain continued for another N blocks."

---

## 8. Scene 7: "Would it have worked in real life?" (Backtest page)

- Page: **Replay a real hack**.
- Select "TMX, January 2026 (Arbitrum)".
- A chart plots the protocol's real on-chain outflows over time, with a marker where Heimdall's detection rules would have fired and the % of funds still in the pool at that moment.
- Caption: "Heimdall's rules, run over the real on-chain history of this incident."

(If time runs out before submission, this scene becomes a static chart generated offline by the same detection engine. It must use real historical on-chain data, not invented numbers.)

---

## 9. Scene 8: Close

- Show Heimdall contracts on **Arbiscan (Arbitrum Sepolia, plus Arbitrum One if deployed)**, verified.
- Three bullets on screen:
  1. "Real protocols, real exits: Morpho/Euler vaults and Aave V3 on Arbitrum One."
  2. "Your money can only come home: the keeper cannot send funds anywhere else."
  3. "First out wins: priority exits using Arbitrum's per-transaction priority fees."
- End card: "Heimdall · the exit guard for Arbitrum depositors".

---

## 10. Not in the main demo (mention only if asked)

- **Arbitrum Security Council freeze**: Heimdall cannot trigger it (it is a human, multisig emergency decision). A future "Incident Pack" export (attacker addresses, tx hashes, timeline) could help victims and protocols ask for one faster.
- **Fast Feed** (paid early transaction feed on Arbitrum One): production upgrade for earlier detection; not needed for the demo.
- **Pooled "protected vault token"** version and **GMX pool** support: roadmap.
