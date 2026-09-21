package config

import (
	"log"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port               string
	DatabaseURL        string
	IPSalt             string
	GeoIPPath          string // optional MaxMind GeoLite2-Country.mmdb
	TrackJSPath        string
	TrustProxy         bool   // trust CF-Connecting-IP / X-Forwarded-For (only behind a proxy you control)
	CookieDomain       string // e.g. ".clientbrand.com"; empty = don't set a cookie on redirect
	ClickBatchSize     int
	ClickFlushInterval time.Duration
	JWTSecret          string
	JWTTTL             time.Duration
	AppURL             string // frontend base URL, used in invite links
	PublicURL          string // backend public base URL, used in webhook URLs and the youtube oauth callback
	EncryptionKey      string // 64 hex chars (32 bytes) for AES-GCM
	GoogleClientID     string // from Google Cloud Console OAuth client
	GoogleClientSecret string
}

func Load() Config {
	c := Config{
		Port:               getenv("PORT", "8080"),
		DatabaseURL:        getenv("DATABASE_URL", "postgres://postgres:postgrespassword@localhost:5432/infopartnr-flow"),
		IPSalt:             getenv("IP_SALT", "dev-only-change-me"),
		GeoIPPath:          os.Getenv("GEOIP_DB"),
		TrackJSPath:        getenv("TRACK_JS_PATH", "../frontend/public/track.js"),
		TrustProxy:         getenv("TRUST_PROXY", "false") == "true",
		CookieDomain:       os.Getenv("COOKIE_DOMAIN"),
		ClickBatchSize:     atoi(getenv("CLICK_BATCH_SIZE", "500")),
		ClickFlushInterval: time.Duration(atoi(getenv("CLICK_FLUSH_MS", "1000"))) * time.Millisecond,
		AppURL:             getenv("APP_URL", "http://localhost:5173"),
		PublicURL:          getenv("PUBLIC_URL", "http://localhost:8080"),
		EncryptionKey:      getenv("ENCRYPTION_KEY", devEncryptionKey),
		JWTSecret:          getenv("JWT_SECRET", "dev-only-jwt-secret-change-me"),
		JWTTTL:             time.Duration(atoi(getenv("JWT_TTL_HOURS", "12"))) * time.Hour,
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
	}
	if c.IPSalt == "dev-only-change-me" {
		log.Println("WARNING: using default IP_SALT, set IP_SALT in production")
	}
	if c.JWTSecret == "dev-only-jwt-secret-change-me" {
		log.Println("WARNING: using default JWT_SECRET, set JWT_SECRET in production")
	}
	if c.EncryptionKey == devEncryptionKey {
		log.Println("WARNING: using default ENCRYPTION_KEY, set ENCRYPTION_KEY (openssl rand -hex 32) in production")
	}
	if c.GoogleClientID == "" || c.GoogleClientSecret == "" {
		log.Println("WARNING: GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET not set, YouTube connect will fail")
	}
	return c
}

const devEncryptionKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("invalid integer env value %q", s)
	}
	return n
}
