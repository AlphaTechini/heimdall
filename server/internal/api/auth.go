package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/store"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	nonceTTL      = 10 * time.Minute
	tokenTTL      = 24 * time.Hour
	siweStatement = "Sign in to Heimdall to change your protection settings."
)

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("no randomness available: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// siweMessage builds the EIP-4361 message the wallet signs.
func siweMessage(domain, uri, address string, chainID uint64, nonce string, issued time.Time) string {
	return fmt.Sprintf("%s wants you to sign in with your Ethereum account:\n%s\n\n%s\n\nURI: %s\nVersion: 1\nChain ID: %d\nNonce: %s\nIssued At: %s",
		domain, address, siweStatement, uri, chainID, nonce, issued.UTC().Format("2006-01-02T15:04:05.000Z"))
}

func (a *API) authNonce(w http.ResponseWriter, r *http.Request) {
	addr, ok := parseAddr(r.URL.Query().Get("address"))
	if !ok {
		writeErr(w, 400, "Enter a valid wallet address (0x...).")
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	host, origin := originHost(r)
	nonce := randHex(16)
	msg := siweMessage(host, origin, addr.Hex(), a.Chain.ChainID.Uint64(), nonce, time.Now())
	if err := a.Store.CreateNonce(ctx, nonce, addr.Hex(), msg, time.Now().Add(nonceTTL)); err != nil {
		a.dbErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"nonce": nonce, "message": msg})
}

var nonceRe = regexp.MustCompile(`(?m)^Nonce: ([0-9a-f]{32})$`)

// recoverSigner recovers the address that signed msg with personal_sign. Wallets return v as
// 27/28 (or 0/1); go-ethereum's SigToPub wants 0/1.
func recoverSigner(msg, sigHex string) (common.Address, error) {
	sig, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil || len(sig) != 65 {
		return common.Address{}, errors.New("the signature is not 65 bytes of hex")
	}
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	if sig[64] > 1 {
		return common.Address{}, errors.New("the signature has an invalid recovery value")
	}
	pub, err := crypto.SigToPub(accounts.TextHash([]byte(msg)), sig)
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(*pub), nil
}

func (a *API) authVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Address   string `json:"address"`
		Message   string `json:"message"`
		Signature string `json:"signature"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	addr, ok := parseAddr(body.Address)
	if !ok {
		writeErr(w, 400, "Enter a valid wallet address (0x...).")
		return
	}
	m := nonceRe.FindStringSubmatch(body.Message)
	if m == nil {
		writeErr(w, 400, "The sign-in message is missing its nonce. Please start again.")
		return
	}
	ctx, cancel := a.ctx(r)
	defer cancel()
	nAddr, nMsg, err := a.Store.ConsumeNonce(ctx, m[1])
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 401, "This sign-in request expired or was already used. Please start again.")
		return
	}
	if err != nil {
		a.dbErr(w, err)
		return
	}
	if nMsg != body.Message || !strings.EqualFold(nAddr, addr.Hex()) {
		writeErr(w, 401, "The signed message does not match the one we asked you to sign.")
		return
	}
	signer, err := recoverSigner(body.Message, body.Signature)
	if err != nil || signer != addr {
		writeErr(w, 401, "The signature does not match this wallet address.")
		return
	}
	exp := time.Now().Add(tokenTTL).UTC()
	_ = a.Store.EnsureUser(ctx, addr.Hex())
	writeJSON(w, 200, map[string]any{"token": a.makeToken(addr, exp), "address": addr.Hex(), "expiresAt": exp.Format(time.RFC3339)})
}

type tokenClaims struct {
	Addr string `json:"a"`
	Exp  int64  `json:"e"`
}

func (a *API) sign(b []byte) []byte {
	h := hmac.New(sha256.New, a.authKey)
	h.Write(b)
	return h.Sum(nil)
}

func (a *API) makeToken(addr common.Address, exp time.Time) string {
	p, _ := json.Marshal(tokenClaims{Addr: strings.ToLower(addr.Hex()), Exp: exp.Unix()})
	return base64.RawURLEncoding.EncodeToString(p) + "." + base64.RawURLEncoding.EncodeToString(a.sign(p))
}

func (a *API) parseToken(tok string) (common.Address, bool) {
	ps, ss, ok := strings.Cut(tok, ".")
	if !ok {
		return common.Address{}, false
	}
	p, err1 := base64.RawURLEncoding.DecodeString(ps)
	s, err2 := base64.RawURLEncoding.DecodeString(ss)
	if err1 != nil || err2 != nil || !hmac.Equal(s, a.sign(p)) {
		return common.Address{}, false
	}
	var c tokenClaims
	if json.Unmarshal(p, &c) != nil || time.Now().Unix() > c.Exp || !common.IsHexAddress(c.Addr) {
		return common.Address{}, false
	}
	return common.HexToAddress(c.Addr), true
}

type authedHandler func(w http.ResponseWriter, r *http.Request, actor common.Address)

func (a *API) authed(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeErr(w, 401, "Please sign in with your wallet first.")
			return
		}
		actor, ok := a.parseToken(strings.TrimSpace(tok))
		if !ok {
			writeErr(w, 401, "Your sign-in expired. Please sign in with your wallet again.")
			return
		}
		h(w, r, actor)
	}
}
