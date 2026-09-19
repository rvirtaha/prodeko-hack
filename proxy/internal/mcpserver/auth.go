package mcpserver

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
)

type contextKey struct{}

// NewContext carries id down to the tools.
func NewContext(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// IdentityFrom returns the identity [RequireBearer] proved for the request.
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(Identity)
	return id, ok
}

// RequireBearer authenticates every request with auth and refuses the ones it
// cannot. The 401 carries RFC 9728's resource_metadata pointer, which is how
// an MCP client that was handed nothing but a URL finds the authorization
// server and registers itself.
//
// The reason a token was refused goes to the log and never to the client.
func RequireBearer(auth Authenticator, resourceMetadataURL string, log *slog.Logger, next http.Handler) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := BearerFromHeader(r.Header.Get("Authorization"))
		if token == "" {
			unauthorized(w, resourceMetadataURL, "invalid_token", "a bearer token is required")
			return
		}
		id, err := auth(token)
		if err != nil {
			log.Warn("bearer token refused", "err", err, "path", r.URL.EscapedPath())
			unauthorized(w, resourceMetadataURL, "invalid_token", "the bearer token is not valid")
			return
		}
		next.ServeHTTP(w, r.WithContext(NewContext(r.Context(), id)))
	})
}

// unauthorized writes the 401 and the challenge that makes discovery work.
func unauthorized(w http.ResponseWriter, resourceMetadataURL, code, description string) {
	challenge := `Bearer error="` + code + `", error_description="` + description + `"`
	if resourceMetadataURL != "" {
		challenge += `, resource_metadata="` + resourceMetadataURL + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, description, http.StatusUnauthorized)
}

// BearerFromHeader returns the token out of an Authorization header, or "".
// The scheme is compared case-insensitively, as RFC 7235 requires.
func BearerFromHeader(authorization string) string {
	const scheme = "bearer "
	s := strings.TrimSpace(authorization)
	if len(s) < len(scheme) || !strings.EqualFold(s[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(s[len(scheme):])
}

// StaticBearer authenticates one fixed token as one fixed identity. It backs
// MCP_DEV_BEARER, which exists so the local demo and the tests can exercise
// the tools without a Keycloak round trip. An empty token disables it, and it
// must never be configured in production.
func StaticBearer(token string, id Identity) Authenticator {
	return func(bearer string) (Identity, error) {
		if token == "" || subtle.ConstantTimeCompare([]byte(bearer), []byte(token)) != 1 {
			return Identity{}, ErrUnauthorized
		}
		return id, nil
	}
}

// FirstOf tries each authenticator in order and returns the first identity one
// of them accepts.
func FirstOf(auths ...Authenticator) Authenticator {
	return func(bearer string) (Identity, error) {
		for _, a := range auths {
			if a == nil {
				continue
			}
			if id, err := a(bearer); err == nil {
				return id, nil
			}
		}
		return Identity{}, ErrUnauthorized
	}
}
