package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type contextKey struct{}

// NewContext attaches id. For tests and for handlers authenticated elsewhere.
func NewContext(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the Identity attached by [Middleware].
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(Identity)
	return id, ok
}

// Verifier is the narrow dependency [Middleware] needs; [*Store] implements it.
type Verifier interface {
	Verify(token string) (Identity, error)
}

// TokenFromHeader accepts both "token <t>", which is what Decap's GitHub
// backend sends, and "Bearer <t>". Returns "" if neither matches.
func TokenFromHeader(authorization string) string {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(authorization), " ")
	if !ok {
		return ""
	}
	switch {
	case strings.EqualFold(scheme, "token"), strings.EqualFold(scheme, "bearer"):
	default:
		return ""
	}
	// Decap sends exactly one space, but be forgiving about extra padding.
	return strings.TrimSpace(rest)
}

// Middleware authenticates the Authorization header and puts the Identity in
// the request context. Rejections are GitHub-shaped JSON ({"message": ...}) so
// Decap surfaces something readable instead of a blank error toast.
func Middleware(v Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := TokenFromHeader(r.Header.Get("Authorization"))
			if token == "" {
				writeError(w, http.StatusUnauthorized,
					"Not signed in. Open /admin and sign in with your Prodeko account.")
				return
			}
			id, err := v.Verify(token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, messageFor(err))
				return
			}
			next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), id)))
		})
	}
}

// RequireRole rejects requests whose Identity lacks role. Defence in depth:
// the role was already required at sign-in.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := FromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized,
					"Not signed in. Open /admin and sign in with your Prodeko account.")
				return
			}
			if !id.HasRole(role) {
				writeError(w, http.StatusForbidden, fmt.Sprintf(
					"Your Prodeko account does not have the %q role required to edit the website.", role))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func messageFor(err error) string {
	switch {
	case errors.Is(err, ErrExpired):
		return "Your Prodeko CMS session has expired. Copy any unsaved text, reload /admin, and sign in again."
	case errors.Is(err, ErrRevoked):
		return "Your Prodeko CMS session has been revoked. Reload /admin and sign in again."
	case errors.Is(err, ErrNoToken):
		return "Not signed in. Open /admin and sign in with your Prodeko account."
	default:
		return "Bad credentials: this session token is not valid. Reload /admin and sign in again."
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
