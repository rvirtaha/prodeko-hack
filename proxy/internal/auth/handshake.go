package auth

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"text/template"
	"unicode"
)

// Decap's popup handshake, verified against
// decap-cms/packages/decap-cms-lib-auth/src/netlify-auth.js (main):
//
//  1. The popup posts the literal string "authorizing:<provider>" to its
//     opener. The opener compares with ===, so no whitespace or trailing
//     newline is allowed.
//  2. The opener registers its success/error listener and only then echoes
//     that same string back, with targetOrigin set to the popup's origin.
//  3. Only now may the popup post
//     "authorization:<provider>:success:" + JSON.stringify({token, provider})
//     or
//     "authorization:<provider>:error:" + JSON.stringify({message})
//     back, with targetOrigin set to the opener's origin.
//
// Posting step 3 without waiting for step 2 is the single most common way this
// integration fails: the opener has not registered the listener yet, so the
// message is dropped and the popup closes with nothing happening. CivicDataLab's
// proxy has exactly this bug on its error path, which is why its "you are not an
// editor" message never reaches the CMS. Both paths here do the full handshake.
//
// The error payload must be an object with a "message" field: Decap does
// JSON.parse on it and wraps it in NetlifyError, whose toString() returns
// this.err && this.err.message.
//
// Decap matches the success payload with /^authorization:<p>:success:(.+)$/ —
// no s and no m flag — so the payload must contain no \n, \r, U+2028 or U+2029.
// The success payload is only an opaque base64url token plus the provider, so
// it is safe by construction; error messages are passed through sanitizeMessage.

