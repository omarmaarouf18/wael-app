// Package mailer sends one-time codes. LogSender logs the code (localhost
// dev); ResendSender delivers via the Resend HTTP API when RESEND_API_KEY is
// set. The sender address always comes from RESEND_FROM_EMAIL env config —
// there is no hardcoded brand sender in this package.
package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Sender delivers a one-time code to an email address.
type Sender interface {
	SendCode(ctx context.Context, toEmail, code, purpose string) error
	// SendNotice delivers a plain-text notice email (no code), e.g. account
	// email-change and self-deletion notices (F-UX2).
	SendNotice(ctx context.Context, toEmail, subject, text string) error
}

// LogSender logs codes to stdout (localhost dev, tests).
type LogSender struct{}

// SendCode logs the code; never fails.
func (LogSender) SendCode(_ context.Context, toEmail, code, purpose string) error {
	// #nosec G706 -- toEmail/purpose sanitized for CR/LF; code is numeric
	log.Printf("[MAIL] purpose=%s to=%s code=%s", sanitize(toEmail), sanitize(purpose), code)
	return nil
}

// SendNotice logs the notice; never fails.
func (LogSender) SendNotice(_ context.Context, toEmail, subject, text string) error {
	// #nosec G706 -- fields sanitized for CR/LF
	log.Printf("[MAIL] notice to=%s subject=%s text=%s", sanitize(toEmail), sanitize(subject), sanitize(text))
	return nil
}

// ResendSender delivers via https://api.resend.com/emails.
type ResendSender struct {
	APIKey string
	From   string
	Client *http.Client
}

// SendCode posts a plain-text code email via Resend.
func (s *ResendSender) SendCode(ctx context.Context, toEmail, code, purpose string) error {
	return s.send(ctx, toEmail, "Your verification code ("+purpose+")",
		"Your verification code is: "+code+"\nIt expires in 10 minutes.")
}

// SendNotice posts a plain-text notice email via Resend.
func (s *ResendSender) SendNotice(ctx context.Context, toEmail, subject, text string) error {
	return s.send(ctx, toEmail, subject, text)
}

func (s *ResendSender) send(ctx context.Context, toEmail, subject, text string) error {
	if s.APIKey == "" || s.From == "" {
		return fmt.Errorf("mailer: resend api key/from not configured")
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	body, _ := json.Marshal(map[string]string{
		"from":    s.From,
		"to":      toEmail,
		"subject": subject,
		"text":    text,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mailer: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("mailer: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mailer: resend status %d", resp.StatusCode)
	}
	return nil
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			out = append(out, ' ')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
