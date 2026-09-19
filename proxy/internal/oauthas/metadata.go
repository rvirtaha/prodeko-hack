package oauthas

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// The two discovery documents. They are contract with the MCP client, not
// documentation: a connector that is handed nothing but the resource URL finds
// the authorization server, the registration endpoint and the PKCE
// requirement here and nowhere else.

// ASMetadata is RFC 8414 authorization server metadata.
type ASMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
}

// ResourceMetadataDocument is RFC 9728 protected resource metadata.
type ResourceMetadataDocument struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
}

// Scope is the one scope this server issues. It names the capability rather
// than a resource, because there is only one resource.
const Scope = "mcp"

// ASMetadataDocument is what GET /.well-known/oauth-authorization-server
// answers with.
//
// S256 is the only code challenge method: plain would leave a code issued to a
// dynamically registered public client bound to nothing.
func (s *Server) ASMetadataDocument() ASMetadata {
	return ASMetadata{
		Issuer:                 s.cfg.PublicURL,
		AuthorizationEndpoint:  s.cfg.PublicURL + AuthorizePath,
		TokenEndpoint:          s.cfg.PublicURL + TokenPath,
		RegistrationEndpoint:   s.cfg.PublicURL + RegisterPath,
		ScopesSupported:        []string{Scope},
		ResponseTypesSupported: []string{"code"},
		// No refresh_token grant in the MVP: a connector signs in again when
		// the token expires, and rotation arrives with the revocation endpoint.
		GrantTypesSupported: []string{"authorization_code"},
		// A client registered through open registration gets no secret, so
		// none is presented at the token endpoint.
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	}
}

// ResourceMetadataDoc is what GET /.well-known/oauth-protected-resource
// answers with, and what the MCP endpoint's 401 points at.
func (s *Server) ResourceMetadataDoc() ResourceMetadataDocument {
	return ResourceMetadataDocument{
		Resource:               s.cfg.PublicURL + s.cfg.ResourcePath,
		AuthorizationServers:   []string{s.cfg.PublicURL},
		BearerMethodsSupported: []string{"header"},
		ScopesSupported:        []string{Scope},
	}
}

func (s *Server) Metadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ASMetadataDocument())
}

func (s *Server) ResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ResourceMetadataDoc())
}

// RegistrationRequest is the part of RFC 7591 dynamic client registration this
// server reads. Registration is permissive on purpose: which redirect URIs a
// connector arrives with is the design's biggest unknown, and refusing an
// unexpected one would be refusing the whole feature.
//
// Permissive is not unbounded. The redirect URIs are recorded and a code is
// only ever returned to one of them, so a registration cannot be used to
// redirect somebody else's authorization elsewhere.
type RegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// RegistrationResponse is what a successful registration returns.
type RegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
}

// RegisterClient handles POST /register.
//
// Registration is open: no initial access token, no client secret, no approval.
// What that grants is the ability to start an authorization, which still ends
// at a Keycloak login and the role conjunction. What it must not grant is a
// redirect URI that could carry somebody's code away, which is why the URIs are
// checked here and compared exactly at /authorize.
func (s *Server) RegisterClient(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)

	var req RegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata",
			"the registration body is not JSON this server can read: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.ClientName)
	if len(name) > MaxClientNameLen {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata",
			fmt.Sprintf("client_name is longer than the %d characters this server records", MaxClientNameLen))
		return
	}
	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri",
			"redirect_uris must name at least one URI; a code has nowhere to go otherwise")
		return
	}
	if len(req.RedirectURIs) > MaxRedirectURIs {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri",
			fmt.Sprintf("redirect_uris names more than the %d URIs this server records", MaxRedirectURIs))
		return
	}
	uris := make([]string, 0, len(req.RedirectURIs))
	seen := make(map[string]bool, len(req.RedirectURIs))
	for _, uri := range req.RedirectURIs {
		if err := checkRedirectURI(uri); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri",
				fmt.Sprintf("redirect_uri %q is not usable: %v", uri, err))
			return
		}
		if !seen[uri] {
			seen[uri] = true
			uris = append(uris, uri)
		}
	}

	id, err := randomToken()
	if err != nil {
		s.log.Error("could not generate a client id", "err", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "registration is unavailable")
		return
	}
	now := s.now()
	if err := s.addClient(&client{ID: id, Name: name, RedirectURIs: uris, CreatedAt: now}); err != nil {
		s.log.Error("could not record a registration", "err", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "registration is unavailable")
		return
	}

	// RFC 7591 section 3.2.1: the server returns the metadata it granted, which
	// need not be the metadata that was asked for. Anything beyond the one
	// grant type and the one response type this server has is dropped rather
	// than refused, so a connector asking for refresh tokens still registers
	// and simply does not get them.
	s.log.Info("client registered",
		"client", id, "name", name, "redirect_uris", uris,
		"requested_grant_types", req.GrantTypes,
		"requested_auth_method", req.TokenEndpointAuthMethod)

	writeJSON(w, http.StatusCreated, RegistrationResponse{
		ClientID:                id,
		ClientIDIssuedAt:        now.Unix(),
		ClientName:              name,
		RedirectURIs:            uris,
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
	})
}

// checkRedirectURI is the one place registration is not permissive. The rules
// are RFC 8252's: an absolute URI, no fragment, and plain http only on the
// loopback interface, where there is no network to read the code off.
func checkRedirectURI(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return errors.New("empty")
	}
	if len(raw) > MaxRedirectURILen {
		return fmt.Errorf("longer than the %d characters this server records", MaxRedirectURILen)
	}
	for i := 0; i < len(raw); i++ {
		if c := raw[i]; c < 0x20 || c == 0x7f {
			return errors.New("contains a control character")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if !u.IsAbs() {
		return errors.New("must be absolute, with a scheme")
	}
	if u.Fragment != "" || u.RawFragment != "" || strings.Contains(raw, "#") {
		return errors.New("must not carry a fragment")
	}
	switch scheme := strings.ToLower(u.Scheme); scheme {
	case "https":
	case "http":
		// A code delivered over plain http to anything but the machine that
		// asked for it is a code read off the wire.
		if !isLoopback(u.Hostname()) {
			return errors.New("http is only allowed on the loopback interface")
		}
	case "javascript", "data", "vbscript", "file", "blob":
		return fmt.Errorf("the %s scheme is not a redirect target", scheme)
	default:
		// A private-use scheme, which is how a native client is called back.
		if u.Opaque == "" && u.Host == "" && u.Path == "" {
			return errors.New("names no target")
		}
	}
	return nil
}

func isLoopback(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// errorResponse is the OAuth error body, which clients parse.
type errorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeOAuthError answers with the RFC 6749 error body. The description is for
// a developer reading a connector's log, so it names what was wrong with the
// request and never anything about the account.
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, errorResponse{Error: code, ErrorDescription: description})
}
