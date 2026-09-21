package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"infopartnr-flow/backend/internal/auth"
	"infopartnr-flow/backend/internal/crypto"
	"infopartnr-flow/backend/internal/db"
	"infopartnr-flow/backend/internal/redirect"
)

type API struct {
	Q    *db.Queries
	Pool *pgxpool.Pool
	Auth *auth.Manager
	// Cache is the redirect cache; link edits must invalidate it.
	Cache              *redirect.Cache
	AppURL             string
	Box                *crypto.Box // encrypts integration secrets + youtube oauth state
	PublicURL          string      // backend public base URL, shown in webhook URLs and the oauth callback
	GoogleClientID     string
	GoogleClientSecret string
}

func (a *API) Routes() http.Handler {
	r := chi.NewRouter()

	r.Post("/auth/register", a.register) // bootstrap only: works while no users exist
	r.Post("/auth/login", a.login)
	r.Get("/invites/{token}", a.inviteInfo)           // public
	r.Post("/invites/{token}/accept", a.acceptInvite) // public

	// Public: Google redirects the bare browser here with no Bearer token.
	// The client/workspace identity comes from the signed state param.
	r.Get("/youtube/oauth/callback", a.youtubeOAuthCallback)

	r.Group(func(r chi.Router) {
		r.Use(a.Auth.Middleware)
		r.Get("/me", a.me)

		r.Route("/clients", func(r chi.Router) {
			r.With(auth.RequireStaff).Get("/", a.listClients)
			r.With(auth.RequireStaff).Post("/", a.createClient)

			r.Route("/{clientID}", func(r chi.Router) {
				r.Use(a.clientCtx) // tenant + role check for everything below
				r.Get("/", a.getClient)

				r.Get("/conversions", a.listConversions)
				r.Get("/stats/overview", a.statsOverview)
				r.Get("/stats/clicks-by-day", a.statsClicksByDay)

				r.Route("/invites", func(r chi.Router) {
					r.Use(auth.RequireStaff)
					r.Get("/", a.listInvites)
					r.Post("/", a.createInvite)
				})

				r.Route("/integrations", func(r chi.Router) {
					r.Use(auth.RequireStaff)
					r.Get("/", a.listIntegrations)
					r.Post("/", a.createIntegration)
					r.Put("/{integrationID}/secret", a.setIntegrationSecret)
					r.Delete("/{integrationID}", a.deleteIntegration)
				})

				r.Route("/links", func(r chi.Router) {
					r.Get("/", a.listLinks)
					r.With(auth.RequireStaff).Post("/", a.createLink)
					r.With(auth.RequireStaff).Patch("/{linkID}", a.updateLink)
					r.With(auth.RequireStaff).Delete("/{linkID}", a.deleteLink)
				})

				// Video costs: staff-only, mirrors the integrations pattern
				// (agency-internal spend data, not exposed to client-role reads).
				r.Route("/videos", func(r chi.Router) {
					r.Use(auth.RequireStaff)
					r.Get("/", a.listVideos)
					r.Route("/{videoID}/costs", func(r chi.Router) {
						r.Get("/", a.listVideoCosts)
						r.Post("/", a.addVideoCost)
						r.Delete("/{costID}", a.deleteVideoCost)
					})
				})

				r.Route("/youtube", func(r chi.Router) {
					r.Get("/channels", a.listYoutubeChannels)
					r.With(auth.RequireStaff).Post("/connect", a.connectYoutube)
					r.With(auth.RequireStaff).Post("/channels/{channelID}/sync", a.syncYoutubeChannel)
					r.With(auth.RequireStaff).Delete("/channels/{channelID}", a.disconnectYoutube)
				})
			})
		})
	})
	return r
}

type clientKey struct{}

func clientFrom(ctx context.Context) db.Client {
	c, _ := ctx.Value(clientKey{}).(db.Client)
	return c
}

// clientCtx loads the client scoped to the caller's workspace. Client-role users
// may only reach their own client. Both failures return 404 so IDs can't be probed.
func (a *API) clientCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := auth.FromContext(r.Context())
		id, err := uuid.Parse(chi.URLParam(r, "clientID"))
		if err != nil || (p.Role == auth.RoleClient && p.ClientID != id) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		c, err := a.Q.GetClient(r.Context(), db.GetClientParams{ID: id, WorkspaceID: p.WorkspaceID})
		if err == pgx.ErrNoRows {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientKey{}, c)))
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}
