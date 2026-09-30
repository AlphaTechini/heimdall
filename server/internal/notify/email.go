package notify

import (
	"context"
	"errors"

	"github.com/resend/resend-go/v2"
)

// Email sends through Resend's Go SDK.
type Email struct {
	client *resend.Client
	from   string
	on     bool
}

// NewEmail creates the channel. A missing key or sender disables it.
func NewEmail(apiKey, from string) *Email {
	e := &Email{from: from, on: apiKey != "" && from != ""}
	if e.on {
		e.client = resend.NewClient(apiKey)
	}
	return e
}

func (e *Email) Name() string  { return "email" }
func (e *Email) Enabled() bool { return e.on }

// Send delivers to the owner's verified address.
func (e *Email) Send(ctx context.Context, r Recipient, m Message) (bool, error) {
	if r.Email == "" {
		return false, nil
	}
	return true, e.SendTo(ctx, r.Email, m.Subject, m.Body)
}

// SendTo sends a plain-text email.
func (e *Email) SendTo(ctx context.Context, to, subject, body string) error {
	if !e.on {
		return errors.New("email alerts are not set up on this server")
	}
	_, err := e.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From: e.from, To: []string{to}, Subject: subject, Text: body,
	})
	return err
}
