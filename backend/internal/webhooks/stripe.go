// Package webhooks receives provider webhooks and turns them into conversions.
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"infopartnr-flow/backend/internal/attribution"
	"infopartnr-flow/backend/internal/crypto"
	"infopartnr-flow/backend/internal/db"
)

const sigTolerance = 5 * time.Minute

type Stripe struct {
	Q   *db.Queries
	Box *crypto.Box
}

type event struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

// Serve handles POST /webhooks/stripe/{integrationID}.
// Non-2xx makes Stripe retry; inserts are idempotent so retries are safe.
func (h *Stripe) Serve(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "integrationID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	integ, err := h.Q.GetIntegrationForWebhook(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("stripe webhook: integration lookup: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if integ.Provider != "stripe" || !integ.IsActive {
		http.NotFound(w, r)
		return
	}
	if len(integ.WebhookSecretEnc) == 0 {
		http.Error(w, "signing secret not configured", http.StatusServiceUnavailable)
		return
	}
	secret, err := h.Box.Decrypt(integ.WebhookSecretEnc)
	if err != nil {
		log.Printf("stripe webhook: decrypt secret (integration %s): %v", id, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !verifySignature(body, r.Header.Get("Stripe-Signature"), secret, time.Now()) {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}

	var ev event
	if err := json.Unmarshal(body, &ev); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := h.handle(r.Context(), integ.ClientID, ev); err != nil {
		log.Printf("stripe webhook: %s %s: %v", ev.Type, ev.ID, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// verifySignature implements Stripe's scheme: HMAC-SHA256 over "{t}.{payload}", header "t=..,v1=..".
func verifySignature(payload []byte, header string, secret []byte, now time.Time) bool {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	if ts == "" || len(sigs) == 0 {
		return false
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := now.Sub(time.Unix(sec, 0)); d > sigTolerance || d < -sigTolerance {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(payload)
	want := mac.Sum(nil)
	for _, s := range sigs {
		got, err := hex.DecodeString(s)
		if err == nil && hmac.Equal(got, want) {
			return true
		}
	}
	return false
}

func (h *Stripe) handle(ctx context.Context, clientID uuid.UUID, ev event) error {
	switch ev.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		return h.checkout(ctx, clientID, ev.Data.Object)
	case "invoice.payment_succeeded":
		return h.invoice(ctx, clientID, ev.Data.Object)
	case "refund.created":
		return h.refund(ctx, clientID, ev.Data.Object)
	case "customer.subscription.deleted":
		var s struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(ev.Data.Object, &s) != nil || s.ID == "" {
			return nil
		}
		return h.Q.CancelSubscription(ctx, db.CancelSubscriptionParams{ClientID: clientID, ExternalID: s.ID})
	default:
		return nil // acknowledged, not tracked
	}
}

// ---- checkout.session.completed ----

type checkoutSession struct {
	ID                string            `json:"id"`
	Mode              string            `json:"mode"`
	PaymentStatus     string            `json:"payment_status"`
	ClientReferenceID string            `json:"client_reference_id"`
	AmountTotal       int64             `json:"amount_total"`
	Currency          string            `json:"currency"`
	Created           int64             `json:"created"`
	CustomerEmail     string            `json:"customer_email"`
	Subscription      string            `json:"subscription"`
	Metadata          map[string]string `json:"metadata"`
	CustomerDetails   struct {
		Email string `json:"email"`
	} `json:"customer_details"`
}

// Revenue rules: one-time purchases and the FIRST subscription payment are recorded here
// (subscription_start, amount_total). invoice.payment_succeeded skips billing_reason
// "subscription_create" so the first payment is never counted twice.
func (h *Stripe) checkout(ctx context.Context, clientID uuid.UUID, obj json.RawMessage) error {
	var s checkoutSession
	if json.Unmarshal(obj, &s) != nil || s.ID == "" {
		return nil
	}
	if s.Mode != "payment" && s.Mode != "subscription" {
		return nil
	}
	if s.PaymentStatus == "unpaid" {
		return nil // async payment pending; async_payment_succeeded will follow
	}
	email := firstNonEmpty(s.CustomerDetails.Email, s.CustomerEmail)
	tid := firstNonEmpty(s.ClientReferenceID, s.Metadata["trakyo_id"])

	att, err := attribution.Resolve(ctx, h.Q, clientID, tid, email)
	if err != nil {
		return err
	}
	occurred := time.Unix(s.Created, 0).UTC()

	eventType := "purchase"
	var subID uuid.NullUUID
	if s.Mode == "subscription" && s.Subscription != "" {
		eventType = "subscription_start"
		id, err := h.Q.UpsertSubscription(ctx, db.UpsertSubscriptionParams{
			ClientID:      clientID,
			ExternalID:    s.Subscription,
			CustomerEmail: strings.ToLower(email),
			ClickID:       att.ClickID,
			LinkID:        att.LinkID,
			VideoID:       att.VideoID,
			StartedAt:     occurred,
		})
		if err != nil {
			return err
		}
		subID = uuid.NullUUID{UUID: id, Valid: true}
	}

	return h.insert(ctx, db.InsertConversionParams{
		ClientID:          clientID,
		ClickID:           att.ClickID,
		LinkID:            att.LinkID,
		VideoID:           att.VideoID,
		SubscriptionID:    subID,
		TrakyoID:          att.TrakyoID,
		Source:            "stripe",
		EventType:         eventType,
		ExternalID:        s.ID,
		AmountCents:       s.AmountTotal,
		Currency:          strings.ToUpper(s.Currency),
		Email:             strings.ToLower(email),
		AttributionMethod: att.Method,
		Raw:               []byte(obj),
		OccurredAt:        occurred,
	})
}

// ---- invoice.payment_succeeded (renewals) ----

type invoice struct {
	ID            string `json:"id"`
	Subscription  string `json:"subscription"` // API versions before 2025-03
	BillingReason string `json:"billing_reason"`
	AmountPaid    int64  `json:"amount_paid"`
	Currency      string `json:"currency"`
	CustomerEmail string `json:"customer_email"`
	Created       int64  `json:"created"`
	Parent        *struct {
		SubscriptionDetails *struct {
			Subscription string `json:"subscription"`
		} `json:"subscription_details"`
	} `json:"parent"` // API versions from 2025-03
	StatusTransitions struct {
		PaidAt int64 `json:"paid_at"`
	} `json:"status_transitions"`
}

func (h *Stripe) invoice(ctx context.Context, clientID uuid.UUID, obj json.RawMessage) error {
	var inv invoice
	if json.Unmarshal(obj, &inv) != nil || inv.ID == "" {
		return nil
	}
	subExt := inv.Subscription
	if subExt == "" && inv.Parent != nil && inv.Parent.SubscriptionDetails != nil {
		subExt = inv.Parent.SubscriptionDetails.Subscription
	}
	// Non-subscription invoices are skipped (checkout already records those);
	// first payment is recorded by checkout.session.completed; $0 invoices (trials) carry no revenue.
	if subExt == "" || inv.BillingReason == "subscription_create" || inv.AmountPaid <= 0 {
		return nil
	}

	when := inv.Created
	if inv.StatusTransitions.PaidAt > 0 {
		when = inv.StatusTransitions.PaidAt
	}
	email := strings.ToLower(inv.CustomerEmail)

	p := db.InsertConversionParams{
		ClientID:          clientID,
		Source:            "stripe",
		EventType:         "subscription_renewal",
		ExternalID:        inv.ID,
		AmountCents:       inv.AmountPaid,
		Currency:          strings.ToUpper(inv.Currency),
		Email:             email,
		AttributionMethod: "none",
		Raw:               []byte(obj),
		OccurredAt:        time.Unix(when, 0).UTC(),
	}

	sub, err := h.Q.GetSubscriptionByExternalID(ctx, db.GetSubscriptionByExternalIDParams{ClientID: clientID, ExternalID: subExt})
	switch {
	case err == nil:
		p.SubscriptionID = uuid.NullUUID{UUID: sub.ID, Valid: true}
		if p.Email == "" {
			p.Email = sub.CustomerEmail
		}
		if sub.ClickID.Valid {
			p.ClickID, p.LinkID, p.VideoID = sub.ClickID, sub.LinkID, sub.VideoID
			p.AttributionMethod = "track_id"
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}

	if !p.ClickID.Valid && p.Email != "" {
		att, err := attribution.Resolve(ctx, h.Q, clientID, "", p.Email)
		if err != nil {
			return err
		}
		p.ClickID, p.LinkID, p.VideoID, p.TrakyoID, p.AttributionMethod = att.ClickID, att.LinkID, att.VideoID, att.TrakyoID, att.Method
	}
	return h.insert(ctx, p)
}

// ---- refund.created ----

type refundObj struct {
	ID            string `json:"id"`
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
	Charge        string `json:"charge"`
	PaymentIntent string `json:"payment_intent"`
	Status        string `json:"status"`
	Created       int64  `json:"created"`
}

// Refunds are stored as negative amounts and inherit attribution from the original payment
// (matched by payment_intent/charge inside the stored Stripe object). If no original is found
// (e.g. newer-API invoices), the refund still reduces client revenue but is unattributed.
func (h *Stripe) refund(ctx context.Context, clientID uuid.UUID, obj json.RawMessage) error {
	var rf refundObj
	if json.Unmarshal(obj, &rf) != nil || rf.ID == "" {
		return nil
	}
	if rf.Status == "failed" || rf.Status == "canceled" {
		return nil
	}
	p := db.InsertConversionParams{
		ClientID:          clientID,
		Source:            "stripe",
		EventType:         "refund",
		ExternalID:        rf.ID,
		AmountCents:       -rf.Amount,
		Currency:          strings.ToUpper(rf.Currency),
		AttributionMethod: "none",
		Raw:               []byte(obj),
		OccurredAt:        time.Unix(rf.Created, 0).UTC(),
	}
	if rf.PaymentIntent != "" || rf.Charge != "" {
		orig, err := h.Q.FindConversionByPayment(ctx, db.FindConversionByPaymentParams{
			ClientID: clientID, PaymentIntent: rf.PaymentIntent, Charge: rf.Charge,
		})
		switch {
		case err == nil:
			p.ClickID, p.LinkID, p.VideoID, p.SubscriptionID = orig.ClickID, orig.LinkID, orig.VideoID, orig.SubscriptionID
			p.TrakyoID, p.Email, p.AttributionMethod = orig.TrakyoID, orig.Email, orig.AttributionMethod
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
	}
	return h.insert(ctx, p)
}

// insert treats a duplicate (ON CONFLICT DO NOTHING -> no row) as success.
func (h *Stripe) insert(ctx context.Context, p db.InsertConversionParams) error {
	_, err := h.Q.InsertConversion(ctx, p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
