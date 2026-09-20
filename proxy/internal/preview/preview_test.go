package preview

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testSite lays down a content tree and the output a build of it would have
// produced. Both are written by hand: what is under test here is the mapping
// between the two, and hugo's correctness is hugo's business.
func testSite(t *testing.T) Site {
	t.Helper()
	root := t.TempDir()
	s := Site{Worktree: filepath.Join(root, "wt"), Output: filepath.Join(root, "out", "public")}

	content := map[string]string{
		"fi/_index.md":         "---\ntitle: Etusivu\ntranslationKey: home\n---\n",
		"fi/tapahtumat.md":     "---\ntitle: Tapahtumat\ntranslationKey: events\n---\n",
		"fi/opinnot/_index.md": "---\ntitle: Opinnot\ntranslationKey: studies\n---\n",
		"fi/haalarit.md":       "---\ntitle: Haalarit\nslug: overalls\n---\n",
		"fi/uusi.md":           "---\ntitle: Uusi\nurl: /fi/aivan/muualla/\n---\n",
		"en/_index.md":         "---\ntitle: Home\ntranslationKey: home\n---\n",
		"en/events.md":         "---\ntitle: Events\ntranslationKey: events\n---\n",
	}
	for rel, body := range content {
		write(t, filepath.Join(s.Worktree, "site", "content", filepath.FromSlash(rel)), body)
	}

	pages := map[string]string{
		"index.html":                  `<html><body>redirect</body></html>`,
		"fi/index.html":               `<html><body><h1>Etusivu</h1></body></html>`,
		"fi/tapahtumat/index.html":    tapahtumatHTML,
		"fi/opinnot/index.html":       `<html><body><h1>Opinnot</h1></body></html>`,
		"fi/overalls/index.html":      `<html><body><h1>Haalarit</h1></body></html>`,
		"fi/aivan/muualla/index.html": `<html><body><h1>Uusi</h1></body></html>`,
		"en/index.html":               `<html><body><h1>Home</h1></body></html>`,
		"en/events/index.html":        `<html><body><h1>Events</h1></body></html>`,
		"css/main.css":                ".events-header { color: rebeccapurple }\n",
	}
	for rel, body := range pages {
		write(t, filepath.Join(s.Output, filepath.FromSlash(rel)), body)
	}
	return s
}

const tapahtumatHTML = `<html>
<head><title>Tapahtumat</title><link rel="stylesheet" href="/css/main.css"></head>
<body>
<header class="site-header"><h1 class="events-header">Tapahtumat</h1></header>
<main><p>Syksyn tapahtumat.</p></main>
</body>
</html>
`

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// The model has either the file it edited or the address the site serves. Both
// have to land on the same page, because asking it to convert between them is
// asking it to guess.
func TestLocateTakesAContentPathOrAnAddress(t *testing.T) {
	s := testSite(t)
	cases := []struct {
		arg     string
		wantURL string
		wantKey string
	}{
		{"site/content/fi/tapahtumat.md", "/fi/tapahtumat/", "events"},
		{"/site/content/fi/tapahtumat.md", "/fi/tapahtumat/", "events"},
		{"site/content/fi/_index.md", "/fi/", "home"},
		{"site/content/fi/opinnot/_index.md", "/fi/opinnot/", "studies"},
		{"site/content/en/events.md", "/en/events/", "events"},
		// Front matter moves an address two ways, and hugo's output is the only
		// thing that settles where a page ended up.
		{"site/content/fi/haalarit.md", "/fi/overalls/", ""},
		{"site/content/fi/uusi.md", "/fi/aivan/muualla/", ""},
		// The same pages asked for by address, spelled the ways a model spells
		// them.
		{"/fi/tapahtumat/", "/fi/tapahtumat/", "events"},
		{"fi/tapahtumat", "/fi/tapahtumat/", "events"},
		{"/fi/tapahtumat/index.html", "/fi/tapahtumat/", "events"},
		{"/fi/", "/fi/", "home"},
		{"/en/events/", "/en/events/", "events"},
	}
	for _, tc := range cases {
		t.Run(tc.arg, func(t *testing.T) {
			page, err := s.Locate(tc.arg)
			if err != nil {
				t.Fatalf("Locate(%q): %v", tc.arg, err)
			}
			if page.URL != tc.wantURL {
				t.Errorf("URL = %q, want %q", page.URL, tc.wantURL)
			}
			if page.TranslationKey != tc.wantKey {
				t.Errorf("translationKey = %q, want %q", page.TranslationKey, tc.wantKey)
			}
			if _, err := os.Stat(page.File); err != nil {
				t.Errorf("File = %q, which is not there: %v", page.File, err)
			}
		})
	}
}

func TestLocateRefusals(t *testing.T) {
	s := testSite(t)
	cases := []struct {
		name string
		arg  string
		want error
	}{
		{"nothing", "   ", ErrBadPath},
		{"the member tree", "site/content-members/fi/kokous.md", ErrBadPath},
		{"a traversal", "/fi/../../etc/passwd", ErrBadPath},
		{"a traversal in a content path", "site/content/fi/../../../etc/passwd", ErrBadPath},
		{"a page that was never built", "/fi/ei-ole/", ErrNoPage},
		{"a content file that is not there", "site/content/fi/ei-ole.md", ErrNoPage},
		{"a content path naming no page", "site/content/fi", ErrBadPath},
		{"a content path spelled short", "content/fi/tapahtumat.md", ErrBadPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Locate(tc.arg); !errors.Is(err, tc.want) {
				t.Fatalf("Locate(%q) = %v, want %v", tc.arg, err, tc.want)
			}
		})
	}
}

