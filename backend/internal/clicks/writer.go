// Package clicks batches click events and bulk-inserts them off the request path.
package clicks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/mileusna/useragent"
	"github.com/oschwald/geoip2-golang"

	"infopartnr-flow/backend/internal/db"
)

// Event is the raw data captured in the request. Expensive enrichment
// (UA parsing, geo lookup, hashing) happens later in the writer goroutine.
type Event struct {
	LinkID      uuid.UUID
	ClientID    uuid.UUID
	VideoID     uuid.NullUUID
	VariantID   uuid.NullUUID
	TrakyoID    string
	IP          string
	UserAgent   string
	Referrer    string
	CountryHint string // e.g. CF-IPCountry, used when no GeoIP DB is configured
}

type Writer struct {
	q         *db.Queries
	geo       *geoip2.Reader // may be nil
	salt      string
	batchSize int
	interval  time.Duration
	ch        chan Event
	done      chan struct{}
	dropped   atomic.Int64
}

func NewWriter(q *db.Queries, geo *geoip2.Reader, salt string, batchSize int, interval time.Duration) *Writer {
	w := &Writer{
		q: q, geo: geo, salt: salt, batchSize: batchSize, interval: interval,
		ch:   make(chan Event, batchSize*8),
		done: make(chan struct{}),
	}
	go w.run()
	return w
}

// Enqueue never blocks. If the buffer is full the click is dropped (and counted)
// so the redirect stays fast.
func (w *Writer) Enqueue(e Event) {
	select {
	case w.ch <- e:
	default:
		if n := w.dropped.Add(1); n%100 == 1 {
			log.Printf("click buffer full, dropped %d clicks so far", n)
		}
	}
}

// Close flushes remaining events. Call only after the HTTP server has stopped.
func (w *Writer) Close() {
	close(w.ch)
	<-w.done
}

func (w *Writer) run() {
	defer close(w.done)
	t := time.NewTicker(w.interval)
	defer t.Stop()

	batch := make([]db.InsertClicksParams, 0, w.batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := w.q.InsertClicks(ctx, batch); err != nil {
			log.Printf("click flush failed (%d rows): %v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case e, ok := <-w.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, w.enrich(e))
			if len(batch) >= w.batchSize {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

func (w *Writer) enrich(e Event) db.InsertClicksParams {
	sum := sha256.Sum256([]byte(w.salt + e.IP))
	ua := useragent.Parse(e.UserAgent)

	device := "desktop"
	switch {
	case ua.Bot:
		device = "bot"
	case ua.Tablet:
		device = "tablet"
	case ua.Mobile:
		device = "mobile"
	}

	country := strings.ToUpper(e.CountryHint)
	if w.geo != nil {
		if ip := net.ParseIP(e.IP); ip != nil {
			if rec, err := w.geo.Country(ip); err == nil && rec.Country.IsoCode != "" {
				country = rec.Country.IsoCode
			}
		}
	}
	if len(country) != 2 || country == "XX" || country == "T1" {
		country = ""
	}

	return db.InsertClicksParams{
		LinkID:     e.LinkID,
		VariantID:  e.VariantID,
		ClientID:   e.ClientID,
		VideoID:    e.VideoID,
		TrakyoID:   e.TrakyoID,
		IpHash:     hex.EncodeToString(sum[:]),
		UserAgent:  trunc(e.UserAgent, 512),
		Referrer:   trunc(e.Referrer, 512),
		Country:    country,
		DeviceType: device,
		Browser:    ua.Name,
		Os:         ua.OS,
		IsBot:      ua.Bot || e.UserAgent == "",
	}
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}