package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// User is a wallet that signed in or has a Guard.
type User struct {
	Address          string
	DefaultPolicy    json.RawMessage // nil when never set
	TelegramChatID   *int64
	TelegramUsername string
	Email            string
	EmailVerified    bool
}

// EnsureUser creates the user row if needed.
func (s *Store) EnsureUser(ctx context.Context, addr string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO users (address) VALUES ($1) ON CONFLICT DO NOTHING`, Lower(addr))
	return err
}

// GetUser returns the user (an empty User when unknown, never an error for "missing").
func (s *Store) GetUser(ctx context.Context, addr string) (*User, error) {
	u := &User{Address: Lower(addr)}
	err := s.Pool.QueryRow(ctx, `SELECT default_policy, telegram_chat_id, telegram_username, email, email_verified FROM users WHERE address=$1`, u.Address).
		Scan(&u.DefaultPolicy, &u.TelegramChatID, &u.TelegramUsername, &u.Email, &u.EmailVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, nil
	}
	return u, err
}

// SetDefaultPolicy stores the owner's default policy.
func (s *Store) SetDefaultPolicy(ctx context.Context, addr string, p json.RawMessage) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO users (address, default_policy) VALUES ($1,$2)
		ON CONFLICT (address) DO UPDATE SET default_policy=EXCLUDED.default_policy`, Lower(addr), []byte(p))
	return err
}

// SetTelegram links a Telegram chat to a wallet.
func (s *Store) SetTelegram(ctx context.Context, addr string, chatID int64, username string) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO users (address, telegram_chat_id, telegram_username) VALUES ($1,$2,$3)
		ON CONFLICT (address) DO UPDATE SET telegram_chat_id=EXCLUDED.telegram_chat_id, telegram_username=EXCLUDED.telegram_username`,
		Lower(addr), chatID, username)
	return err
}

// ClearTelegram unlinks Telegram.
func (s *Store) ClearTelegram(ctx context.Context, addr string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE users SET telegram_chat_id=NULL, telegram_username='' WHERE address=$1`, Lower(addr))
	return err
}

// CreateTelegramLink stores a one-time link code.
func (s *Store) CreateTelegramLink(ctx context.Context, addr, code string, exp time.Time) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM telegram_links WHERE address=$1 OR expires_at < now()`, Lower(addr))
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO telegram_links (code,address,expires_at) VALUES ($1,$2,$3)`, code, Lower(addr), exp)
	return err
}

// ConsumeTelegramLink returns the wallet for an unexpired code and deletes it.
func (s *Store) ConsumeTelegramLink(ctx context.Context, code string) (string, bool, error) {
	var addr string
	err := s.Pool.QueryRow(ctx, `DELETE FROM telegram_links WHERE code=$1 AND expires_at > now() RETURNING address`, code).Scan(&addr)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return addr, err == nil, err
}

// SetEmailPending stores an email awaiting verification.
func (s *Store) SetEmailPending(ctx context.Context, addr, email, codeHash string, exp time.Time) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO email_verifications (address,email,code_hash,expires_at,attempts) VALUES ($1,$2,$3,$4,0)
		ON CONFLICT (address) DO UPDATE SET email=EXCLUDED.email, code_hash=EXCLUDED.code_hash, expires_at=EXCLUDED.expires_at, attempts=0`,
		Lower(addr), email, codeHash, exp)
	return err
}

// VerifyEmail checks the code (max 5 attempts) and marks the email verified.
func (s *Store) VerifyEmail(ctx context.Context, addr, codeHash string) (bool, string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var email, hash string
	var exp time.Time
	var attempts int
	err = tx.QueryRow(ctx, `SELECT email, code_hash, expires_at, attempts FROM email_verifications WHERE address=$1 FOR UPDATE`, Lower(addr)).
		Scan(&email, &hash, &exp, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "no_pending", nil
	}
	if err != nil {
		return false, "", err
	}
	if time.Now().After(exp) {
		return false, "expired", nil
	}
	if attempts >= 5 {
		return false, "too_many", nil
	}
	if hash != codeHash {
		_, err = tx.Exec(ctx, `UPDATE email_verifications SET attempts=attempts+1 WHERE address=$1`, Lower(addr))
		if err != nil {
			return false, "", err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, "", err
		}
		return false, "wrong", nil
	}
	if _, err = tx.Exec(ctx, `INSERT INTO users (address,email,email_verified) VALUES ($1,$2,true)
		ON CONFLICT (address) DO UPDATE SET email=EXCLUDED.email, email_verified=true`, Lower(addr), email); err != nil {
		return false, "", err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM email_verifications WHERE address=$1`, Lower(addr)); err != nil {
		return false, "", err
	}
	return true, "", tx.Commit(ctx)
}

// PendingEmail returns an email that has been entered but not verified yet ("" when none).
func (s *Store) PendingEmail(ctx context.Context, addr string) (string, error) {
	var email string
	err := s.Pool.QueryRow(ctx, `SELECT email FROM email_verifications WHERE address=$1 AND expires_at > now()`, Lower(addr)).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return email, err
}

// CreateNonce stores a SIWE nonce with the exact message the wallet must sign.
func (s *Store) CreateNonce(ctx context.Context, nonce, addr, message string, exp time.Time) error {
	if _, err := s.Pool.Exec(ctx, `DELETE FROM auth_nonces WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err := s.Pool.Exec(ctx, `INSERT INTO auth_nonces (nonce,address,message,expires_at) VALUES ($1,$2,$3,$4)`, nonce, Lower(addr), message, exp)
	return err
}

// ConsumeNonce deletes the nonce (single use) and returns the stored address and message.
func (s *Store) ConsumeNonce(ctx context.Context, nonce string) (addr, message string, err error) {
	err = s.Pool.QueryRow(ctx, `DELETE FROM auth_nonces WHERE nonce=$1 AND expires_at > now() RETURNING address, message`, nonce).Scan(&addr, &message)
	return addr, message, notFound(err)
}
