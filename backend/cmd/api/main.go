package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/oschwald/geoip2-golang"

	"infopartnr-flow/backend/internal/api"
	"infopartnr-flow/backend/internal/auth"
	"infopartnr-flow/backend/internal/clicks"
	"infopartnr-flow/backend/internal/collect"
	"infopartnr-flow/backend/internal/config"
	"infopartnr-flow/backend/internal/crypto"
	"infopartnr-flow/backend/internal/db"
	"infopartnr-flow/backend/internal/redirect"
	"infopartnr-flow/backend/internal/webhooks"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Printf("Notice: could not load .env file: %v", err)
	}

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("database ping: %v", err)
	}
	q := db.New(pool)

	box, err := crypto.New(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("encryption: %v", err)
	}

	var geo *geoip2.Reader
	if cfg.GeoIPPath != "" {
		if geo, err = geoip2.Open(cfg.GeoIPPath); err != nil {
			log.Fatalf("geoip: %v", err)
		}
		defer geo.Close()
	}

	writer := clicks.NewWriter(q, geo, cfg.IPSalt, cfg.ClickBatchSize, cfg.ClickFlushInterval)

	cache := redirect.NewCache(30*time.Second, 10*time.Second)
	rh := &redirect.Handler{
		Q:            q,
		Cache:        cache,
		Clicks:       writer,
		TrustProxy:   cfg.TrustProxy,
		CookieDomain: cfg.CookieDomain,
	}

	// No request logger on the hot path; add sampled logging if needed.
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	r.Get("/track.js", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeFile(w, req, cfg.TrackJSPath)
	})
	apiH := &api.API{Q: q, Pool: pool, Auth: auth.NewManager(cfg.JWTSecret, cfg.JWTTTL), Cache: cache, AppURL: cfg.AppURL, Box: box, PublicURL: cfg.PublicURL, GoogleClientID: cfg.GoogleClientID, GoogleClientSecret: cfg.GoogleClientSecret}
	apiH.StartYoutubeSync(ctx, 6*time.Hour)
	r.Mount("/api", apiH.Routes())

	stripeH := &webhooks.Stripe{Q: q, Box: box}
	r.Post("/webhooks/stripe/{integrationID}", stripeH.Serve)
	typeformH := &webhooks.Typeform{Q: q, Box: box}
	r.Post("/webhooks/typeform/{integrationID}", typeformH.Serve)
	calendlyH := &webhooks.Calendly{Q: q, Box: box}
	r.Post("/webhooks/calendly/{integrationID}", calendlyH.Serve)

	collectH := &collect.Handler{Q: q}
	r.Post("/collect", collectH.Serve)
	r.Options("/collect", collectH.Serve)

	r.Get("/{slug}", rh.Serve)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	writer.Close() // flush pending clicks after HTTP has stopped
}
