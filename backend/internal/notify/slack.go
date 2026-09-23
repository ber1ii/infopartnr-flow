// Package notify sends Slack notifications for revenue events.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"infopartnr-flow/backend/internal/db"
)

type Slack struct {
	Q *db.Queries
}

type ConversionEvent struct {
	ClientID    uuid.UUID
	EventType   string // purchase, subscription_start, subscription_renewal
	AmountCents int64
	Currency    string
	Email       string
	VideoTitle  string // "" if unattributed
}

// Fire looks up active Slack channels for this client (or global), filters by
// event type + min_amount, and posts. Call as `go notify.Fire(...)` — never
// block the webhook response on Slack being slow/down.
func (s *Slack) Fire(ctx context.Context, ev ConversionEvent) {
	channels, err := s.Q.ListActiveSlackChannels(ctx, uuid.NullUUID{UUID: ev.ClientID, Valid: true})
	if err != nil {
		log.Printf("slack notify: list channels: %v", err)
		return
	}
	for _, ch := range channels {
		if !containsEvent(ch.Events, ev.EventType) {
			continue
		}
		if ev.AmountCents < ch.MinAmountCents {
			continue
		}
		msg := buildMessage(ev, ch.MinAmountCents)
		if err := post(ch.WebhookUrl, msg); err != nil {
			log.Printf("slack notify: post to channel %s: %v", ch.ID, err)
		}
	}
}

const largeSaleThresholdCents = 50000 // $500 — display-only split; actual filtering is per-channel MinAmountCents

func buildMessage(ev ConversionEvent, _ int64) string {
	amount := fmt.Sprintf("$%.2f", float64(ev.AmountCents)/100)
	label := map[string]string{
		"purchase":             "New sale",
		"subscription_start":   "New subscriber",
		"subscription_renewal": "Subscription renewed",
	}[ev.EventType]
	if label == "" {
		label = ev.EventType
	}

	emoji := "💰"
	prefix := ""
	if ev.AmountCents >= largeSaleThresholdCents {
		emoji = "🚨"
		prefix = "*Big one!* "
	}

	video := ""
	if ev.VideoTitle != "" {
		video = fmt.Sprintf(" via _%s_", ev.VideoTitle)
	}

	return fmt.Sprintf("%s %s%s: *%s*%s (%s)", emoji, prefix, label, amount, video, ev.Email)
}

func containsEvent(events []string, want string) bool {
	for _, e := range events {
		if e == want {
			return true
		}
	}
	return false
}

func post(webhookURL, text string) error {
	body, _ := json.Marshal(map[string]string{"text": text})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned %d", resp.StatusCode)
	}
	return nil
}
