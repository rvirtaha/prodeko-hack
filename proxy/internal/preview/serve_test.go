package preview

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// The pictures are taken over HTTP because hugo's output assumes a server: the
// stylesheet is an absolute path from the site root, and a browser reading
// file:// looks for it at the root of the filesystem.
func TestServeAnswersPagesAndTheirAssets(t *testing.T) {
	s := testSite(t)
	srv, err := Serve(s.Output)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer srv.Close()

	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Fatalf("URL = %q, want a loopback address: nothing off this host may reach an unreviewed change", srv.URL)
	}

	cases := map[string]string{
		"/fi/tapahtumat/": "Syksyn tapahtumat.",
		"/fi/":            "Etusivu",
		"/":               "redirect",
		"/css/main.css":   "rebeccapurple",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			body, status := get(t, srv.URL+path)
			if status != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", path, status)
			}
			if !strings.Contains(body, want) {
				t.Fatalf("GET %s does not carry %q:\n%s", path, want, body)
			}
		})
	}

	if _, status := get(t, srv.URL+"/fi/ei-ole/"); status != http.StatusNotFound {
		t.Errorf("a page that is not there answers %d, want 404", status)
	}
}

// A viewport tall enough for a long page would stretch anything the site sizes
// in viewport units, so every page is served with that neutralised. The
// stylesheet itself is served untouched: it is what the picture is of.
func TestServeUnstretchesPagesOnly(t *testing.T) {
	s := testSite(t)
	srv, err := Serve(s.Output)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer srv.Close()

	page, _ := get(t, srv.URL+"/fi/tapahtumat/")
	if !strings.Contains(page, "min-height:0!important") {
		t.Errorf("the page was served without the stylesheet that unstretches it:\n%s", page)
	}
	if !strings.Contains(page, "Syksyn tapahtumat.") {
		t.Errorf("the page lost its own content:\n%s", page)
	}
	if i, j := strings.Index(page, "min-height:0"), strings.Index(page, "</body>"); i < 0 || i > j {
		t.Errorf("the stylesheet is not inside the body, where it overrides the page's own")
	}

	css, _ := get(t, srv.URL+"/css/main.css")
	if strings.Contains(css, "min-height:0!important") {
		t.Errorf("the stylesheet was rewritten:\n%s", css)
	}
}

// There is nothing to serve before a build, and that is the same answer render
// gives: build first.
func TestServeRefusesAnUnbuiltChange(t *testing.T) {
	if _, err := Serve(filepath.Join(t.TempDir(), "public")); !errors.Is(err, ErrNotBuilt) {
		t.Fatalf("Serve of an unbuilt change = %v, want ErrNotBuilt", err)
	}
}

func TestInjectPutsTheFragmentBeforeTheClosingBody(t *testing.T) {
	for _, tc := range []struct{ page, want string }{
		{"<html><body>hei</body></html>", "<html><body>hei<style></style></body></html>"},
		{"<HTML><BODY>hei</BODY></HTML>", "<HTML><BODY>hei<style></style></BODY></HTML>"},
		// A document with no body to find is still a document a browser parses.
		{"<p>hei</p>", "<p>hei</p><style></style>"},
	} {
		if got := string(inject([]byte(tc.page), "<style></style>")); got != tc.want {
			t.Errorf("inject(%q) = %q, want %q", tc.page, got, tc.want)
		}
	}
}

func get(t *testing.T, url string) (string, int) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", url, err)
	}
	return string(body), res.StatusCode
}
