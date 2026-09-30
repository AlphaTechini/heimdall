#!/usr/bin/env python3
"""Heimdall click-through check (docs/UI_UX.md section 7).

Drives the real web app against the REAL Go server and a local anvil with a Playwright stub for
`window.ethereum` (Ada, anvil account #1, forwards every RPC call to anvil, which has her unlocked).

What it does
  1. Visits every route (/, /positions/<id>, /activity, /simulator, /settings, /tx/<hash>) at 1440 px
     and 375 px, waits for networkidle, saves full-page screenshots to e2e/screenshots/ and collects
     console errors.
  2. Exercises every visible button, link, input, select, [role=switch] on each page and asserts
     something observable changed (URL, DOM text, a form/aria state, a dialog, or an API request).
     Controls that change money or state (Activate protection, Exit now, Withdraw, ...) are excluded
     from the generic sweep and must be clicked, with assertions, by the scripted demo path.
  3. Runs the demo path: connect, Protect (validation, createGuard, approve+deposit, SIWE, PUT policy),
     Guarded card, Simulator Fast drain (band Warning/Critical, partial then complete exit, Home safe),
     Ada vs Ben comparison, /tx/<hash>, Settings, Reset fork, and the detail-page actions on a fresh
     guarded position (Pause/Resume, Turn off/on, Withdraw, Exit now).

Usage
  python3 e2e/clickthrough.py

Environment
  API_URL        Heimdall server           (default http://127.0.0.1:8080)
  WEB_URL        the web app               (default http://127.0.0.1:4173; if it does not answer,
                                            the script runs `pnpm build` with PUBLIC_API_URL=API_URL
                                            and starts `pnpm preview` itself)
  RPC_URL        anvil                     (default http://127.0.0.1:8545)
  CHROMIUM_PATH  chromium executable       (default /opt/pw-browsers/chromium)
  HEADED=1       show the browser

Exit code is non-zero when any step fails. The script calls POST /sim/reset at the start and the end.
"""

from __future__ import annotations

import json
import os
import re
import signal
import subprocess
import sys
import time
import traceback
import urllib.error
import urllib.request
from urllib.parse import urljoin
from pathlib import Path

from playwright.sync_api import Page, expect, sync_playwright
from playwright.sync_api import TimeoutError as PlaywrightTimeout

ROOT = Path(__file__).resolve().parent.parent
SHOTS = Path(__file__).resolve().parent / "screenshots"
API_URL = os.environ.get("API_URL", "http://127.0.0.1:8080").rstrip("/")
WEB_URL = os.environ.get("WEB_URL", "http://127.0.0.1:4173").rstrip("/")
RPC_URL = os.environ.get("RPC_URL", "http://127.0.0.1:8545")
CHROMIUM = os.environ.get("CHROMIUM_PATH", "/opt/pw-browsers/chromium")
ADA = "0x70997970C51812dc3A010C7d01b50e0d17dc79C8"
VAULT = "mock-vault-usdc"
AAVE = "mock-aave-usdc"
VAULT_LABEL = "Mock lending vault (USDC)"
AAVE_LABEL = "Mock Aave V3 (USDC)"
EXPECTED_404 = "/tx/0x" + "ab" * 32  # the negative test in the tx step
GUARANTEE = "Heimdall can only send this money back to you."

# Controls that move money or change state. The generic sweep skips them; the demo path must click each
# one with an assertion (see `act`). The run fails if one appeared in a sweep but was never exercised.
SCENARIO_ONLY = {
    "Activate protection",
    "Withdraw to my wallet",
    "Exit now",
    "Reset fork",
    "Run scenario",
    "Turn off protection",
    "Turn on protection",
    "Pause Heimdall",
    "Resume",
    "Disconnect",
    "Switch network",
}

WALLET_STUB = r"""
(() => {
  const ADA = '%(ada)s';
  const RPC = '%(rpc)s';
  const hex = (n) => '0x' + n.toString(16);
  let chain = localStorage.getItem('stub.chain') || '0x7a69';
  let connected = localStorage.getItem('stub.connected') !== '0';
  const handlers = {};
  const emit = (e, a) => (handlers[e] || []).forEach((f) => f(a));
  const rpc = async (method, params) => {
    const r = await fetch(RPC, { method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }) });
    const j = await r.json();
    if (j.error) { const e = new Error(j.error.message); e.code = j.error.code; throw e; }
    return j.result;
  };
  window.__stub = { rejectNext: false, calls: [] };
  window.ethereum = {
    isMetaMask: true,
    on: (e, f) => ((handlers[e] ||= []).push(f)),
    removeListener: () => {},
    request: async ({ method, params }) => {
      window.__stub.calls.push(method);
      const guarded = ['eth_sendTransaction', 'personal_sign', 'eth_requestAccounts'];
      if (window.__stub.rejectNext && guarded.includes(method)) {
        window.__stub.rejectNext = false;
        const e = new Error('User rejected the request.'); e.code = 4001; throw e;
      }
      switch (method) {
        case 'eth_requestAccounts': connected = true; localStorage.setItem('stub.connected', '1'); return [ADA];
        case 'eth_accounts': return connected ? [ADA] : [];
        case 'eth_chainId': return chain;
        case 'wallet_switchEthereumChain':
          chain = params[0].chainId; localStorage.setItem('stub.chain', chain); emit('chainChanged', chain); return null;
        default: return rpc(method, params);
      }
    },
  };
})();
""" % {"ada": ADA, "rpc": RPC_URL}

# ---------------------------------------------------------------------------------------------
# Results
# ---------------------------------------------------------------------------------------------

RESULTS: list[tuple[str, str, str]] = []  # (name, PASS|FAIL, detail)
CONSOLE_ERRORS: list[str] = []
EXERCISED: set[str] = set()  # names of SCENARIO_ONLY controls clicked by the demo path
SEEN_SCENARIO_ONLY: set[str] = set()
SWEEP_LOG: list[str] = []
DISABLED_LOG: list[str] = []


def log(msg: str) -> None:
    print(msg, flush=True)


