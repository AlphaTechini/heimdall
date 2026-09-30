package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/notify"
	"github.com/AlphaTechini/heimdall/server/internal/policy"
	"github.com/ethereum/go-ethereum/common"
)

// ownerOf returns the guard's on-chain owner after checking that the guard is a Heimdall guard.
func (a *API) requireOwner(w http.ResponseWriter, r *http.Request, guardStr string, actor common.Address) (common.Address, bool) {
	ga, ok := parseAddr(guardStr)
	if !ok {
		writeErr(w, 400, "That is not a valid Guard address.")
		return ga, false
	}
	owner, err := a.guardOwner(r, ga)
	if errors.Is(err, errNotFound) {
		writeErr(w, 404, "That address is not a Heimdall Guard.")
		return ga, false
	}
	if err != nil {
		a.rpcErr(w, err)
		return ga, false
	}
	if owner != actor {
		writeErr(w, 403, "Only the owner of this Guard can do that.")
		return ga, false
	}
	return ga, true
}

func (a *API) putPolicy(w http.ResponseWriter, r *http.Request, actor common.Address) {
	ga, ok := a.requireOwner(w, r, r.PathValue("guard"), actor)
	if !ok {
		return
	}
	t := a.target(w, r.PathValue("targetId"))
	if t == nil {
		return
	}
	var p policy.Policy
	if !decodeBody(w, r, &p) {
		return
	}
	if err := p.Validate(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	if err := a.Policy.Put(ctx, ga.Hex(), t.ID, p); err != nil {
		a.dbErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (a *API) meSettings(w http.ResponseWriter, r *http.Request, actor common.Address) {
	ctx, cancel := a.ctx(r)
	defer cancel()
	u, err := a.Store.GetUser(ctx, actor.Hex())
	if err != nil {
		a.dbErr(w, err)
		return
	}
	def, err := a.Policy.OwnerDefault(ctx, actor.Hex())
	if err != nil {
		a.dbErr(w, err)
		return
	}
	resp := map[string]any{"address": actor.Hex(), "telegram": nil, "email": nil, "defaultPolicy": def}
	if u.TelegramChatID != nil {
		resp["telegram"] = map[string]any{"linked": true, "username": u.TelegramUsername}
	}
	if u.Email != "" {
		resp["email"] = map[string]any{"address": u.Email, "verified": u.EmailVerified}
	} else if pend, err := a.Store.PendingEmail(ctx, actor.Hex()); err == nil && pend != "" {
		resp["email"] = map[string]any{"address": pend, "verified": false}
	}
	writeJSON(w, 200, resp)
}

func (a *API) meDefaultPolicy(w http.ResponseWriter, r *http.Request, actor common.Address) {
	var p policy.Policy
	if !decodeBody(w, r, &p) {
		return
	}
	if err := p.Validate(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	if err := a.Policy.SetOwnerDefault(ctx, actor.Hex(), p); err != nil {
		a.dbErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

// ---- Telegram ----

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func linkCode() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

func (a *API) telegramLink(w http.ResponseWriter, r *http.Request, actor common.Address) {
	if !a.telegramEnabled() {
		writeErr(w, 400, "Telegram alerts are not set up on this server (the bot token or bot name is missing).")
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	code, exp := linkCode(), time.Now().Add(10*time.Minute).UTC()
	if err := a.Store.CreateTelegramLink(ctx, actor.Hex(), code, exp); err != nil {
		a.dbErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"code": code, "deepLink": fmt.Sprintf("https://t.me/%s?start=%s", a.Telegram.Bot, code), "expiresAt": exp.Format(time.RFC3339)})
}

func (a *API) telegramUnlink(w http.ResponseWriter, r *http.Request, actor common.Address) {
	ctx, cancel := a.ctx(r)
	defer cancel()
	if err := a.Store.ClearTelegram(ctx, actor.Hex()); err != nil {
		a.dbErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) telegramTest(w http.ResponseWriter, r *http.Request, actor common.Address) {
	if !a.telegramEnabled() {
		writeErr(w, 400, "Telegram alerts are not set up on this server.")
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	u, err := a.Store.GetUser(ctx, actor.Hex())
	if err != nil {
		a.dbErr(w, err)
		return
	}
	if u.TelegramChatID == nil {
		writeErr(w, 400, "Link Telegram first, then send a test message.")
		return
	}
	err = a.Notifier.SendNow(ctx, "telegram", notify.Recipient{TelegramChatID: u.TelegramChatID}, notify.Message{Subject: "Heimdall test alert", Body: "If you can read this, Telegram alerts work."})
	if err != nil {
		slog.Warn("telegram test failed", "err", err)
		writeErr(w, 502, "Telegram did not accept the message: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"sent": true})
}

// ---- Email ----

func hashCode(addr, code string) string {
	h := sha256.Sum256([]byte(strings.ToLower(addr) + ":" + code))
	return hex.EncodeToString(h[:])
}

func (a *API) cooldown(actor common.Address, d time.Duration) bool {
	a.cooldownMu.Lock()
	defer a.cooldownMu.Unlock()
	k := strings.ToLower(actor.Hex())
	if t, ok := a.emailCooldown[k]; ok && time.Now().Before(t) {
		return false
	}
	a.emailCooldown[k] = time.Now().Add(d)
	return true
}

func (a *API) emailSet(w http.ResponseWriter, r *http.Request, actor common.Address) {
	if !a.emailEnabled() {
		writeErr(w, 400, "Email alerts are not set up on this server (the Resend key or sender address is missing).")
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	pa, err := mail.ParseAddress(strings.TrimSpace(body.Email))
	if err != nil || !strings.Contains(pa.Address, ".") {
		writeErr(w, 400, "That email address does not look right.")
		return
	}
	if !a.cooldown(actor, 20*time.Second) {
		writeErr(w, 429, "Please wait a few seconds before asking for another code.")
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	code := fmt.Sprintf("%06d", n.Int64())
	if err := a.Store.SetEmailPending(ctx, actor.Hex(), pa.Address, hashCode(actor.Hex(), code), time.Now().Add(10*time.Minute)); err != nil {
		a.dbErr(w, err)
		return
	}
	if err := a.Email.SendTo(ctx, pa.Address, "Your Heimdall verification code", fmt.Sprintf("Your Heimdall code is %s. It works for 10 minutes.", code)); err != nil {
		slog.Warn("verification email failed", "err", err)
		writeErr(w, 502, "We could not send the email: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"sent": true})
}

func (a *API) emailVerify(w http.ResponseWriter, r *http.Request, actor common.Address) {
	var body struct {
		Code string `json:"code"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	ok, why, err := a.Store.VerifyEmail(ctx, actor.Hex(), hashCode(actor.Hex(), strings.TrimSpace(body.Code)))
	if err != nil {
		a.dbErr(w, err)
		return
	}
	if !ok {
		msg := map[string]string{
			"no_pending": "There is no email waiting to be verified. Enter your email first.",
			"expired":    "That code expired. Ask for a new one.",
			"too_many":   "Too many wrong tries. Ask for a new code.",
			"wrong":      "That code is not right. Check the email and try again.",
		}[why]
		writeErr(w, 400, msg)
		return
	}
	writeJSON(w, 200, map[string]bool{"verified": true})
}

func (a *API) emailTest(w http.ResponseWriter, r *http.Request, actor common.Address) {
	if !a.emailEnabled() {
		writeErr(w, 400, "Email alerts are not set up on this server.")
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	u, err := a.Store.GetUser(ctx, actor.Hex())
	if err != nil {
		a.dbErr(w, err)
		return
	}
	if !u.EmailVerified || u.Email == "" {
		writeErr(w, 400, "Verify your email first, then send a test.")
		return
	}
	if !a.cooldown(actor, 10*time.Second) {
		writeErr(w, 429, "Please wait a few seconds before sending another test.")
		return
	}
	if err := a.Email.SendTo(ctx, u.Email, "Heimdall test alert", "If you can read this, email alerts work."); err != nil {
		writeErr(w, 502, "We could not send the email: "+err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"sent": true})
}

// ---- stop ----

func (a *API) stopExit(w http.ResponseWriter, r *http.Request, actor common.Address) {
	ga, ok := a.requireOwner(w, r, r.PathValue("guard"), actor)
	if !ok {
		return
	}
	t := a.target(w, r.PathValue("targetId"))
	if t == nil {
		return
	}
	if !a.Executor.Stop(ga, t.ID) {
		writeErr(w, 404, "There is no exit in progress for that position.")
		return
	}
	writeJSON(w, 200, map[string]bool{"stopped": true})
}
