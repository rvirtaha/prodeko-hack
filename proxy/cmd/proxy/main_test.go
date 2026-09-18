package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prodeko/prodeko-hack/proxy/internal/config"
	"github.com/prodeko/prodeko-hack/proxy/internal/forward"
	"github.com/prodeko/prodeko-hack/proxy/internal/session"
)

const (
	cmsOrigin  = "https://prodeko.org"
	editorRole = "cms-editor"
)

// stubSignin stands in for auth.Handler, whose constructor does OIDC discovery.
// The wiring only needs it to claim two routes.
type stubSignin struct{ registered []string }

func (s *stubSignin) Register(mux *http.ServeMux) {
	for _, p := range []string{"GET /auth", "GET /callback"} {
		s.registered = append(s.registered, p)
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "sign-in")
		})
	}
}

func testConfig() *config.Config {
	return &config.Config{
		PublicURL:  "https://cms.prodeko.org",
		CMSOrigins: []string{cmsOrigin},
		Keycloak:   config.Keycloak{EditorRole: editorRole},
	}
}

// testRoutes builds the real routing table over a real session store and a
// real forward handler, with only sign-in stubbed out.
func testRoutes(t *testing.T) (http.Handler, *session.Store, *stubSignin) {
	t.Helper()
	cfg := testConfig()

	store, err := session.NewStore(session.Options{Secret: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	publicBase, _ := url.Parse(cfg.PublicURL)
	github, err := forward.New(forward.Config{
		Owner:      "prodeko",
		Repo:       "prodeko-hack",
		Branch:     "main",
		Token:      "ghp_not-a-real-token",
		EditorRole: editorRole,
		Committer:  forward.Author{Name: config.DefaultCommitterName, Email: config.DefaultCommitterEmail},
		PublicBase: publicBase,
		Prefix:     githubPrefix,
		Logger:     slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("forward.New: %v", err)
	}

	signin := &stubSignin{}
	return routes(cfg, slog.New(slog.DiscardHandler), signin, store, github), store, signin
}

func tokenFor(t *testing.T, store *session.Store, roles ...string) string {
	t.Helper()
	token, _, err := store.Issue(session.Identity{
		Subject:  "abc-123",
		Name:     "Aino Editor",
		Email:    "aino@prodeko.org",
		Username: "aino",
		Roles:    roles,
	})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return token
}

// A preflight carries no Authorization header by definition. If the session
// middleware sees it first the browser gets a 401 it cannot read, and every
// GitHub call from the CMS fails before it is made.
func TestPreflightIsNotAuthenticated(t *testing.T) {
	h, _, _ := testRoutes(t)

	req := httptest.NewRequest(http.MethodOptions, githubPrefix+"/repos/x/y/git/commits", nil)
	req.Header.Set("Origin", cmsOrigin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != cmsOrigin {
		t.Errorf("Allow-Origin = %q, want %q", got, cmsOrigin)
	}
	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	for _, want := range forward.CORSRequestHeaders {
		if !strings.Contains(strings.ToLower(allowed), strings.ToLower(want)) {
			t.Errorf("Allow-Headers %q does not permit %q", allowed, want)
		}
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), http.MethodPost) {
		t.Errorf("Allow-Methods = %q, want POST", rec.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestGitHubRequiresASession(t *testing.T) {
	h, _, _ := testRoutes(t)

	req := httptest.NewRequest(http.MethodGet, githubPrefix+"/user", nil)
	req.Header.Set("Origin", cmsOrigin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	// Decap parses every error body as JSON and reads .message; a body without
	// one throws inside its client and triggers five retries.
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Message == "" {
		t.Fatalf("401 body is not {\"message\":...}: %q (%v)", rec.Body.String(), err)
	}
}

// Rule 1: a valid Prodeko session without the editor role must not get through.
func TestGitHubRequiresTheEditorRole(t *testing.T) {
	h, store, _ := testRoutes(t)

	req := httptest.NewRequest(http.MethodGet, githubPrefix+"/user", nil)
	req.Header.Set("Authorization", "token "+tokenFor(t, store, "member"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), editorRole) {
		t.Errorf("403 body does not name the missing role: %q", rec.Body.String())
	}
}

// The end-to-end path through CORS, the session middleware, the role check and
// forward. GET /user is answered from the session and never reaches GitHub,
// which is what makes it safe to assert against a fake token.
func TestSignedInEditorReachesForward(t *testing.T) {
	h, store, _ := testRoutes(t)

	req := httptest.NewRequest(http.MethodGet, githubPrefix+"/user", nil)
	req.Header.Set("Origin", cmsOrigin)
	req.Header.Set("Authorization", "token "+tokenFor(t, store, editorRole))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var user struct {
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &user); err != nil {
		t.Fatalf("body is not JSON: %q", rec.Body.String())
	}
	if user.Login != "aino" || user.Name != "Aino Editor" || user.Email != "aino@prodeko.org" {
		t.Errorf("GET /user = %+v, want the Keycloak identity", user)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != cmsOrigin {
		t.Errorf("Allow-Origin = %q, want %q", got, cmsOrigin)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, "Link") {
		t.Errorf("Expose-Headers = %q, want Link among them", got)
	}
}

func TestUnlistedOriginGetsNoCORSHeaders(t *testing.T) {
	h, store, _ := testRoutes(t)

	req := httptest.NewRequest(http.MethodGet, githubPrefix+"/user", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Authorization", "token "+tokenFor(t, store, editorRole))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q for an unlisted origin, want none", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Errorf("Vary = %q, want Origin; a shared cache would cross the origins", rec.Header().Get("Vary"))
	}
}

func TestSigninRoutesAreMounted(t *testing.T) {
	h, _, signin := testRoutes(t)

	if len(signin.registered) == 0 {
		t.Fatal("Register was never called")
	}
	for _, path := range []string{"/auth", "/callback"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}

func TestHealthz(t *testing.T) {
	h, _, _ := testRoutes(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("GET /healthz = %d %q", rec.Code, rec.Body.String())
	}
}

func TestShutdownIsGraceful(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	h, _, _ := testRoutes(t)
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: readHeaderTimeout}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, slog.New(slog.DiscardHandler)) }()

	deadline := time.Now().Add(5 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/healthz")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never came up: %v", err)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the context was cancelled")
	}
}