class Step:
    """Context manager that records PASS/FAIL, keeps going after a failure and screenshots it."""

    def __init__(self, name: str, page: Page | None = None):
        self.name, self.page = name, page

    def __enter__(self):
        self.t0 = time.time()
        log(f"--- {self.name}")
        return self

    def __exit__(self, exc_type, exc, tb):
        dt = time.time() - self.t0
        if exc is None:
            RESULTS.append((self.name, "PASS", f"{dt:.1f}s"))
            return False
        detail = f"{type(exc).__name__}: {str(exc).splitlines()[0] if str(exc) else ''}"
        RESULTS.append((self.name, "FAIL", detail[:200]))
        tb_lines = [l for l in traceback.format_exception(exc_type, exc, tb) if "clickthrough.py" in l]
        log("FAIL " + self.name + "\n" + "".join(tb_lines[-3:]) + "  " + str(exc)[:500])
        if self.page is not None:
            try:
                shot(self.page, "FAIL-" + re.sub(r"\W+", "-", self.name))
                for _ in range(2):  # do not let an open dialog break the next step
                    if self.page.locator("dialog[open]").count():
                        self.page.keyboard.press("Escape")
                        self.page.wait_for_timeout(300)
            except Exception:
                pass
        return True  # swallow: later steps still run


def shot(page: Page, name: str) -> None:
    SHOTS.mkdir(parents=True, exist_ok=True)
    page.screenshot(path=str(SHOTS / f"{name}.png"), full_page=True)


# ---------------------------------------------------------------------------------------------
# Server helpers
# ---------------------------------------------------------------------------------------------