// Before a build there is nothing to look at, and saying so is what sends the
// model to build rather than to a different page.
func TestLocateSaysWhenNothingHasBeenBuilt(t *testing.T) {
	s := Site{Worktree: t.TempDir(), Output: filepath.Join(t.TempDir(), "public")}
	if _, err := s.Locate("/fi/"); !errors.Is(err, ErrNotBuilt) {
		t.Fatalf("Locate against an unbuilt change = %v, want ErrNotBuilt", err)
	}
}

// One screenshot call covers both languages, and the pairing is the
// translationKey rather than a guess at the address: "tapahtumat" is "events" in
// English and nothing about the path says so.
func TestCounterpartsFollowTheTranslationKey(t *testing.T) {
	s := testSite(t)

	page, err := s.Locate("site/content/fi/tapahtumat.md")
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	others, err := s.Counterparts(page)
	if err != nil {
		t.Fatalf("Counterparts: %v", err)
	}
	if len(others) != 1 {
		t.Fatalf("counterparts = %+v, want the English page", others)
	}
	if others[0].URL != "/en/events/" || others[0].Lang != "en" {
		t.Errorf("counterpart = %+v, want /en/events/", others[0])
	}

	// A page with no key has no counterpart, and neither has one whose key is
	// only used in its own language.
	for _, arg := range []string{"site/content/fi/haalarit.md", "site/content/fi/opinnot/_index.md"} {
		page, err := s.Locate(arg)
		if err != nil {
			t.Fatalf("Locate(%q): %v", arg, err)
		}
		if others, err := s.Counterparts(page); err != nil || len(others) != 0 {
			t.Errorf("Counterparts(%q) = %+v, %v; want none", arg, others, err)
		}
	}
}

// A counterpart whose page is not in the output is not a counterpart to show.
// An unbuilt language is not a missing translation.
func TestCounterpartsSkipAPageThatIsNotBuilt(t *testing.T) {
	s := testSite(t)
	if err := os.RemoveAll(filepath.Join(s.Output, "en")); err != nil {
		t.Fatalf("removing the English output: %v", err)
	}
	page, err := s.Locate("site/content/fi/tapahtumat.md")
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	others, err := s.Counterparts(page)
	if err != nil {
		t.Fatalf("Counterparts: %v", err)
	}
	if len(others) != 0 {
		t.Fatalf("counterparts = %+v, want none: the English page is not built", others)
	}
}

func TestCanonicalAddress(t *testing.T) {
	for arg, want := range map[string]string{
		"/fi/tapahtumat/":           "/fi/tapahtumat/",
		"fi/tapahtumat":             "/fi/tapahtumat/",
		"/fi/tapahtumat/index.html": "/fi/tapahtumat/",
		"//fi//tapahtumat//":        "/fi/tapahtumat/",
		"/fi/tapahtumat/?x=1":       "/fi/tapahtumat/",
		"/":                         "/",
		"":                          "/",
		"/robots.txt":               "/robots.txt",
	} {
		if got := canonicalAddress(arg); got != want {
			t.Errorf("canonicalAddress(%q) = %q, want %q", arg, got, want)
		}
	}
}

func TestFrontMatterReadsTheTopLevelScalars(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		body string
		want FrontMatter
	}{
		{"plain", "---\ntitle: Tapahtumat\ntranslationKey: events\n---\n\nText.\n", FrontMatter{TranslationKey: "events"}},
		{"quoted", "---\ntranslationKey: \"events\"\nslug: 'overalls'\n---\n", FrontMatter{TranslationKey: "events", Slug: "overalls"}},
		{"url", "---\nurl: /fi/aivan/muualla/\n---\n", FrontMatter{URL: "/fi/aivan/muualla/"}},
		{"commented", "---\ntranslationKey: events # the English pair\n---\n", FrontMatter{TranslationKey: "events"}},
		{"no front matter", "# Otsikko\n\nText.\n", FrontMatter{}},
		{"empty", "", FrontMatter{}},
		// A nested key of that name belongs to the mapping above it and is not
		// this page's.
		{"nested", "---\ntitle: Tapahtumat\nhero:\n  slug: not-the-page\n---\n", FrontMatter{}},
		// Body text after the closing marker is body text.
		{"key after the block", "---\ntitle: T\n---\n\ntranslationKey: events\n", FrontMatter{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".md")
			write(t, path, tc.body)
			got, err := frontMatter(path)
			if err != nil {
				t.Fatalf("frontMatter: %v", err)
			}
			if got != tc.want {
				t.Fatalf("frontMatter = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Reading +++ as YAML would return nothing, and a page with no translation key
// looks exactly like a page with no counterpart. It says so instead.
func TestFrontMatterRefusesTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page.md")
	write(t, path, "+++\ntranslationKey = \"events\"\n+++\n")
	_, err := frontMatter(path)
	if err == nil || !strings.Contains(err.Error(), "TOML") {
		t.Fatalf("frontMatter of a TOML page = %v, want it to name TOML", err)
	}
}
