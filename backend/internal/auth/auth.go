// Package auth handles passwords, JWTs and role-based middleware.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	RoleOwner  = "owner"
	RoleAdmin  = "agency_admin"
	RoleClient = "client"
)

// Claims embed the tenant scope so requests need no membership lookup.
// Trade-off: role changes apply after token expiry.
type Claims struct {
	WorkspaceID uuid.UUID  `json:"wid"`
	Role        string     `json:"role"`
	ClientID    *uuid.UUID `json:"cid,omitempty"`
	jwt.RegisteredClaims
}

type Principal struct {
	UserID      uuid.UUID
	WorkspaceID uuid.UUID
	Role        string
	ClientID    uuid.UUID // zero unless Role == RoleClient
}

func (p Principal) IsStaff() bool { return p.Role == RoleOwner || p.Role == RoleAdmin }

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl}
}

func (m *Manager) Issue(userID, workspaceID uuid.UUID, role string, clientID *uuid.UUID) (string, error) {
	now := time.Now()
	c := Claims{
		WorkspaceID: workspaceID, Role: role, ClientID: clientID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)
}

func (m *Manager) parse(tok string) (Principal, error) {
	var c Claims
	t, err := jwt.ParseWithClaims(tok, &c,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !t.Valid {
		return Principal{}, errors.New("invalid token")
	}
	uid, err := uuid.Parse(c.Subject)
	if err != nil {
		return Principal{}, errors.New("invalid subject")
	}
	p := Principal{UserID: uid, WorkspaceID: c.WorkspaceID, Role: c.Role}
	if c.ClientID != nil {
		p.ClientID = *c.ClientID
	}
	return p, nil
}

type ctxKey struct{}

func FromContext(ctx context.Context) Principal {
	p, _ := ctx.Value(ctxKey{}).(Principal)
	return p
}

// Middleware requires a valid "Authorization: Bearer <jwt>" header.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		p, err := m.parse(tok)
		if err != nil {
			jsonErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

// RequireStaff allows owner and agency_admin only.
func RequireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !FromContext(r.Context()).IsStaff() {
			jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jsonErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ---- passwords ----

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

var dummyHash = sync.OnceValue(func() string {
	h, _ := HashPassword("dummy-password-for-timing")
	return h
})

// BurnPassword spends the same time as a real check, so unknown emails
// can't be told apart from wrong passwords by response time.
func BurnPassword(pw string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash()), []byte(pw))
}
