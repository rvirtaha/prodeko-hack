package preview

import (
	"errors"
	"strings"
	"testing"
)

// A whole page comes back as hugo wrote it. What a template produced is the
// thing being examined, so nothing here reformats it.
func TestHTMLReturnsThePageVerbatim(t *testing.T) {
	s := testSite(t)
	page, err := s.Locate("/fi/tapahtumat/")
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	got, err := page.HTML("")
	if err != nil {
		t.Fatalf("HTML: %v", err)
	}
	if got != tapahtumatHTML {
		t.Fatalf("HTML = %q, want the file byte for byte", got)
	}
}

func TestHTMLReturnsTheSelectorSubtree(t *testing.T) {
	s := testSite(t)
	page, err := s.Locate("/fi/tapahtumat/")
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	cases := map[string]string{
		"h1.events-header":         `<h1 class="events-header">Tapahtumat</h1>`,
		".site-header":             `<header class="site-header"><h1 class="events-header">Tapahtumat</h1></header>`,
		"header.site-header h1":    `<h1 class="events-header">Tapahtumat</h1>`,
		"main p":                   `<p>Syksyn tapahtumat.</p>`,
		"  h1.events-header  ":     `<h1 class="events-header">Tapahtumat</h1>`,
		"link[rel=stylesheet]":     `<link rel="stylesheet" href="/css/main.css"/>`,
		"h1.events-header, main p": "<h1 class=\"events-header\">Tapahtumat</h1>\n<p>Syksyn tapahtumat.</p>",
	}
	for selector, want := range cases {
		t.Run(selector, func(t *testing.T) {
			got, err := page.HTML(selector)
			if err != nil {
				t.Fatalf("HTML(%q): %v", selector, err)
			}
			if got != want {
				t.Fatalf("HTML(%q) = %q, want %q", selector, got, want)
			}
		})
	}
}

// A selector that matches nothing is not an empty page: it usually means the
// element is named something else, and the model has to be told so.
func TestHTMLSaysWhenASelectorMatchesNothing(t *testing.T) {
	s := testSite(t)
	page, err := s.Locate("/fi/tapahtumat/")
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if _, err := page.HTML(".ei-ole"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("HTML of a selector that matches nothing = %v, want ErrNoMatch", err)
	}
	_, err = page.HTML("h1[")
	if err == nil || !strings.Contains(err.Error(), "CSS selector") {
		t.Fatalf("HTML of an unusable selector = %v, want it named as one", err)
	}
}
