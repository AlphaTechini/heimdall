package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Telegram talks to the plain HTTP Bot API (https://core.telegram.org/bots/api): sendMessage
// and getUpdates. The bot token is part of the URL, so errors are stripped of it before they
// are logged or returned (specs W9 spirit: no secrets in logs).
type Telegram struct {
	token   string
	Bot     string
	baseURL string
	hc      *http.Client
}

// NewTelegram creates the channel. An empty token disables it.
func NewTelegram(token, botUsername string) *Telegram {
	base := "https://api.telegram.org"
	if v := os.Getenv("TELEGRAM_API_BASE"); v != "" { // for tests against a local stub only
		base = strings.TrimRight(v, "/")
	}
	return &Telegram{token: token, Bot: botUsername, baseURL: base, hc: &http.Client{Timeout: 40 * time.Second}}
}

func (t *Telegram) Name() string  { return "telegram" }
func (t *Telegram) Enabled() bool { return t.token != "" }

func clean(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("telegram request failed: %w", ue.Err)
	}
	return err
}

type apiResp struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (t *Telegram) call(ctx context.Context, method string, body any) (json.RawMessage, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/bot"+t.token+"/"+method, bytes.NewReader(b))
	if err != nil {
		return nil, errors.New("telegram: bad request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.hc.Do(req)
	if err != nil {
		return nil, clean(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var ar apiResp
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("telegram answered with HTTP %d", resp.StatusCode)
	}
	if !ar.OK {
		return nil, fmt.Errorf("telegram refused the request: %s", ar.Description)
	}
	return ar.Result, nil
}

// Send delivers a message to the owner's linked chat.
func (t *Telegram) Send(ctx context.Context, r Recipient, m Message) (bool, error) {
	if r.TelegramChatID == nil {
		return false, nil
	}
	return true, t.SendText(ctx, *r.TelegramChatID, m.Subject+"\n\n"+m.Body)
}

// SendText sends plain text to a chat.
func (t *Telegram) SendText(ctx context.Context, chatID int64, text string) error {
	_, err := t.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text})
	return err
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		From struct {
			Username string `json:"username"`
		} `json:"from"`
	} `json:"message"`
}

// Poll long-polls getUpdates and calls onStart for every "/start <code>" message. The returned
// string is sent back to the chat as the reply. It returns when ctx ends.
func (t *Telegram) Poll(ctx context.Context, onStart func(ctx context.Context, code string, chatID int64, username string) string) {
	var offset int64
	for ctx.Err() == nil {
		res, err := t.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message"}})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("telegram polling failed; retrying", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		var ups []update
		if err := json.Unmarshal(res, &ups); err != nil {
			continue
		}
		for _, u := range ups {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message == nil {
				continue
			}
			text := strings.TrimSpace(u.Message.Text)
			code, ok := strings.CutPrefix(text, "/start")
			code = strings.TrimSpace(code)
			if !ok || code == "" {
				if ok {
					_ = t.SendText(ctx, u.Message.Chat.ID, "Open Heimdall > Settings > Link Telegram to get a link code.")
				}
				continue
			}
			reply := onStart(ctx, code, u.Message.Chat.ID, u.Message.From.Username)
			if err := t.SendText(ctx, u.Message.Chat.ID, reply); err != nil {
				slog.Warn("telegram reply failed", "err", err)
			}
		}
	}
}
