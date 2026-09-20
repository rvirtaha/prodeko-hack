package fence

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The layout fence, file by file. These are the real names in the site's layouts
// tree, so the table is also the statement of what the media team may do to it.
func TestLayoutWritability(t *testing.T) {
	cases := []struct {
		rel   string
		write bool
	}{
		// The partials a page is assembled from.
		{"site/layouts/partials/header.html", true},
		{"site/layouts/partials/footer-note.html", true},
		{"site/layouts/partials/page-shell.html", true},
		{"site/layouts/partials/people-grid.html", true},
		{"site/layouts/partials/breadcrumbs.html", true},
		{"site/layouts/partials/section-nav.html", true},
		{"site/layouts/partials/nested/card.html", true},
		// The page layouts, under both of Hugo's naming schemes.
		{"site/layouts/home.html", true},
		{"site/layouts/section.html", true},
		{"site/layouts/page.html", true},
		{"site/layouts/_default/list.html", true},
		{"site/layouts/_default/single.html", true},
		// The skeleton.
		{"site/layouts/baseof.html", false},
		{"site/layouts/_default/baseof.html", false},
		{"site/layouts/partials/head.html", false},
		{"site/layouts/partials/footer.html", false},
		// The bilingual pairing.
		{"site/layouts/partials/lang-switch.html", false},
		// Render hooks and shortcodes, wherever Hugo keeps them.
		{"site/layouts/_markup/render-image.html", false},
		{"site/layouts/_default/_markup/render-link.html", false},
		{"site/layouts/_shortcodes/ilmo.html", false},
		{"site/layouts/shortcodes/ilmo.html", false},
		// Page types of their own, and anything the table does not name.
		{"site/layouts/alumni-hub.html", false},
		{"site/layouts/corporate-hub.html", false},
		{"site/layouts/people.html", false},
		{"site/layouts/archive.html", false},
		{"site/layouts/index.html", false},
		{"site/layouts/robots.txt", false},
	}
	for _, tc := range cases {
		t.Run(tc.rel, func(t *testing.T) {
			// Every template is readable; that is what makes a CSS selector
			// findable and a fenced template quotable to the person asking.
			if _, ok := Match(tc.rel); !ok {
				t.Fatalf("Match(%q) covers no rule, so the template cannot even be read", tc.rel)
			}
			err := Writable(tc.rel)
			if tc.write && err != nil {
				t.Fatalf("Writable(%q) = %v, want nil", tc.rel, err)
			}
			if !tc.write {
				if !errors.Is(err, ErrReadOnly) {
					t.Fatalf("Writable(%q) = %v, want ErrReadOnly", tc.rel, err)
				}
				// A refusal that does not say why sends the model looking for
				// another way in rather than to a developer.
				if !strings.Contains(err.Error(), "outside the fence, deliberately") {
					t.Errorf("the refusal does not place the file outside the fence: %v", err)
				}
				if !strings.Contains(err.Error(), "site/layouts/partials/**") {
					t.Errorf("the refusal does not name what is writable instead: %v", err)
				}
			}
		})
	}
}

// Writing is the only thing the layout fence narrows. A template nobody may
// write is still a template everybody may read.
func TestEveryLayoutStaysReadable(t *testing.T) {
	f, _ := worktree(t)
	for _, rel := range []string{
		"site/layouts/baseof.html",
		"site/layouts/partials/head.html",
		"site/layouts/_shortcodes/ilmo.html",
		"site/layouts/partials/header.html",
	} {
		if err := f.CheckRead(rel); err != nil {
			t.Errorf("CheckRead(%q): %v", rel, err)
		}
	}
}

// The write checks and the staging check are three doors into the same room, and
// a template that may not be written may not arrive in a commit either.
func TestEveryWritePathAppliesTheLayoutFence(t *testing.T) {
	f, root := worktree(t)
	const fenced = "site/layouts/partials/head.html"
	const open = "site/layouts/partials/header.html"
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(fenced)), "<link>\n")
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(open)), "<header></header>\n")

	if err := f.CheckWrite(fenced, 8); !errors.Is(err, ErrReadOnly) {
		t.Errorf("CheckWrite(%q) = %v, want ErrReadOnly", fenced, err)
	}
	if _, err := f.ResolveWrite(fenced, 8); !errors.Is(err, ErrReadOnly) {
		t.Errorf("ResolveWrite(%q) = %v, want ErrReadOnly", fenced, err)
	}
	if _, err := f.CheckStage(fenced); !errors.Is(err, ErrReadOnly) {
		t.Errorf("CheckStage(%q) = %v, want ErrReadOnly", fenced, err)
	}
	if err := f.CheckChange([]string{fenced}, 8); !errors.Is(err, ErrReadOnly) {
		t.Errorf("CheckChange(%q) = %v, want ErrReadOnly", fenced, err)
	}

	if err := f.CheckWrite(open, 8); err != nil {
		t.Errorf("CheckWrite(%q): %v", open, err)
	}
	if _, err := f.CheckStage(open); err != nil {
		t.Errorf("CheckStage(%q): %v", open, err)
	}
	if err := f.CheckChange([]string{open}, 8); err != nil {
		t.Errorf("CheckChange(%q): %v", open, err)
	}
}