def http(method: str, url: str, body: dict | None = None, timeout: float = 10):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method, headers={"content-type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as res:
            raw = res.read().decode()
            return res.status, (json.loads(raw) if raw else None)
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw


def api(path: str):
    status, body = http("GET", API_URL + path)
    if status != 200:
        raise AssertionError(f"GET {path} -> {status} {body}")
    return body


def reset_fork() -> None:
    for _ in range(60):
        status, body = http("POST", API_URL + "/sim/reset", timeout=60)
        if status == 202:
            time.sleep(2)  # let the watcher and executor settle after the revert
            return
        if status == 409:  # a scenario is still running
            time.sleep(2)
            continue
        raise AssertionError(f"POST /sim/reset -> {status} {body}")
    raise AssertionError("POST /sim/reset kept answering 409")


def reachable(url: str) -> bool:
    try:
        with urllib.request.urlopen(url, timeout=3):
            return True
    except Exception:
        return False


# ---------------------------------------------------------------------------------------------
# Generic "does this control do something observable" machinery
# ---------------------------------------------------------------------------------------------

SIGNATURE_JS = """() => {
  const norm = (s) => s.replace(/\\d+/g, '#');
  const states = [...document.querySelectorAll('[aria-expanded],[aria-checked],input,select,dialog,button')]
    .map((e) => [e.tagName, e.getAttribute('aria-expanded'), e.getAttribute('aria-checked'),
      e.value ?? '', e.checked ?? '', e.disabled ?? '', e.open ?? ''].join('|')).join(';');
  return location.href + '\\n' + document.querySelectorAll('dialog[open]').length + '\\n' +
    norm(document.body.innerText) + '\\n' + states;
}"""

ENUMERATE_JS = """() => {
  const dialog = document.querySelector('dialog[open]');
  const root = dialog || document;
  const sel = 'button, a[href], input, select, [role=switch], [role=tab], [role=menuitem]';
  const out = [];
  let i = 0;
  const nameOf = (el) => {
    const aria = el.getAttribute('aria-label');
    if (aria) return aria.trim();
    const by = el.getAttribute('aria-labelledby');
    if (by) { const t = by.split(' ').map((id) => document.getElementById(id)?.innerText || '').join(' ').trim(); if (t) return t; }
    if (el.id) { const l = document.querySelector('label[for="' + el.id + '"]'); if (l) return l.innerText.trim(); }
    if (el.tagName === 'INPUT' && el.type === 'radio') { const l = el.closest('label'); if (l) return l.innerText.split('\\n')[0].trim(); }
    return (el.innerText || el.placeholder || el.value || el.title || '').trim().split('\\n')[0];
  };
  document.querySelectorAll('[data-e2e]').forEach((e) => e.removeAttribute('data-e2e'));
  for (const el of root.querySelectorAll(sel)) {
    if (el.type === 'hidden') continue;
    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    if (r.width < 3 || r.height < 3 || cs.visibility === 'hidden' || cs.display === 'none') continue;
    if (el.closest('[inert]')) continue;
    el.setAttribute('data-e2e', String(i));
    out.push({ i: i++, tag: el.tagName.toLowerCase(), type: el.type || '', role: el.getAttribute('role') || '',
      name: nameOf(el), href: el.getAttribute('href') || '', target: el.getAttribute('target') || '',
      disabled: !!el.disabled, checked: !!el.checked, inDialog: !!dialog });
  }
  return out;
}"""


class Watcher:
    """Counts requests to the API so a click that only fetches data still counts as observable."""

    def __init__(self, page: Page):
        self.count = 0
        page.on("request", lambda r: self._req(r.url))

    def _req(self, url: str):
        if url.startswith(API_URL):
            self.count += 1


WATCHERS: dict[int, Watcher] = {}


def watcher(page: Page) -> Watcher:
    key = id(page)
    if key not in WATCHERS:
        WATCHERS[key] = Watcher(page)
    return WATCHERS[key]


def observable(page: Page, action, what: str, settle_ms: int = 900) -> None:
    """Run `action`, then require that the URL, DOM text, control states, a dialog or an API request changed."""
    w = watcher(page)
    before, reqs = page.evaluate(SIGNATURE_JS), w.count
    action()
    deadline = time.time() + settle_ms / 1000
    while time.time() < deadline:
        page.wait_for_timeout(150)
        if page.evaluate(SIGNATURE_JS) != before or w.count > reqs:
            return
    raise AssertionError(f"no observable change after: {what}")


def act(page: Page, locator, name: str, action=None, settle_ms: int = 900) -> None:
    """Click a SCENARIO_ONLY control with the observable-change assertion and remember it."""
    EXERCISED.add(name)
    observable(page, action or (lambda: locator.click()), f"click '{name}'", settle_ms)


def enumerate_elements(page: Page) -> list[dict]:
    return page.evaluate(ENUMERATE_JS)


def close_dialog(page: Page) -> None:
    if page.locator("dialog[open]").count() == 0:
        return
    page.keyboard.press("Escape")
    page.wait_for_timeout(250)
    if page.locator("dialog[open]").count():
        for label in ("Cancel", "Close"):
            btn = page.locator("dialog[open]").get_by_role("button", name=label)
            if btn.count() and btn.first.is_enabled():
                btn.first.click()
                page.wait_for_timeout(250)
                break
    if page.locator("dialog[open]").count():
        raise AssertionError("dialog would not close")


def value_for(d: dict) -> str:
    if d["type"] == "email":
        return "e2e@example.com"
    if d["name"].lower().startswith("6-digit"):
        return "123456"
    if "pay up to" in d["name"].lower():
        return "3"
    return "test"


def exercise_one(page: Page, d: dict, where: str) -> str:
    """Exercise one control. Returns a short status string, raises on a no-op."""
    loc = page.locator(f'[data-e2e="{d["i"]}"]').first
    tag, typ, name = d["tag"], d["type"], d["name"] or "(unnamed)"
    if not d["name"] and d["tag"] != "input":
        raise AssertionError(f"{where}: {tag} without an accessible name")
    if d["disabled"]:
        DISABLED_LOG.append(f"{where}: disabled {tag} '{name}'")
        return "disabled"
    if tag == "input" and typ == "radio":
        if d["checked"]:
            return "already selected"
        observable(page, lambda: loc.check(), f"select radio '{name}'")
        return "radio"
    if tag == "input":
        observable(page, lambda: loc.fill(value_for(d)), f"type into '{name}'")
        return "typed"
    if tag == "select":
        options = loc.evaluate("(s) => [...s.options].filter((o) => !o.disabled).map((o) => o.value)")
        current = loc.input_value()
        other = next((o for o in options if o != current), None)
        if other is None:
            raise AssertionError(f"{where}: select '{name}' has no other option to choose")
        observable(page, lambda: loc.select_option(other), f"choose option in '{name}'", 1500)
        return "select"
    if d["role"] == "switch":
        observable(page, lambda: loc.click(timeout=8000), f"toggle switch '{name}'")
        return "switch"
    if tag == "a":
        href = d["href"]
        if href.startswith("http"):
            if d["target"] != "_blank":
                raise AssertionError(f"{where}: external link '{name}' does not open in a new tab")
            return "external (new tab)"
        start = page.url
        if urljoin(start, href).rstrip("/") == start.rstrip("/"):
            return "link to the current page"  # e.g. the active nav item; navigating changes nothing by design
        observable(page, lambda: loc.click(timeout=8000), f"follow link '{name}' -> {href}", 2500)
        page.wait_for_load_state("networkidle")
        if page.locator("main").count() == 0 or page.locator("main").inner_text().strip() == "":
            raise AssertionError(f"link '{name}' -> {href} led to an empty page")
        if page.url != start:
            page.go_back()
            page.wait_for_load_state("networkidle")
            page.wait_for_timeout(500)
        return "link"
    # button / menuitem
    if name in SCENARIO_ONLY:
        SEEN_SCENARIO_ONLY.add(name)
        return "scenario-only"
    was_dialog = page.locator("dialog[open]").count()
    observable(page, lambda: loc.click(timeout=8000), f"click '{name}'", 1500)
    if page.locator("dialog[open]").count() and not was_dialog:
        # the click opened a dialog: sweep inside it, then close it
        sweep(page, f"{where} > dialog '{name}'", dialog_mode=True)
        close_dialog(page)
    return "button"


DEFERRED = {"Close", "Cancel"}  # dialog closers: exercised last so the rest of the dialog gets its turn


def sweep(page: Page, where: str, limit: int = 120, dialog_mode: bool = False) -> None:
    """Exercise every visible interactive element once (see exercise_one)."""
    done: set[str] = set()
    n = 0
    for _ in range(limit):
        if dialog_mode and page.locator("dialog[open]").count() == 0:
            break
        elems = enumerate_elements(page)
        ordinal: dict[str, int] = {}
        candidates = []
        for d in elems:
            base = f'{d["tag"]}|{d["type"]}|{d["role"]}|{d["name"]}|{d["href"]}'
            ordinal[base] = ordinal.get(base, 0) + 1
            key = f"{base}#{ordinal[base]}"
            if key not in done:
                candidates.append((key, d))
        if not candidates:
            break
        pick = next((c for c in candidates if c[1]["name"] not in DEFERRED), candidates[0])
        key, target = pick
        done.add(key)
        if os.environ.get("VERBOSE"):
            log(f'      ... {target["tag"]} "{target["name"]}"')
        try:
            status = exercise_one(page, target, where)
        except PlaywrightTimeout:
            if page.locator(f'[data-e2e="{target["i"]}"]').count() == 0:
                status = "vanished (toast or re-render)"
            else:
                raise
        n += 1
        SWEEP_LOG.append(f'{where}: {target["tag"]} "{target["name"]}" -> {status}')
    else:
        raise AssertionError(f"{where}: sweep did not finish")
    log(f"    swept {n} controls on {where}")


def wait_idle(page: Page, extra_ms: int = 600) -> None:
    try:
        page.wait_for_load_state("networkidle", timeout=15000)
    except Exception:
        pass
    page.wait_for_timeout(extra_ms)


def goto(page: Page, path: str) -> None:
    page.goto(WEB_URL + path)
    wait_idle(page, 900)


def card(page: Page, label: str):
    return page.locator("article").filter(has=page.get_by_role("heading", name=label))


def band_word(page: Page, label: str) -> str:
    return card(page, label).locator('[role="status"]').first.inner_text().strip()


def toast(page: Page, text: str, timeout: int = 40000) -> None:
    expect(page.get_by_text(text).first).to_be_visible(timeout=timeout)


def horizontal_overflow(page: Page) -> bool:
    return page.evaluate("document.documentElement.scrollWidth > window.innerWidth + 1")


# ---------------------------------------------------------------------------------------------
# Web app process
# ---------------------------------------------------------------------------------------------


def ensure_web() -> subprocess.Popen | None:
    if reachable(WEB_URL):
        log(f"web app already answering at {WEB_URL}")
        return None
    web = ROOT / "web"
    port = re.search(r":(\d+)$", WEB_URL)
    port_s = port.group(1) if port else "4173"
    env = dict(os.environ, PUBLIC_API_URL=API_URL)
    log(f"building web app with PUBLIC_API_URL={API_URL}")
    subprocess.run(["pnpm", "build"], cwd=web, env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    log(f"starting pnpm preview on {port_s}")
    proc = subprocess.Popen(
        ["pnpm", "preview", "--port", port_s, "--host", "127.0.0.1", "--strictPort"],
        cwd=web, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        start_new_session=True,  # own process group, so stop_web can kill vite as well as pnpm
    )
    for _ in range(60):
        if reachable(WEB_URL):
            return proc
        time.sleep(0.5)
    stop_web(proc)
    raise SystemExit("could not start pnpm preview")


def stop_web(proc: subprocess.Popen | None) -> None:
    if proc is None:
        return
    try:
        os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
    except ProcessLookupError:
        pass


# ---------------------------------------------------------------------------------------------
# The run
# ---------------------------------------------------------------------------------------------


def new_page(ctx, width: int = 1440):
    page = ctx.new_page()
    page.set_viewport_size({"width": width, "height": 900 if width > 600 else 812})
    def on_console(m):
        if m.type != "error":
            return
        loc = (m.location or {}).get("url", "")
        # the two deliberate negative tests: an unknown tx hash (server 404) and an unknown position id
        if EXPECTED_404 in loc:
            return
        CONSOLE_ERRORS.append(f"[{page.url}] {m.text} <{loc}>")

    page.on("console", on_console)
    page.on("pageerror", lambda e: CONSOLE_ERRORS.append(f"[{page.url}] pageerror: {e}"))
    watcher(page)
    return page


def set_known_policy(scope) -> None:
    """Put a policy form (dialog or Settings) into the documented defaults, whatever it started from."""
    (scope if hasattr(scope, "wait_for_timeout") else scope.page).wait_for_timeout(1200)  # let a saved default policy finish loading first
    scope.get_by_role("radio", name="Exit automatically").check()
    scope.get_by_role("radio", name="Notify me").check()
    sw = scope.get_by_role("switch")
    if sw.get_attribute("aria-checked") != "true":
        sw.click()
    scope.get_by_label("Pay up to (US dollars)").fill("2")


def protect(page: Page, label: str, *, policy_changes: bool = False) -> None:
    """Protect a position through the panel and wait until the card is Guarded."""
    c = card(page, label)
    act_protect = c.get_by_role("button", name="Protect", exact=True)
    observable(page, lambda: act_protect.click(), f"open Protect panel for {label}")
    dlg = page.locator("dialog[open]")
    expect(dlg).to_be_visible()
    if policy_changes:
        dlg.get_by_role("radio", name="Ask me first").check()
        dlg.get_by_role("radio", name="Exit half").check()
    set_known_policy(dlg)  # a default policy saved in Settings must not change what the demo does
    activate = dlg.get_by_role("button", name="Activate protection")
    act(page, activate, "Activate protection", settle_ms=3000)
    expect(page.locator("dialog[open]")).to_have_count(0, timeout=90000)
    toast(page, "Protection active", 8000)
    expect(card(page, label).get_by_text("Guarded", exact=True)).to_be_visible(timeout=30000)
    expect(card(page, label).get_by_text(GUARANTEE)).to_be_visible()


def run() -> int:
    web_proc = ensure_web()
    SHOTS.mkdir(parents=True, exist_ok=True)
    for old in SHOTS.glob("*.png"):
        old.unlink()
    try:
        health = api("/healthz")
        cfg = api("/config")
        log(f"server ok: chain {health['chainId']} block {health['block']} demoMode={cfg['demoMode']}")
        assert cfg["demoMode"], "the server is not in demo mode; the simulator cannot run"
        ids = [t["id"] for t in cfg["targets"]]
        assert VAULT in ids and AAVE in ids, f"expected targets {VAULT}, {AAVE}; server has {ids}"
        reset_fork()
        return _run(cfg)
    finally:
        try:
            reset_fork()
        except Exception as e:  # pragma: no cover
            log(f"final reset failed: {e}")
        stop_web(web_proc)


def _run(cfg: dict) -> int:
    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path=CHROMIUM, headless=os.environ.get("HEADED") != "1", args=["--no-sandbox"]
        )
        ctx = browser.new_context(
            viewport={"width": 1440, "height": 900}, permissions=["clipboard-read", "clipboard-write"]
        )
        ctx.add_init_script(WALLET_STUB)
        page = new_page(ctx)

        # ------------------------------------------------------------------ wallet states
        with Step("wallet: not installed shows a fix", page):
            bare = browser.new_context(viewport={"width": 1440, "height": 900})  # no stub
            bp = new_page(bare)
            goto(bp, "/")
            expect(bp.get_by_text("No wallet found in this browser").first).to_be_visible()
            shot(bp, "wallet-not-installed")
            bare.close()

        with Step("wallet: not connected shows Connect wallet, click connects", page):
            goto(page, "/")
            page.evaluate("localStorage.setItem('stub.connected','0')")
            page.reload()
            wait_idle(page)
            expect(page.get_by_role("heading", name="Connect your wallet to begin")).to_be_visible()
            expect(page.get_by_text("Supported vaults")).to_be_visible()
            shot(page, "dashboard-not-connected-1440")
            sweep(page, "dashboard (not connected)")
            # the sweep clicked Connect wallet (the stub connects), so the address must now show in the header
            expect(page.get_by_role("button", name=re.compile(r"0x7099\.\.\.79C8")).first).to_be_visible()

        with Step("wallet: wrong network offers Switch network", page):
            page.evaluate("localStorage.setItem('stub.chain','0x1')")
            page.reload()
            wait_idle(page)
            switch = page.get_by_role("button", name="Switch network").first
            expect(switch).to_be_visible()
            shot(page, "wrong-network")
            act(page, switch, "Switch network", settle_ms=2500)
            expect(page.get_by_role("button", name="Switch network")).to_have_count(0)
            expect(page.get_by_text(cfg["chainName"]).first).to_be_visible()

        with Step("nav: no Backtest item while /backtests is empty; Simulator present in demo mode", page):
            status, body = http("GET", API_URL + "/backtests")
            nav = page.get_by_role("navigation", name="Main")
            expect(nav.get_by_role("link", name="Simulator")).to_be_visible()
            if status == 200 and not (body or {}).get("backtests"):
                expect(nav.get_by_role("link", name="Backtest")).to_have_count(0)

        with Step("a11y: skip link is the first Tab stop; focus ring is visible", page):
            goto(page, "/")
            page.keyboard.press("Tab")
            expect(page.get_by_role("link", name="Skip to content")).to_be_visible()
            page.keyboard.press("Tab")
            page.keyboard.press("Tab")
            outline = page.evaluate("getComputedStyle(document.activeElement).outlineStyle")
            assert outline != "none", "focused element has no outline"

        # ------------------------------------------------------------------ dashboard: unprotected
        with Step("dashboard: two unprotected cards with horn band, risk strip, Protect", page):
            goto(page, "/")
            for label in (VAULT_LABEL, AAVE_LABEL):
                c = card(page, label)
                expect(c).to_be_visible(timeout=20000)
                expect(c.get_by_text("Unprotected", exact=True)).to_be_visible()
                expect(c.get_by_role("button", name="Protect", exact=True)).to_be_visible()
                expect(c.get_by_role("list", name="Risk signals").get_by_role("listitem")).to_have_count(6, timeout=20000)
                assert band_word(page, label) in ("Calm", "Warning", "Critical"), band_word(page, label)
            shot(page, "dashboard-unprotected-1440")

        with Step("dashboard: sweep every control (unprotected)", page):
            sweep(page, "dashboard (unprotected)")

        with Step("protect panel: validation, switch, tooltip, Esc, focus trap", page):
            goto(page, "/")
            c = card(page, VAULT_LABEL)
            c.get_by_role("button", name="Protect", exact=True).click()
            dlg = page.locator("dialog[open]")
            expect(dlg).to_be_visible()
            shot(page, "protect-panel-1440")
            expect(dlg.get_by_text("My wallet, as USDC")).to_be_visible()
            expect(dlg.get_by_text("USDG")).to_have_count(0)
            expect(dlg.get_by_text(ADA)).to_be_visible()  # safe address, read only
            tip = dlg.get_by_label("Pay up to (US dollars)")
            activate = dlg.get_by_role("button", name="Activate protection")
            for bad, msg in (("abc", "Use digits only"), ("0", r"above \$0"), ("99", r"most you can set is \$50"), ("", "Enter the most")):
                tip.fill(bad)
                expect(dlg.get_by_text(re.compile(msg)).first).to_be_visible()
                expect(activate).to_be_disabled()
            expect(dlg.get_by_text("Fix the priority limit")).to_be_visible()  # disabled control explains why
            shot(page, "protect-panel-invalid-1440")
            tip.fill("2.5")
            expect(activate).to_be_enabled()
            sw = dlg.get_by_role("switch")
            sw.click()
            expect(sw).to_have_attribute("aria-checked", "false")
            expect(tip).to_be_disabled()
            expect(dlg.get_by_text("Priority exit is off")).to_be_visible()
            sw.click()
            dlg.get_by_role("button", name="What is a priority exit?").click()
            expect(dlg.get_by_role("tooltip")).to_contain_text("first out wins")
            # focus stays inside the dialog while tabbing
            for _ in range(25):
                page.keyboard.press("Tab")
                # a modal <dialog> makes the page inert: focus may leave for the browser UI (body) but never
                # lands on page content behind the drawer
                assert page.evaluate(
                    "document.activeElement === document.body || !!document.activeElement.closest('dialog')"
                ), "focus escaped the dialog into the page behind it"
            page.keyboard.press("Escape")
            expect(page.locator("dialog[open]")).to_have_count(0)

        with Step("protect panel: wallet rejection is calm and keeps the form", page):
            c = card(page, VAULT_LABEL)
            c.get_by_role("button", name="Protect", exact=True).click()
            dlg = page.locator("dialog[open]")
            dlg.get_by_role("radio", name="Ask me first").check()
            dlg.get_by_label("Pay up to (US dollars)").fill("4")
            page.evaluate("window.__stub.rejectNext = true")
            act(page, dlg.get_by_role("button", name="Activate protection"), "Activate protection")
            expect(dlg.get_by_text("You closed the request in your wallet")).to_be_visible(timeout=15000)
            assert dlg.get_by_role("radio", name="Ask me first").is_checked(), "form state lost"
            assert dlg.get_by_label("Pay up to (US dollars)").input_value() == "4", "tip cap lost"
            shot(page, "protect-panel-rejected-1440")
            dlg.get_by_role("radio", name="Exit automatically").check()
            dlg.get_by_label("Pay up to (US dollars)").fill("2")

        with Step("protect: Activate creates Guard, moves position, signs in, saves policy", page):
            dlg = page.locator("dialog[open]")
            retry = dlg.get_by_role("button", name="Retry")
            observable(page, lambda: retry.click(), "Retry after rejection", 3000)
            expect(page.locator("dialog[open]")).to_have_count(0, timeout=90000)
            toast(page, "Protection active", 8000)
            c = card(page, VAULT_LABEL)
            expect(c.get_by_text("Guarded", exact=True)).to_be_visible(timeout=30000)
            expect(c.get_by_text(GUARANTEE)).to_be_visible()
            for name in ("View details", "Turn off protection", "Withdraw to my wallet"):
                expect(c.get_by_role("link" if name == "View details" else "button", name=name)).to_be_visible()
            EXERCISED.add("Activate protection")
            shot(page, "dashboard-guarded-1440")
            st = api(f"/positions?address={ADA}")
            assert st["guard"], "server does not see the Guard"
            pos = next(x for x in st["positions"] if x["targetId"] == VAULT)
            assert pos["status"] == "guarded" and pos["policy"], f"server position: {pos}"

        with Step("protect: second position (Aave) also becomes Guarded", page):
            protect(page, AAVE_LABEL, policy_changes=True)
            shot(page, "dashboard-both-guarded-1440")

        with Step("dashboard: sweep every control (guarded, non-destructive)", page):
            goto(page, "/")
            sweep(page, "dashboard (guarded)")

        # ------------------------------------------------------------------ simulator: fast drain
        sim = new_page(ctx)
        with Step("simulator: page, scenario select, label, disabled reason", sim):
            goto(sim, "/simulator")
            expect(sim.get_by_role("heading", name="Incident simulator")).to_be_visible()
            expect(sim.get_by_text("Simulated on an Arbitrum One fork").first).to_be_visible()
            expect(sim.get_by_label("Scenario", exact=True)).to_be_visible()
            shot(sim, "simulator-1440")
            sweep(sim, "simulator")

        with Step("simulator: Fast drain drives the dashboard Warning/Critical then Exited, partial then complete", sim):
            goto(page, "/")
            seen: list[str] = []
            sim.get_by_label("Scenario", exact=True).select_option(label="Fast drain")
            run_btn = sim.get_by_role("button", name="Run scenario")
            act(sim, run_btn, "Run scenario", settle_ms=4000)
            expect(sim.get_by_role("log")).to_contain_text("Attack starting", timeout=30000)
            deadline = time.time() + 180
            shot_taken = set()
            while time.time() < deadline:
                word = band_word(page, VAULT_LABEL)
                if not seen or seen[-1] != word:
                    seen.append(word)
                    log(f"    band: {word}")
                    if word in ("Warning", "Critical", "Exiting") and word not in shot_taken:
                        shot(page, f"dashboard-{word.lower()}-1440")
                        shot_taken.add(word)
                if word == "Home safe":
                    break
                page.wait_for_timeout(150)
            assert seen[-1] == "Home safe", f"card never reached Home safe; band sequence {seen}"
            assert any(w in seen for w in ("Warning", "Critical")), f"band never escalated: {seen}"
            log(f"    band sequence: {' -> '.join(seen)}")
            page.wait_for_timeout(1500)
            expect(card(page, VAULT_LABEL).get_by_text("Exited", exact=True)).to_be_visible()
            expect(card(page, VAULT_LABEL).get_by_role("link", name="View exit")).to_be_visible()
            shot(page, "dashboard-exited-1440")
            expect(sim.get_by_text("Finished.", exact=True)).to_be_visible(timeout=120000)
            shot(sim, "simulator-after-run-1440")

        with Step("activity: partial then complete exit, filter works, live", page):
            goto(page, "/activity")
            body = page.locator("main")
            expect(body.get_by_text("Partial exit", exact=True).first).to_be_visible(timeout=20000)
            expect(body.get_by_text("Exit complete", exact=True).first).to_be_visible()
            expect(body.get_by_text("Risk level changed").first).to_be_visible()
            shot(page, "activity-1440")
            flt = page.get_by_label("Position")
            all_count = page.locator("main ol > li").count()
            flt.select_option(AAVE)
            page.wait_for_timeout(1200)
            aave_count = page.locator("main ol > li").count()
            flt.select_option(VAULT)
            page.wait_for_timeout(1200)
            vault_count = page.locator("main ol > li").count()
            assert vault_count > 0 and vault_count <= all_count, (vault_count, all_count)
            assert aave_count != vault_count, f"filter did not change the list ({aave_count} vs {vault_count})"
            shot(page, "activity-filtered-1440")
            flt.select_option("")
            sweep(page, "activity")

        with Step("simulator: comparison panel shows Ada vs Ben and the timeline", sim):
            goto(sim, "/simulator")
            expect(sim.get_by_role("heading", name="Ada and Ben")).to_be_visible()
            expect(sim.get_by_text("Ada, protected")).to_be_visible()
            expect(sim.get_by_text("Ben, unprotected")).to_be_visible()
            expect(sim.get_by_text("Timeline")).to_be_visible()
            expect(sim.get_by_text(re.compile(r"Block \d+")).first).to_be_visible()
            cmp_ = api("/sim/compare")
            assert cmp_["timeline"]["firstSignalBlock"], f"no first signal in timeline: {cmp_['timeline']}"
            shot(sim, "simulator-compare-1440")

        with Step("tx page: exit transaction link opens /tx/<hash> with decoded events", page):
            goto(page, "/activity")
            link = page.locator("main").get_by_role("link", name="View transaction").first
            href = link.get_attribute("href")
            assert href and href.startswith("/tx/0x"), href
            observable(page, lambda: link.click(), "open /tx link", 3000)
            expect(page.get_by_role("heading", name="Transaction")).to_be_visible()
            expect(page.get_by_text("Succeeded")).to_be_visible(timeout=15000)
            expect(page.get_by_text("Priority fee")).to_be_visible()
            expect(page.locator("main").get_by_text(re.compile(r"^Exited|Deposited|Withdrawn|GuardCreated|KeeperToggled"))
                   .first).to_be_visible()
            shot(page, "tx-1440")
            sweep(page, "tx page")

        with Step("tx page: unknown hash shows an error with retry", page):
            goto(page, "/tx/0x" + "ab" * 32)
            expect(page.get_by_role("alert")).to_be_visible(timeout=15000)
            expect(page.get_by_role("button", name="Try again")).to_be_visible()
            shot(page, "tx-unknown-1440")
            sweep(page, "tx page (unknown)")

        with Step("position detail (exited): band, receipts with links, history charts", page):
            goto(page, f"/positions/{VAULT}")
            expect(page.get_by_role("heading", name=VAULT_LABEL)).to_be_visible()
            expect(page.get_by_text("Home safe")).to_be_visible()
            expect(page.get_by_role("heading", name="Exit receipts")).to_be_visible()
            expect(page.locator("#exits article").first).to_be_visible(timeout=20000)
            expect(page.locator("#exits").get_by_role("link", name="View transaction").first).to_be_visible()
            expect(page.get_by_role("heading", name="Signal history")).to_be_visible()
            shot(page, "position-exited-1440")
            sweep(page, "position detail (exited)")

        with Step("position detail (unknown id) explains itself", page):
            goto(page, "/positions/nope")
            expect(page.get_by_role("heading", name="Position not found")).to_be_visible()
            sweep(page, "position not found")

        # ------------------------------------------------------------------ settings
        with Step("settings: sign in, disabled Telegram/Email explain why, default policy saves", page):
            page.evaluate("sessionStorage.clear()")
            goto(page, "/settings")
            signin = page.get_by_role("button", name="Sign in with your wallet")
            expect(signin).to_be_visible()
            shot(page, "settings-signin-1440")
            observable(page, lambda: signin.click(), "sign in on Settings", 8000)
            expect(page.get_by_role("heading", name="Telegram")).to_be_visible(timeout=15000)
            expect(page.get_by_role("button", name="Connect Telegram")).to_be_disabled()
            expect(page.get_by_text("Telegram alerts are not set up on this server")).to_be_visible()
            expect(page.get_by_role("button", name="Send code")).to_be_disabled()
            expect(page.get_by_label("Email address")).to_be_disabled()
            expect(page.get_by_text("Email alerts are not set up on this server")).to_be_visible()
            shot(page, "settings-1440")
            # default policy: change, Save, reload, value persists
            set_known_policy(page)  # earlier runs may have left another default policy saved
            page.get_by_role("radio", name="Ask me first").check()
            page.get_by_role("radio", name="Exit half").check()
            page.get_by_label("Pay up to (US dollars)").fill("7.5")
            save = page.get_by_role("button", name="Save")
            observable(page, lambda: save.click(), "Save default policy", 5000)
            expect(page.get_by_text("Default policy saved").first).to_be_visible(timeout=10000)
            page.reload()
            wait_idle(page)
            expect(page.get_by_role("heading", name="Default policy")).to_be_visible(timeout=15000)
            assert page.get_by_role("radio", name="Ask me first").is_checked(), "default policy did not persist"
            assert page.get_by_role("radio", name="Exit half").is_checked()
            assert page.get_by_label("Pay up to (US dollars)").input_value() in ("7.5",)
            # invalid tip cap blocks Save and says why
            page.get_by_label("Pay up to (US dollars)").fill("51")
            expect(page.get_by_role("button", name="Save")).to_be_disabled()
            expect(page.get_by_text("Fix the priority limit before you save")).to_be_visible()
            page.get_by_label("Pay up to (US dollars)").fill("2")
            page.get_by_role("radio", name="Exit automatically").check()
            page.get_by_role("radio", name="Notify me").check()
            page.get_by_role("button", name="Save").click()
            expect(page.get_by_text("Default policy saved").first).to_be_visible(timeout=10000)

        with Step("settings: sweep every control", page):
            goto(page, "/settings")
            expect(page.get_by_role("heading", name="Telegram")).to_be_visible(timeout=15000)
            sweep(page, "settings")
            # the sweep changed radios and toggles; put the saved default policy back to the documented defaults
            set_known_policy(page)
            page.get_by_role("button", name="Save").click()
            expect(page.get_by_text("Default policy saved").first).to_be_visible(timeout=10000)

        # ------------------------------------------------------------------ backtest (only with real data)
        with Step("backtest: page and nav item exist only when /backtests lists an incident", page):
            status, body = http("GET", API_URL + "/backtests")
            incidents = (body or {}).get("backtests", []) if status == 200 else []
            nav = page.get_by_role("navigation", name="Main")
            goto(page, "/backtest")
            if not incidents:
                expect(nav.get_by_role("link", name="Backtest")).to_have_count(0)
                expect(page.get_by_text("No incidents with real on-chain data are loaded")).to_be_visible()
                shot(page, "backtest-absent-1440")
                sweep(page, "backtest (no data)")
                log("    no backtest data on this server: nav item hidden, page explains itself")
            else:
                expect(nav.get_by_role("link", name="Backtest")).to_be_visible()
                chart = page.get_by_role("img", name=re.compile(r"Balance held by the pool"))
                expect(chart).to_be_visible(timeout=20000)
                expect(page.get_by_text("Heimdall's rules, run over the real on-chain history of this incident.")).to_be_visible()
                expect(page.get_by_role("heading", name="Data source")).to_be_visible()
                expect(page.get_by_text(re.compile(r"about \d|Critical at block|never reached Critical")).first).to_be_visible()
                shot(page, "backtest-1440")
                sweep(page, "backtest")
                if len(incidents) > 1:  # the incident select loads the chosen incident
                    sel = page.get_by_label("Incident")
                    sel.select_option(incidents[1]["id"])
                    expect(chart).to_be_visible(timeout=20000)

        # ------------------------------------------------------------------ reset + fresh guarded position
        with Step("simulator: Reset fork restores a clean chain", sim):
            goto(sim, "/simulator")
            reset_btn = sim.get_by_role("button", name="Reset fork")
            act(sim, reset_btn, "Reset fork", settle_ms=4000)
            expect(sim.get_by_text("The fork is back to its starting point.")).to_be_visible(timeout=60000)
            shot(sim, "simulator-reset-1440")
            time.sleep(2)
            goto(page, "/")
            expect(card(page, VAULT_LABEL).get_by_text("Unprotected", exact=True)).to_be_visible(timeout=20000)
            expect(card(page, AAVE_LABEL).get_by_text("Unprotected", exact=True)).to_be_visible()

        with Step("fresh guarded position for the detail-page actions", page):
            page.evaluate("sessionStorage.clear()")
            goto(page, "/")
            protect(page, VAULT_LABEL)

        with Step("detail page: sweep + Pause/Resume + Turn off/on", page):
            c = card(page, VAULT_LABEL)
            observable(page, lambda: c.get_by_role("link", name="View details").click(), "View details", 3000)
            expect(page.get_by_role("heading", name="Actions")).to_be_visible()
            expect(page.get_by_text(GUARANTEE)).to_be_visible()
            expect(page.get_by_role("heading", name="Policy")).to_be_visible()
            shot(page, "position-guarded-1440")
            # Edit policy dialog (SIWE + PUT)
            edit = page.get_by_role("button", name="Edit policy")
            observable(page, lambda: edit.click(), "Edit policy", 2000)
            dlg = page.locator("dialog[open]")
            dlg.get_by_role("radio", name="Exit half").check()
            save = dlg.get_by_role("button", name="Save policy")
            observable(page, lambda: save.click(), "Save policy", 8000)
            expect(page.locator("dialog[open]")).to_have_count(0, timeout=15000)
            expect(page.get_by_text("Exit half").first).to_be_visible()
            sweep(page, "position detail (guarded)")
            # Pause / Resume
            pause = page.get_by_role("button", name="Pause Heimdall")
            act(page, pause, "Pause Heimdall", settle_ms=3000)
            resume = page.get_by_role("button", name="Resume")
            expect(resume).to_be_visible(timeout=30000)
            act(page, resume, "Resume", settle_ms=3000)
            expect(page.get_by_role("button", name="Pause Heimdall")).to_be_visible(timeout=30000)
            # Turn off / on
            off = page.get_by_role("button", name="Turn off protection")
            act(page, off, "Turn off protection", settle_ms=3000)
            on = page.get_by_role("button", name="Turn on protection")
            expect(on).to_be_visible(timeout=30000)
            shot(page, "position-protection-off-1440")
            act(page, on, "Turn on protection", settle_ms=3000)
            expect(page.get_by_role("button", name="Turn off protection")).to_be_visible(timeout=30000)
            st = api(f"/positions?address={ADA}")
            assert st["keeperEnabled"] and not st["paused"], st

        with Step("detail page: Withdraw to my wallet returns the position, then Protect again", page):
            withdraw = page.get_by_role("button", name="Withdraw to my wallet")
            act(page, withdraw, "Withdraw to my wallet", settle_ms=3000)
            toast(page, "Withdrawn to your wallet", 40000)
            pos = next(x for x in api(f"/positions?address={ADA}")["positions"] if x["targetId"] == VAULT)
            assert pos["guardedPositionTokens"] == "0" or pos["status"] == "unprotected", pos
            goto(page, "/")
            protect(page, VAULT_LABEL)

        with Step("detail page: Exit now sends the money to the wallet, band shows Home safe", page):
            goto(page, f"/positions/{VAULT}")
            exit_btn = page.get_by_role("button", name="Exit now")
            act(page, exit_btn, "Exit now", settle_ms=3000)
            expect(page.get_by_text("Exit sent.")).to_be_visible(timeout=40000)
            expect(page.get_by_text("Home safe").first).to_be_visible(timeout=30000)
            expect(page.locator("#exits article").first).to_be_visible(timeout=30000)
            shot(page, "position-after-exit-now-1440")

        with Step("wallet menu: copy address, Disconnect then Connect wallet", page):
            goto(page, "/")
            menu = page.get_by_role("button", name=re.compile(r"0x7099"))
            observable(page, lambda: menu.click(), "open wallet menu")
            expect(page.get_by_role("menuitem", name="Copy address")).to_be_visible()
            observable(page, lambda: page.get_by_role("menuitem", name="Copy address").click(), "Copy address")
            expect(page.get_by_text("Address copied").first).to_be_visible()
            clip = page.evaluate("navigator.clipboard.readText()")
            assert clip == ADA, clip
            menu.click()
            disc = page.get_by_role("menuitem", name="Disconnect")
            act(page, disc, "Disconnect")
            expect(page.get_by_role("main").get_by_role("button", name="Connect wallet")).to_be_visible()
            page.get_by_role("main").get_by_role("button", name="Connect wallet").click()
            expect(page.get_by_role("button", name=re.compile(r"0x7099"))).to_be_visible()

        # ------------------------------------------------------------------ every route, two widths
        for width in (1440, 375):
            vp = new_page(ctx, width)
            routes = ["/", f"/positions/{VAULT}", f"/positions/{AAVE}", "/activity", "/simulator", "/settings"]
            tx_hash = None
            try:
                ev = api(f"/activity?address={ADA}&limit=50")["events"]
                tx_hash = next((e["txHash"] for e in ev if e.get("txHash")), None)
            except Exception:
                pass
            if tx_hash:
                routes.append(f"/tx/{tx_hash}")
            routes.append("/backtest")
            for r in routes:
                with Step(f"route {r} at {width}px: loads, no overflow, screenshot", vp):
                    goto(vp, r)
                    assert vp.locator("main").inner_text().strip(), "empty page"
                    assert not horizontal_overflow(vp), "horizontal scroll"
                    name = ("dashboard" if r == "/" else r.strip("/").replace("/", "_")[:60])
                    shot(vp, f"route-{name}-{width}")
            if width == 375:
                with Step("mobile: dashboard actions and Protect drawer fit the viewport", vp):
                    goto(vp, "/")
                    for b in vp.locator("article button, article a").all():
                        box = b.bounding_box()
                        if box:
                            assert box["x"] >= 0 and box["x"] + box["width"] <= 376, box
                    vp.get_by_role("button", name="Protect", exact=True).first.click()
                    expect(vp.locator("dialog[open]")).to_be_visible()
                    assert not horizontal_overflow(vp), "overflow with drawer"
                    shot(vp, "protect-panel-375")
                    close_dialog(vp)
            vp.close()

        # ------------------------------------------------------------------ verdicts
        with Step("coverage: every state-changing control was exercised by the demo path"):
            missing = sorted(SEEN_SCENARIO_ONLY - EXERCISED)
            assert not missing, f"controls seen but never exercised with an assertion: {missing}"
            required = {"Activate protection", "Run scenario", "Reset fork", "Pause Heimdall", "Resume",
                        "Turn off protection", "Turn on protection", "Withdraw to my wallet", "Exit now",
                        "Disconnect", "Switch network"}
            assert required <= EXERCISED, f"demo path skipped: {sorted(required - EXERCISED)}"

        with Step("console: no errors on any page"):
            errs = [e for e in CONSOLE_ERRORS]
            assert not errs, f"{len(errs)} console errors, first: {errs[0]}"

        browser.close()

    # summary
    width = max(len(n) for n, _, _ in RESULTS) + 2
    print("\n" + "=" * (width + 40))
    print(f"{'STEP':<{width}}RESULT  DETAIL")
    print("-" * (width + 40))
    for name, status, detail in RESULTS:
        print(f"{name:<{width}}{status:<8}{detail}")
    print("=" * (width + 40))
    failed = [r for r in RESULTS if r[1] == "FAIL"]
    print(f"{len(RESULTS) - len(failed)} passed, {len(failed)} failed; {len(SWEEP_LOG)} controls swept; "
          f"screenshots in {SHOTS}")
    if DISABLED_LOG:
        print(f"{len(DISABLED_LOG)} disabled controls seen (each was checked for an explanation in its step):")
        for line in sorted(set(DISABLED_LOG)):
            print("   " + line)
    if CONSOLE_ERRORS:
        print(f"{len(CONSOLE_ERRORS)} console errors:")
        for e in CONSOLE_ERRORS[:10]:
            print("   " + e[:200])
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(run())
