// Package notify sends alerts to Guard owners over Telegram and email. Sending is
// asynchronous and never blocks an exit (specs U6).
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/AlphaTechini/heimdall/server/internal/store"
)

// Message is one alert for a Guard owner.
type Message struct {
	Owner   string
	Kind    string
	Subject string
	Body    string
}

// Recipient holds an owner's verified contact details.
type Recipient struct {
	TelegramChatID *int64
	Email          string // verified address only
}

// Notifier is one delivery channel.
type Notifier interface {
	Name() string
	Enabled() bool
	// Send delivers m; ok is false when the recipient has no contact on this channel.
	Send(ctx context.Context, r Recipient, m Message) (ok bool, err error)
}

// ErrNoContact means the owner has not set this channel up.
var ErrNoContact = errors.New("no contact set up for this channel")

// Dispatcher queues messages and delivers them with retries.
type Dispatcher struct {
	st        *store.Store
	notifiers []Notifier
	q         chan Message
}

// NewDispatcher starts the workers. Disabled channels are skipped (and logged once).
func NewDispatcher(ctx context.Context, st *store.Store, ns ...Notifier) *Dispatcher {
	d := &Dispatcher{st: st, q: make(chan Message, 256)}
	for _, n := range ns {
		if n.Enabled() {
			d.notifiers = append(d.notifiers, n)
		} else {
			slog.Info("notification channel disabled (credentials not set)", "channel", n.Name())
		}
	}
	for i := 0; i < 4; i++ {
		go d.worker(ctx)
	}
	return d
}

// Enqueue never blocks: when the queue is full the message is dropped and logged.
func (d *Dispatcher) Enqueue(m Message) {
	select {
	case d.q <- m:
	default:
		slog.Warn("notification queue is full; dropping alert", "kind", m.Kind)
	}
}

func (d *Dispatcher) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-d.q:
			d.deliver(ctx, m)
		}
	}
}

func (d *Dispatcher) deliver(ctx context.Context, m Message) {
	u, err := d.st.GetUser(ctx, m.Owner)
	if err != nil {
		slog.Error("notification: cannot read user", "err", err)
		return
	}
	r := Recipient{TelegramChatID: u.TelegramChatID}
	if u.EmailVerified {
		r.Email = u.Email
	}
	for _, n := range d.notifiers {
		id, err := d.st.InsertNotification(ctx, store.Notification{Owner: m.Owner, Channel: n.Name(), Kind: m.Kind, Subject: m.Subject, Body: m.Body})
		if err != nil {
			slog.Error("notification: cannot store", "err", err)
			continue
		}
		var lastErr error
		attempts := 0
		for attempts < 3 {
			attempts++
			sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			ok, err := n.Send(sctx, r, m)
			cancel()
			if !ok && err == nil {
				lastErr = ErrNoContact
				break
			}
			lastErr = err
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(attempts) * time.Second):
			}
		}
		switch {
		case lastErr == nil:
			_ = d.st.UpdateNotification(ctx, id, "sent", attempts, "")
		case errors.Is(lastErr, ErrNoContact):
			_ = d.st.UpdateNotification(ctx, id, "skipped", 0, "owner has not set up this channel")
		default:
			slog.Warn("notification failed", "channel", n.Name(), "err", lastErr)
			_ = d.st.UpdateNotification(ctx, id, "failed", attempts, lastErr.Error())
		}
	}
}

// Channel returns the notifier with the given name (nil when missing or disabled).
func (d *Dispatcher) Channel(name string) Notifier {
	for _, n := range d.notifiers {
		if n.Name() == name {
			return n
		}
	}
	return nil
}

// SendNow sends synchronously on one channel (test messages and verification codes).
func (d *Dispatcher) SendNow(ctx context.Context, channel string, r Recipient, m Message) error {
	n := d.Channel(channel)
	if n == nil {
		return fmt.Errorf("%s alerts are not set up on this server", channel)
	}
	ok, err := n.Send(ctx, r, m)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoContact
	}
	return nil
}