// The content rule: a template that builds an asset is read-only whatever its
// name, and a writable one does not become one by being written into.
func TestCheckTemplateAndTheAssetPipeline(t *testing.T) {
	f, root := worktree(t)
	const bundler = "site/layouts/partials/bundle.html"
	const plain = "site/layouts/partials/header.html"
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(bundler)),
		"{{ $css := resources.Get \"css/main.css\" | minify | fingerprint }}\n")
	mustWriteFile(t, filepath.Join(root, filepath.FromSlash(plain)),
		"<header>{{ with resources.Get \"images/logo.svg\" }}<img src=\"{{ .RelPermalink }}\">{{ end }}</header>\n")

	cases := []struct {
		name    string
		rel     string
		content string
		want    error
	}{
		{"a template that already bundles", bundler, "<p>hei</p>\n", ErrReadOnly},
		{"a partial that would start bundling", plain,
			"{{ $css := resources.Concat \"css/x.css\" }}\n", ErrReadOnly},
		{"a partial that would cache itself", plain,
			"{{ partialCached \"people-grid.html\" . }}\n", ErrReadOnly},
		{"an image the partial points at", plain,
			"<header>{{ with resources.Get \"images/logo.svg\" }}<img>{{ end }}</header>\n", nil},
		{"ordinary markup", plain, "<header><h1>Prodeko</h1></header>\n", nil},
		{"a new partial", "site/layouts/partials/uusi.html", "<p>uusi</p>\n", nil},
		// The words this looks for are ordinary ones outside a template, and a
		// stylesheet is not a template.
		{"a stylesheet mentioning the pipeline", "site/assets/css/main.css",
			"/* the bundle is built with minify and fingerprint */\n.a { color: red }\n", nil},
		{"a page mentioning the pipeline", "site/content/fi/index.md",
			"---\ntitle: Hei\n---\n\nHugo can minify a stylesheet.\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := f.CheckTemplate(tc.rel, []byte(tc.content))
			if tc.want == nil {
				if err != nil {
					t.Fatalf("CheckTemplate(%q) = %v, want nil", tc.rel, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("CheckTemplate(%q) = %v, want %v", tc.rel, err, tc.want)
			}
			// The refusal has to name the call it saw, or the model cannot tell
			// which part of what it wrote was the problem.
			named := false
			for _, call := range []string{"resources.", "partialCached", "minify", "fingerprint"} {
				if strings.Contains(err.Error(), call) {
					named = true
				}
			}
			if !named {
				t.Errorf("the refusal names no pipeline call: %v", err)
			}
		})
	}
}

// get_conventions prints these and nothing else, so every group has to carry the
// two things it prints: what it is, and the line explaining it.
func TestLayoutGroupsAreStatable(t *testing.T) {
	groups := LayoutGroups()
	if len(groups) < 3 {
		t.Fatalf("LayoutGroups() has %d groups, which cannot describe the fence", len(groups))
	}
	var writable int
	for _, g := range groups {
		if g.Paths == "" {
			t.Errorf("a group names no paths: %+v", g)
		}
		if g.Why == "" {
			t.Errorf("group %q has no reason, and the reason is what it is for", g.Paths)
		}
		if !strings.HasSuffix(strings.TrimSpace(g.Why), ".") {
			t.Errorf("group %q reads as a fragment in a sentence: %q", g.Paths, g.Why)
		}
		if g.Write {
			writable++
		}
	}
	if writable == 0 {
		t.Error("no group is writable, so the layout fence lets nobody edit anything")
	}

	// The asset pipeline rule is about content rather than about a path, and
	// stating the fence without it would promise a partial nobody may write.
	var mentioned bool
	for _, g := range groups {
		if strings.Contains(g.Why, "fingerprint") {
			mentioned = true
		}
	}
	if !mentioned {
		t.Error("the groups do not state the asset pipeline rule")
	}
}

// One list, in one place: the sentence a refusal uses comes from the same table
// get_conventions prints.
func TestWritableLayoutsNamesEveryWritableGroup(t *testing.T) {
	got := WritableLayouts()
	for _, g := range LayoutGroups() {
		if g.Write && !strings.Contains(got, g.Paths) {
			t.Errorf("WritableLayouts() = %q, which omits %q", got, g.Paths)
		}
	}
}

// The roots a refusal names have to say that layouts are partly writable, or a
// model reading "read only" will not try the partial it is allowed to edit.
func TestRootsNamesTheLayoutException(t *testing.T) {
	got := Roots()
	for _, want := range []string{"site/content/**", LayoutsRoot + "**", "site/layouts/partials/**"} {
		if !strings.Contains(got, want) {
			t.Errorf("Roots() = %q, which omits %q", got, want)
		}
	}
}

func mustWriteFile(t *testing.T, abs, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}