// handshakeTemplate is the popup page. Every value substituted into the script
// is either a CSP nonce (base64) or a pre-escaped JSON literal, so text/template
// is correct here and html/template would double-escape the JSON.
var handshakeTemplate = template.Must(template.New("handshake").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Prodeko CMS sign-in</title>
<style nonce="{{.Nonce}}">
body { font: 16px/1.5 system-ui, sans-serif; margin: 3rem auto; max-width: 34rem; padding: 0 1rem; }
</style>
</head>
<body>
<p id="status">Finishing sign-in&hellip;</p>
<noscript><p>JavaScript is required to finish signing in to the Prodeko CMS.</p></noscript>
<script nonce="{{.Nonce}}">
(function () {
  var provider = {{.Provider}};
  var status   = {{.Status}};
  var payload  = {{.Payload}};
  var origins  = {{.Origins}};
  var el = document.getElementById('status');
  var spoke = false;
  function say(t) { el.textContent = t; spoke = true; }

  if (!window.opener || window.opener.closed) {
    say('This page has to be opened by the CMS sign-in window. Start again from /admin.');
    return;
  }

  function allowed(o) {
    if (origins.length === 0) { return true; }
    for (var i = 0; i < origins.length; i++) { if (origins[i] === o) { return true; } }
    return false;
  }

  var done = false;
  var timer = setTimeout(function () {
    if (done || spoke) { return; }
    say('The CMS did not answer the sign-in handshake. Check that base_url in the Decap '
      + 'config is exactly ' + window.location.origin + ' with no path and no trailing slash.');
  }, 10000);

  function onMessage(e) {
    if (done || e.data !== 'authorizing:' + provider) { return; }
    if (e.source !== window.opener) {
      say('Refusing to finish sign-in: the handshake came from a window that did not open this one.');
      return;
    }
    if (!allowed(e.origin)) {
      say('Refusing to finish sign-in for the origin ' + e.origin + '.');
      return;
    }
    done = true;
    clearTimeout(timer);
    window.removeEventListener('message', onMessage, false);
    e.source.postMessage(
      'authorization:' + provider + ':' + status + ':' + JSON.stringify(payload),
      e.origin
    );
    say(status === 'success'
      ? 'Signed in. This window can be closed.'
      : payload.message);
  }

  // The listener must exist before the handshake is sent.
  window.addEventListener('message', onMessage, false);
  window.opener.postMessage('authorizing:' + provider, '*');
})();
</script>
</body>
</html>
`))

type handshakePage struct {
	Nonce    string
	Provider string
	Status   string
	Payload  string
	Origins  string
}

// writeSuccess completes the handshake and hands the session token to the CMS.
func (h *Handler) writeSuccess(w http.ResponseWriter, provider, token string) {
	h.writeHandshake(w, http.StatusOK, provider, "success",
		map[string]string{"token": token, "provider": provider})
}

// writeFailure completes the same handshake and delivers a readable error.
// The HTTP status stays 200: this is a page for a browser popup, and a non-2xx
// body would still have to run the same script.
func (h *Handler) writeFailure(w http.ResponseWriter, provider, message string) {
	h.writeHandshake(w, http.StatusOK, provider, "error",
		map[string]string{"message": sanitizeMessage(message)})
}

func (h *Handler) writeHandshake(w http.ResponseWriter, status int, provider, outcome string, payload any) {
	providerJSON, err := jsonForScript(safeProvider(provider))
	if err != nil {
		http.Error(w, "sign-in failed", http.StatusInternalServerError)
		return
	}
	outcomeJSON, err := jsonForScript(outcome)
	if err != nil {
		http.Error(w, "sign-in failed", http.StatusInternalServerError)
		return
	}
	payloadJSON, err := jsonForScript(payload)
	if err != nil {
		http.Error(w, "sign-in failed", http.StatusInternalServerError)
		return
	}
	// A nil slice would encode as null, and the script does origins.length.
	origins := h.cfg.CMSOrigins
	if origins == nil {
		origins = []string{}
	}
	originsJSON, err := jsonForScript(origins)
	if err != nil {
		http.Error(w, "sign-in failed", http.StatusInternalServerError)
		return
	}
	nonce, err := scriptNonce()
	if err != nil {
		http.Error(w, "sign-in failed", http.StatusInternalServerError)
		return
	}

	hdr := w.Header()
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Referrer-Policy", "no-referrer")
	// window.opener must survive: Cross-Origin-Opener-Policy: same-origin here
	// or on the /admin page severs it and the handshake becomes impossible.
	hdr.Set("Cross-Origin-Opener-Policy", "unsafe-none")
	// A bare script-src 'self' would block the inline handshake and produce the
	// identical silent failure, so the script carries a per-response nonce.
	hdr.Set("Content-Security-Policy",
		"default-src 'none'; script-src 'nonce-"+nonce+"'; style-src 'nonce-"+nonce+"'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)

	_ = handshakeTemplate.Execute(w, handshakePage{
		Nonce:    nonce,
		Provider: providerJSON,
		Status:   outcomeJSON,
		Payload:  payloadJSON,
		Origins:  originsJSON,
	})
}

// jsonForScript encodes v for embedding in a <script> block. encoding/json
// already escapes <, > and & as \u00xx; U+2028 and U+2029 it does not, and they
// terminate a JavaScript line, so they are escaped here.
func jsonForScript(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	s := string(b)
	s = strings.ReplaceAll(s, "\u2028", `\u2028`)
	s = strings.ReplaceAll(s, "\u2029", `\u2029`)
	return s, nil
}

// safeProvider echoes back the provider Decap asked for, restricted to a
// character set that cannot escape a JSON string or a URL.
func safeProvider(p string) string {
	if p == "" || len(p) > 32 {
		return "github"
	}
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return "github"
		}
	}
	return p
}

// sanitizeMessage flattens a message into something that survives Decap's
// success/error regex (no newlines, no U+2028/U+2029) and cannot grow without
// bound.
func sanitizeMessage(m string) string {
	const maxLen = 300
	var b strings.Builder
	lastSpace := false
	for _, r := range m {
		switch {
		case r == '\u2028' || r == '\u2029' || unicode.IsSpace(r) || unicode.IsControl(r):
			if !lastSpace && b.Len() > 0 {
				b.WriteByte(' ')
				lastSpace = true
			}
		case r == unicode.ReplacementChar:
			// drop
		default:
			b.WriteRune(r)
			lastSpace = false
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "Sign-in failed."
	}
	if len(out) > maxLen {
		out = strings.ToValidUTF8(out[:maxLen], "") + "…"
	}
	return out
}

func scriptNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("auth: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
