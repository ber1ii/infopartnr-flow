package redirect

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"infopartnr-flow/backend/internal/clicks"
	"infopartnr-flow/backend/internal/db"
)

const cookieName = "trakyo_id"

type Handler struct {
	Q            *db.Queries
	Cache        *Cache
	Clicks       *clicks.Writer
	TrustProxy   bool
	CookieDomain string
}

func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	link, err := h.load(r, slug)
	if err != nil {
		log.Printf("link lookup failed for %q: %v", slug, err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if link == nil || !link.Active || (link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt)) {
		http.NotFound(w, r)
		return
	}

	target := link.TargetURL
	var variantID uuid.NullUUID
	if v := h.chooseVariant(w, r, link); v != nil {
		target = v.TargetURL
		variantID = uuid.NullUUID{UUID: v.ID, Valid: true}
	}

	id := newTrakyoID()
	target = decorateForProvider(target, id)
	dest, err := withParam(target, "trakyo_id", id)
	if err != nil {
		log.Printf("bad target url on link %s: %v", link.ID, err)
		http.Error(w, "invalid destination", http.StatusInternalServerError)
		return
	}

	ev := clicks.Event{
		LinkID:    link.ID,
		ClientID:  link.ClientID,
		VideoID:   link.VideoID,
		VariantID: variantID,
		TrakyoID:  id,
		IP:        h.clientIP(r),
		UserAgent: r.UserAgent(),
		Referrer:  r.Referer(),
	}
	if h.TrustProxy {
		ev.CountryHint = r.Header.Get("CF-IPCountry")
	}
	h.Clicks.Enqueue(ev)

	if h.CookieDomain != "" {
		http.SetCookie(w, &http.Cookie{
			Name: cookieName, Value: id, Path: "/", Domain: h.CookieDomain,
			MaxAge: 30 * 24 * 3600, SameSite: http.SameSiteLaxMode,
			Secure:   r.TLS != nil || (h.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https"),
			HttpOnly: false, // track.js must read it
		})
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, dest, http.StatusFound)
}

// Sticky: a per-link cookie on the short-link host remembers the assigned variant.
func (h *Handler) chooseVariant(w http.ResponseWriter, r *http.Request, link *Link) *Variant {
	vs := link.Variants
	if len(vs) < 2 {
		return pickVariant(vs)
	}
	name := "flv_" + link.ID.String()[:8]
	if c, err := r.Cookie(name); err == nil {
		for i := range vs {
			if vs[i].ID.String() == c.Value {
				return &vs[i]
			}
		}
	}
	v := pickVariant(vs)
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: v.ID.String(), Path: "/", MaxAge: 30 * 24 * 3600,
		SameSite: http.SameSiteLaxMode, HttpOnly: true,
		Secure: r.TLS != nil || (h.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https"),
	})
	return v
}

func (h *Handler) load(r *http.Request, slug string) (*Link, error) {
	if l, ok := h.Cache.Get(slug); ok {
		return l, nil
	}
	ctx := r.Context()
	row, err := h.Q.GetLinkForRedirect(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		h.Cache.Set(slug, nil)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	vs, err := h.Q.ListActiveVariants(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	l := &Link{
		ID: row.ID, ClientID: row.ClientID, VideoID: row.VideoID,
		TargetURL: row.TargetUrl, Active: row.IsActive, ExpiresAt: row.ExpiresAt,
	}
	for _, v := range vs {
		l.Variants = append(l.Variants, Variant{ID: v.ID, TargetURL: v.TargetUrl, Weight: v.Weight})
	}
	h.Cache.Set(slug, l)
	return l, nil
}

func (h *Handler) clientIP(r *http.Request) string {
	if h.TrustProxy {
		if v := r.Header.Get("CF-Connecting-IP"); v != "" {
			return v
		}
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			return strings.TrimSpace(strings.Split(v, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func pickVariant(vs []Variant) *Variant {
	switch len(vs) {
	case 0:
		return nil
	case 1:
		return &vs[0]
	}
	total := 0
	for _, v := range vs {
		total += int(v.Weight)
	}
	n := rand.IntN(total)
	for i := range vs {
		n -= int(vs[i].Weight)
		if n < 0 {
			return &vs[i]
		}
	}
	return &vs[len(vs)-1]
}

// decorateForProvider mirrors track.js's per-provider decoration rules,
// applied server-side so direct-to-provider target_urls (no client page in
// between) still get attribution. The generic trakyo_id param is appended
// separately by the caller via withParam; this only adds provider-specific
// extras. checkout.stripe.com is intentionally left untouched: it needs
// client_reference_id set server-side at Checkout Session creation time on
// the client's own backend, which a redirect can't do.
func decorateForProvider(target, trakyoID string) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	host := strings.ToLower(u.Hostname())

	switch {
	case host == "buy.stripe.com" || strings.HasSuffix(host, ".buy.stripe.com"):
		if out, err := withParam(target, "client_reference_id", trakyoID); err == nil {
			return out
		}
	case host == "calendly.com" || strings.HasSuffix(host, ".calendly.com"):
		if out, err := withParam(target, "utm_content", trakyoID); err == nil {
			return out
		}
	case host == "typeform.com" || strings.HasSuffix(host, ".typeform.com"):
		// trakyo_id itself is added by the caller for all targets already;
		// standalone Typeform links need nothing further.
	}
	return target
}

// withParam appends k=v while preserving the original query order and fragment.
func withParam(raw, k, v string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	p := k + "=" + url.QueryEscape(v)
	if u.RawQuery == "" {
		u.RawQuery = p
	} else {
		u.RawQuery += "&" + p
	}
	return u.String(), nil
}

// newTrakyoID returns "clk_" + 32 hex chars (36 total, safe for Calendly UTM limits).
func newTrakyoID() string {
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		panic(err) // entropy failure: never issue a non-unique id; Recoverer returns 500
	}
	return "clk_" + hex.EncodeToString(b)
}
