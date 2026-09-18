package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// One 2048-bit key for the whole test binary; generating one per test is slow.
var testKey = sync.OnceValue(func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
})

// fakeIdP is just enough Keycloak to drive the callback: discovery, a JWKS, and
// a token endpoint that returns whatever claims the test asks for.
type fakeIdP struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu sync.Mutex
	// tokenResponse builds the token endpoint body from the posted form.
	tokenResponse func(form url.Values) (status int, body map[string]any)
	lastTokenForm url.Values
	// discoveredIssuer is what .well-known reports, which differs from the URL
	// it was fetched from in a split-horizon setup.
	discoveredIssuer string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	f := &fakeIdP{t: t, key: testKey()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                f.reportedIssuer(),
			"authorization_endpoint":                f.issuer() + "/protocol/openid-connect/auth",
			"token_endpoint":                        f.issuer() + "/protocol/openid-connect/token",
			"jwks_uri":                              f.issuer() + "/protocol/openid-connect/certs",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
		})
	})
	mux.HandleFunc("GET /protocol/openid-connect/certs", func(w http.ResponseWriter, r *http.Request) {
		pub := f.key.Public().(*rsa.PublicKey)
		writeJSON(w, map[string]any{"keys": []any{map[string]any{
			"kty": "RSA",
			"alg": "RS256",
			"use": "sig",
			"kid": "test-key",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("POST /protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.lastTokenForm = r.PostForm
		build := f.tokenResponse
		f.mu.Unlock()
		if build == nil {
			http.Error(w, "no token response configured", http.StatusInternalServerError)
			return
		}
		status, body := build(r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) issuer() string { return f.srv.URL }

// reportedIssuer is the "iss" advertised by discovery and stamped on tokens.
func (f *fakeIdP) reportedIssuer() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.discoveredIssuer != "" {
		return f.discoveredIssuer
	}
	return f.srv.URL
}

func (f *fakeIdP) setDiscoveredIssuer(iss string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discoveredIssuer = iss
}

func (f *fakeIdP) setTokenResponse(fn func(form url.Values) (int, map[string]any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenResponse = fn
}

func (f *fakeIdP) tokenForm() url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastTokenForm
}

// sign produces an RS256 JWT with the given claims, filling in iss/iat/exp.
func (f *fakeIdP) sign(claims map[string]any) string {
	f.t.Helper()
	full := map[string]any{
		"iss": f.issuer(),
		"iat": time.Now().Add(-time.Minute).Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range claims {
		full[k] = v
	}
	header, err := json.Marshal(map[string]any{"alg": "RS256", "typ": "JWT", "kid": "test-key"})
	if err != nil {
		f.t.Fatal(err)
	}
	payload, err := json.Marshal(full)
	if err != nil {
		f.t.Fatal(err)
	}
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
