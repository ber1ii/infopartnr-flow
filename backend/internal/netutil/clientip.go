package netutil

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP resolves the request's client IP. trustProxy must only be true
// when a proxy you control (and that strips/overwrites these headers on any
// request it forwards) is the sole path to this process -- otherwise a
// direct request can forge them. This is the one place that trust decision
// is made; callers should not re-implement it.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
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
