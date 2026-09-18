package auth

import (
	"regexp"
	"strings"
	"testing"
)

// PUBLIC_URL is compared by Decap with `e.origin === base_url`, where e.origin
// is browser-computed and therefore never has a path. A PUBLIC_URL with a path
// can never match and the popup hangs with no diagnostics, so it is rejected at
// startup rather than at 2am.
func TestParseOrigin(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"plain https", "https://cms.prodeko.org", "https://cms.prodeko.org", true},
		{"trailing slash", "https://cms.prodeko.org/", "https://cms.prodeko.org", true},
		{"several trailing slashes", "https://cms.prodeko.org///", "https://cms.prodeko.org", true},
		{"with port", "http://localhost:8080", "http://localhost:8080", true},
		{"surrounding whitespace", "  https://cms.prodeko.org  ", "https://cms.prodeko.org", true},
		{"uppercase host", "HTTPS://CMS.Prodeko.ORG", "https://cms.prodeko.org", true},

		{"empty", "", "", false},
		{"path", "https://prodeko.org/cms", "", false},
		{"deep path", "https://prodeko.org/cms/auth", "", false},
		{"query", "https://cms.prodeko.org?a=1", "", false},
		{"fragment", "https://cms.prodeko.org#x", "", false},
		{"credentials", "https://user:pw@cms.prodeko.org", "", false},
		{"no scheme", "cms.prodeko.org", "", false},
		{"scheme relative", "//cms.prodeko.org", "", false},
		{"wrong scheme", "ftp://cms.prodeko.org", "", false},
		{"javascript scheme", "javascript:alert(1)", "", false},
		{"no host", "https://", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOrigin(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("parseOrigin(%q) = error %v, want %q", tc.in, err, tc.want)
			}
			if !tc.ok {
				if err == nil {
					t.Fatalf("parseOrigin(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("parseOrigin(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The provider string is interpolated into the postMessage wire format, so it
// must never carry anything that could break out of a JSON string.
func TestSafeProvider(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"github", "github"},
		{"gitlab", "gitlab"},
		{"bit-bucket_2", "bit-bucket_2"},
		{"", "github"},
		{`github"`, "github"},
		{"github:success:{}", "github"},
		{"git hub", "github"},
		{"github\n", "github"},
		{"</script>", "github"},
		{strings.Repeat("a", 33), "github"},
		{strings.Repeat("a", 32), strings.Repeat("a", 32)},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := safeProvider(tc.in); got != tc.want {
				t.Errorf("safeProvider(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Decap matches the payload with /^authorization:github:(success|error):(.+)$/
// built without the s or m flag, so a newline, U+2028 or U+2029 anywhere in the
// message silently breaks the match and the popup dies with no explanation.
func TestSanitizeMessageSurvivesDecapRegex(t *testing.T) {
	decap := regexp.MustCompile(`^authorization:github:error:(.+)$`)

	tests := []struct {
		name string
		in   string
	}{
		{"newline", "line one\nline two"},
		{"carriage return", "line one\rline two"},
		{"crlf", "line one\r\nline two"},
		{"line separator", "before\u2028after"},
		{"paragraph separator", "before\u2029after"},
		{"tab", "a\tb"},
		{"null byte", "a\x00b"},
		{"script close", "oops </script><script>alert(1)</script>"},
		{"quotes", `he said "no" and 'no'`},
		{"backslash", `C:\path\to`},
		{"finnish", "Käyttäjällä ei ole oikeuksia"},
		{"very long", strings.Repeat("pitkä viesti ", 200)},
		{"only whitespace", "  \n\t "},
		{"empty", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := sanitizeMessage(tc.in)
			if msg == "" {
				t.Fatal("sanitizeMessage returned empty; Decap would show an empty toast")
			}
			for _, bad := range []string{"\n", "\r", "\u2028", "\u2029", "\x00"} {
				if strings.Contains(msg, bad) {
					t.Fatalf("message still contains %q: %q", bad, msg)
				}
			}
			payload, err := jsonForScript(map[string]string{"message": msg})
			if err != nil {
				t.Fatal(err)
			}
			wire := "authorization:github:error:" + payload
			m := decap.FindStringSubmatch(wire)
			if m == nil {
				t.Fatalf("Decap's regex would not match %q", wire)
			}
			if m[1] != payload {
				t.Fatalf("regex captured %q, want %q", m[1], payload)
			}
		})
	}
}

// The payload is embedded inside a <script> block, so anything that could close
// the tag or start a line has to come out escaped.
func TestJSONForScriptEscaping(t *testing.T) {
	tests := []struct {
		name     string
		in       any
		mustNot  []string
		mustHave []string
	}{
		{
			name:    "script tag",
			in:      map[string]string{"message": "</script><script>alert(1)</script>"},
			mustNot: []string{"</script>", "<", ">"},
		},
		{
			name:    "ampersand",
			in:      map[string]string{"message": "a & b"},
			mustNot: []string{"&"},
		},
		{
			name:     "line separators",
			in:       map[string]string{"message": "a\u2028b\u2029c"},
			mustNot:  []string{"\u2028", "\u2029"},
			mustHave: []string{`\u2028`, `\u2029`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := jsonForScript(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			for _, bad := range tc.mustNot {
				if strings.Contains(got, bad) {
					t.Errorf("output %q still contains %q", got, bad)
				}
			}
			for _, want := range tc.mustHave {
				if !strings.Contains(got, want) {
					t.Errorf("output %q is missing %q", got, want)
				}
			}
		})
	}
}

func TestJSONForScriptEmptyOriginsIsAnArray(t *testing.T) {
	got, err := jsonForScript([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "[]" {
		t.Fatalf("empty CMSOrigins encodes as %q; the script does origins.length on it", got)
	}
}
