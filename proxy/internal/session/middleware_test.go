package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Decap's GitHub backend hardcodes tokenKeyword = 'token', so the middleware
// has to accept "token <t>" and not only "Bearer <t>".
func TestTokenFromHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"decap style", "token abc123", "abc123"},
		{"bearer style", "Bearer abc123", "abc123"},
		{"lowercase bearer", "bearer abc123", "abc123"},
		{"uppercase token", "TOKEN abc123", "abc123"},
		{"mixed case", "ToKeN abc123", "abc123"},
		{"extra spaces", "token   abc123  ", "abc123"},
		{"leading space", "  token abc123", "abc123"},
		{"empty", "", ""},
		{"scheme only", "token", ""},
		{"scheme only with space", "token ", ""},
		{"unknown scheme", "Basic abc123", ""},
		{"github pat pasted raw", "ghp_abc123", ""},
		{"no scheme", "abc123", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := TokenFromHeader(tc.header); got != tc.want {
				t.Errorf("TokenFromHeader(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	now := base
	s := testStore(t, func() time.Time { return now }, time.Hour)
	token, _, err := s.Issue(sampleIdentity())
	if err != nil {
		t.Fatal(err)
	}
	revoked, _, err := s.Issue(sampleIdentity())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(revoked); err != nil {
		t.Fatal(err)
	}

	var sawIdentity Identity
	var reached bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		sawIdentity, _ = FromContext(r.Context())
		w.WriteHeader(http.StatusTeapot)
	})
	handler := Middleware(s)(next)

	tests := []struct {
		name       string
		header     string
		at         time.Time
		wantStatus int
		wantPass   bool
	}{
		{"valid decap header", "token " + token, base, http.StatusTeapot, true},
		{"valid bearer header", "Bearer " + token, base, http.StatusTeapot, true},
		{"no header", "", base, http.StatusUnauthorized, false},
		{"wrong scheme", "Basic " + token, base, http.StatusUnauthorized, false},
		{"garbage token", "token nonsense", base, http.StatusUnauthorized, false},
		{"expired", "token " + token, base.Add(2 * time.Hour), http.StatusUnauthorized, false},
		{"revoked", "token " + revoked, base, http.StatusUnauthorized, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now = tc.at
			reached, sawIdentity = false, Identity{}

			req := httptest.NewRequest(http.MethodGet, "/github/user", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if reached != tc.wantPass {
				t.Fatalf("handler reached = %v, want %v", reached, tc.wantPass)
			}
			if tc.wantPass {
				if sawIdentity.Subject != sampleIdentity().Subject {
					t.Errorf("identity not in context: %+v", sawIdentity)
				}
				return
			}
			// Rejections must be GitHub-shaped so Decap's error toast says
			// something instead of nothing.
			if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", ct)
			}
			var body struct {
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
			}
			if body.Message == "" {
				t.Error("rejection carried no message")
			}
		})
	}
}

// An expired session must be distinguishable from a broken one, because the
// editor's recovery action differs and Decap shows the message verbatim.
func TestMiddlewareExpiredMessageIsDistinct(t *testing.T) {
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	now := base
	s := testStore(t, func() time.Time { return now }, time.Hour)
	token, _, err := s.Issue(sampleIdentity())
	if err != nil {
		t.Fatal(err)
	}
	now = base.Add(2 * time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/github/user", nil)
	req.Header.Set("Authorization", "token "+token)
	rec := httptest.NewRecorder()
	Middleware(s)(http.NotFoundHandler()).ServeHTTP(rec, req)

	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Message == messageFor(ErrInvalidToken) {
		t.Errorf("expired and invalid produce the same message: %q", body.Message)
	}
}

func TestRequireRole(t *testing.T) {
	const role = "cms-editor"
	handler := RequireRole(role)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	tests := []struct {
		name       string
		identity   *Identity
		wantStatus int
	}{
		{"has the role", &Identity{Subject: "a", Roles: []string{"membership", role}}, http.StatusTeapot},
		{"lacks the role", &Identity{Subject: "a", Roles: []string{"membership"}}, http.StatusForbidden},
		{"no roles at all", &Identity{Subject: "a"}, http.StatusForbidden},
		{"near miss", &Identity{Subject: "a", Roles: []string{"cms-editors"}}, http.StatusForbidden},
		{"case mismatch", &Identity{Subject: "a", Roles: []string{"CMS-Editor"}}, http.StatusForbidden},
		{"no identity in context", nil, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/github/repos/x/y/contents/a.md", nil)
			if tc.identity != nil {
				req = req.WithContext(NewContext(req.Context(), *tc.identity))
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestFromContextEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := FromContext(req.Context()); ok {
		t.Fatal("FromContext reported an identity on a bare context")
	}
}
