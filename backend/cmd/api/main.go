package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"infopartnr-flow/backend/internal/db"
)

type Server struct {
	queries *db.Queries
}

// generateTrakyoID creates a 36-character URL-safe secure tracking ID.
// Total length is kept strictly under 255 characters to ensure compatibility
// with Calendly UTM passthrough limits.
func generateTrakyoID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback in the highly unlikely event of a system entropy failure
		return "clk_entropy_failure_fallback"
	}
	return "clk_" + hex.EncodeToString(b)
}

func main() {
	ctx := context.Background()
	// Connection string maps directly to the local docker-compose setup
	dbURL := "postgres://postgres:postgrespassword@localhost:5432/infopartnr-flow"

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v", err)
	}
	defer pool.Close()

	srv := &Server{
		queries: db.New(pool),
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Serve the robust track.js SDK directly from the backend
	r.Get("/track.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		http.ServeFile(w, r, "../frontend/public/track.js")
	})

	r.Get("/{slug}", srv.HandleRedirect)

	fmt.Println("Redirect Engine running on :8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}

func (s *Server) HandleRedirect(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	ctx := r.Context()

	// 1. Fetch Target URL
	link, err := s.queries.GetLinkBySlug(ctx, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 2. Generate unique tracking ID (Safe for Calendly UTM constraints)
	trakyoID := generateTrakyoID()

	// 3. Fire-and-forget background click tracking
	ip := r.RemoteAddr
	userAgent := r.UserAgent()

	go func(linkID pgtype.UUID, tID, ipAddr, ua string) {
		bgCtx := context.Background()

		hash := sha256.Sum256([]byte(ipAddr))
		ipHash := hex.EncodeToString(hash[:])

		err := s.queries.InsertClick(bgCtx, db.InsertClickParams{
			LinkID:    linkID,
			TrakyoID:  tID,
			IpHash:    pgtype.Text{String: ipHash, Valid: true},
			UserAgent: pgtype.Text{String: ua, Valid: true},
		})
		if err != nil {
			log.Printf("Failed to insert click asynchronously: %v", err)
		}
	}(link.ID, trakyoID, ip, userAgent)

	// 4. Redirect immediately
	redirectURL := fmt.Sprintf("%s?trakyo_id=%s", link.TargetUrl, trakyoID)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}
