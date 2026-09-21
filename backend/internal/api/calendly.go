package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const calendlyAPIBase = "https://api.calendly.com"

// calendlyClient talks to Calendly's own REST API using the staff member's
// personal access token -- only for the duration of the connect request.
// The token is never persisted; only the signing key we generate ourselves
// and hand to Calendly is stored (encrypted) afterward.
type calendlyClient struct {
	token string
	hc    *http.Client
}

func newCalendlyClient(token string) *calendlyClient {
	return &calendlyClient{token: token, hc: &http.Client{Timeout: 10 * time.Second}}
}

func (c *calendlyClient) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, calendlyAPIBase+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("calendly %s %s: %d %s", method, path, resp.StatusCode, string(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// currentUser returns the token owner's organization URI, needed to
// register an organization-scoped webhook subscription.
func (c *calendlyClient) currentUser(ctx context.Context) (orgURI string, err error) {
	var me struct {
		Resource struct {
			URI                 string `json:"uri"`
			CurrentOrganization string `json:"current_organization"`
		} `json:"resource"`
	}
	if err = c.do(ctx, http.MethodGet, "/users/me", nil, &me); err != nil {
		return "", err
	}
	if me.Resource.CurrentOrganization == "" {
		return "", errors.New("token has no organization (requires an org admin's personal access token)")
	}
	return me.Resource.CurrentOrganization, nil
}

// createWebhookSubscription registers an organization-scoped webhook for
// invitee.created, so bookings made with any staff member's link are
// caught, and returns its Calendly-side resource URI.
func (c *calendlyClient) createWebhookSubscription(ctx context.Context, callbackURL, orgURI, signingKey string) (string, error) {
	body := map[string]any{
		"url":          callbackURL,
		"events":       []string{"invitee.created"},
		"organization": orgURI,
		"scope":        "organization",
		"signing_key":  signingKey,
	}
	var out struct {
		Resource struct {
			URI string `json:"uri"`
		} `json:"resource"`
	}
	if err := c.do(ctx, http.MethodPost, "/webhook_subscriptions", body, &out); err != nil {
		return "", err
	}
	return out.Resource.URI, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
