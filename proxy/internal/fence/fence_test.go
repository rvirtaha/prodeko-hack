package fence

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRulesCoverTheDesignedTree(t *testing.T) {
	want := map[string]bool{
		"site/content/":         true,
		"site/content-members/": true,
		"site/data/":            true,
		"site/assets/css/":      true,
		"site/assets/images/":   true,
		"site/layouts/":         false,
	}
	got := Rules()
	if len(got) != len(want) {
		t.Fatalf("Rules() has %d entries, want %d", len(got), len(want))
	}
	for _, r := range got {
		write, ok := want[r.Prefix]
		if !ok {
			t.Errorf("unexpected rule %q", r.Prefix)
			continue
		}
		if r.Write != write {
			t.Errorf("rule %q: write = %v, want %v", r.Prefix, r.Write, write)
		}
	}
}

// The allowlist must not be reachable through the slice callers are handed.
func TestRulesReturnsACopy(t *testing.T) {
	got := Rules()
	got[0] = Rule{Prefix: ".github/", Write: true}
	if Rules()[0].Prefix == ".github/" {
		t.Fatal("mutating the returned slice changed the allowlist")
	}
}

func TestNewFenceRejectsEmptyRoot(t *testing.T) {
	if _, err := NewFence(""); !errors.Is(err, ErrBadPath) {
		t.Fatalf("NewFence(\"\") error = %v, want ErrBadPath", err)
	}
}

// A root that is not there is not a fence that allows everything.
func TestMissingRootDenies(t *testing.T) {
	f, err := NewFence(filepath.Join(t.TempDir(), "never-created"))
	if err != nil {
		t.Fatalf("NewFence: %v", err)
	}
	if err := f.CheckRead("site/content/fi/index.md"); !errors.Is(err, ErrOutside) {
		t.Errorf("CheckRead with no worktree = %v, want ErrOutside", err)
	}
	if err := f.CheckWrite("site/content/fi/index.md", 10); !errors.Is(err, ErrOutside) {
		t.Errorf("CheckWrite with no worktree = %v, want ErrOutside", err)
	}
}

func TestCleanRejects(t *testing.T) {
	cases := []struct {
		name string
		rel  string
	}{
		{"empty", ""},
		{"dot", "."},
		{"dot dot", ".."},
		{"literal traversal", "site/content/../../etc/passwd"},
		{"trailing traversal", "site/content/fi/.."},
		{"encoded traversal lower", "site/content/%2e%2e/etc/passwd"},
		{"encoded traversal upper", "site/content/%2E%2E/etc/passwd"},
		{"encoded traversal with slash", "site/content/%2e%2e%2fetc%2fpasswd"},
		{"encoded slash hiding a traversal", "site/content/x%2F..%2Fy"},
		{"single dot segment", "site/content/./fi/index.md"},
		{"absolute", "/etc/passwd"},
		{"empty segment", "site/content//fi/index.md"},
		{"trailing slash", "site/content/fi/"},
		{"backslash", `site\content\fi\index.md`},
		{"encoded backslash", "site/content/fi%5C..%5Cx.md"},
		{"null byte", "site/content/fi/index.md\x00"},
		{"encoded null byte", "site/content/fi/index%00.md"},
		{"newline", "site/content/fi/in\ndex.md"},
		{"delete", "site/content/fi/index\x7f.md"},
		{"bad percent encoding", "site/content/fi/100%zz.md"},
		{"too long", "site/content/fi/" + strings.Repeat("a", maxPathLen)},
		{"invalid utf-8", "site/content/fi/\xff.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := Clean(tc.rel); !errors.Is(err, ErrBadPath) {
				t.Fatalf("Clean(%q) = %q, %v; want ErrBadPath", tc.rel, got, err)
			}
			// Nothing a Clean refuses may be visible or matched either.
			if _, ok := Match(tc.rel); ok {
				t.Errorf("Match(%q) reported a rule for a path Clean refuses", tc.rel)
			}
		})
	}
}

func TestCleanAccepts(t *testing.T) {
	for _, rel := range []string{
		"site/content/fi/index.md",
		"site/content/fi/tapahtumat/_index.md",
		"site/data/board-2026.yaml",
		"site/assets/css/tokens/colors.css",
		"site/layouts/partials/header.html",
		"README.md",
		"site/content/fi/..hidden.md",
		"site/content/fi/kesätyö.md",
	} {
		got, err := Clean(rel)
		if err != nil {
			t.Errorf("Clean(%q): %v", rel, err)
			continue
		}
		if got != rel {
			t.Errorf("Clean(%q) = %q, want it unchanged", rel, got)
		}
	}
}

func TestMatchAndVisible(t *testing.T) {
	f, err := NewFence(t.TempDir())
	if err != nil {
		t.Fatalf("NewFence: %v", err)
	}
	cases := []struct {
		rel     string
		visible bool
		write   bool
	}{
		{"site/content/fi/index.md", true, true},
		{"site/content-members/fi/poytakirjat/2026-01.md", true, true},
		{"site/data/navigation.yaml", true, true},
		{"site/assets/css/main.css", true, true},
		{"site/assets/images/logo.svg", true, true},
		{"site/layouts/_default/baseof.html", true, false},
		{".github/workflows/preview.yml", false, false},
		{"site/hugo.toml", false, false},
		{"site/config/members/hugo.toml", false, false},
		{"site/static/admin/config.yml", false, false},
		{"site/check-trees.sh", false, false},
		{"site/assets/js/app.js", false, false},
		{"proxy/internal/fence/fence.go", false, false},
		{"docs/design.md", false, false},
		// A rule covers files under a root, never the root itself, and never
		// a sibling whose name merely starts the same way.
		{"site/content", false, false},
		{"site/contentious/x.md", false, false},
	}
	for _, tc := range cases {
		rule, ok := Match(tc.rel)
		if ok != tc.visible {
			t.Errorf("Match(%q) matched = %v, want %v", tc.rel, ok, tc.visible)
		}
		if got := f.Visible(tc.rel); got != tc.visible {
			t.Errorf("Visible(%q) = %v, want %v", tc.rel, got, tc.visible)
		}
		if ok && rule.Write != tc.write {
			t.Errorf("Match(%q) write = %v, want %v", tc.rel, rule.Write, tc.write)
		}
	}
}

// A path percent-encoded whole is one segment, and one segment named
// "site%2Fcontent%2Ffi%2Fx.md" is not under any root.
func TestEncodedSlashDoesNotEnterTheTree(t *testing.T) {
	const rel = "site%2Fcontent%2Ffi%2Fx.md"
	if _, ok := Match(rel); ok {
		t.Errorf("Match(%q) entered the tree through an encoded slash", rel)
	}
}

// worktree builds the fixture tree: the editable roots, the denied ones, and a
// fence over it.
func worktree(t *testing.T) (*Fence, string) {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"site/content/fi/index.md":          "# Etusivu\n",
		"site/content/en/index.md":          "# Home\n",
		"site/content-members/fi/kokous.md": "# Kokous\n",
		"site/data/navigation.yaml":         "fi: []\n",
		"site/assets/css/main.css":          ":root { color: red }\n",
		"site/assets/css/tokens/colors.css": ":root { --brand: #002b5c }\n",
		"site/assets/images/logo.svg":       "<svg/>\n",
		"site/layouts/_default/baseof.html": "<html>{{ block \"main\" . }}{{ end }}</html>\n",
		"site/hugo.toml":                    "baseURL = \"/\"\n",
		"site/check-trees.sh":               "#!/bin/sh\n",
		".github/workflows/preview.yml":     "on: pull_request\n",
		"site/static/admin/config.yml":      "backend: {}\n",
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	f, err := NewFence(root)
	if err != nil {
		t.Fatalf("NewFence: %v", err)
	}
	return f, root
}

func TestCheckReadAndWriteAcrossTheTree(t *testing.T) {
	f, root := worktree(t)

	for _, rel := range []string{
		"site/content/fi/index.md",
		"site/content-members/fi/kokous.md",
		"site/data/navigation.yaml",
		"site/assets/css/tokens/colors.css",
		"site/assets/images/logo.svg",
	} {
		if err := f.CheckRead(rel); err != nil {
			t.Errorf("CheckRead(%q): %v", rel, err)
		}
		if err := f.CheckWrite(rel, 32); err != nil {
			t.Errorf("CheckWrite(%q): %v", rel, err)
		}
	}

	// A file that is not there yet is writable; its parent is created later.
	if err := f.CheckWrite("site/content/fi/uusi/sivu.md", 32); err != nil {
		t.Errorf("CheckWrite on a new path: %v", err)
	}

	// Templates are read to find a selector and never written.
	if err := f.CheckRead("site/layouts/_default/baseof.html"); err != nil {
		t.Errorf("CheckRead of a layout: %v", err)
	}
	if err := f.CheckWrite("site/layouts/_default/baseof.html", 32); !errors.Is(err, ErrReadOnly) {
		t.Errorf("CheckWrite of a layout = %v, want ErrReadOnly", err)
	}

	// The denies that carry the security of the feature, both ways.
	for _, rel := range []string{
		".github/workflows/preview.yml",
		"site/hugo.toml",
		"site/static/admin/config.yml",
		"site/check-trees.sh",
	} {
		if err := f.CheckRead(rel); !errors.Is(err, ErrOutside) {
			t.Errorf("CheckRead(%q) = %v, want ErrOutside", rel, err)
		}
		if err := f.CheckWrite(rel, 32); !errors.Is(err, ErrOutside) {
			t.Errorf("CheckWrite(%q) = %v, want ErrOutside", rel, err)
		}
		if f.Visible(rel) {
			t.Errorf("Visible(%q) = true", rel)
		}
	}

	// A refusal names where work is possible instead.
	err := f.CheckRead(".github/workflows/preview.yml")
	if !strings.Contains(err.Error(), "site/content/") {
		t.Errorf("refusal %q names no editable root", err)
	}

	// A directory is not a file, even inside an editable root.
	if err := f.CheckRead("site/content/fi"); !errors.Is(err, ErrBadPath) {
		t.Errorf("CheckRead of a directory = %v, want ErrBadPath", err)
	}
	if err := f.CheckWrite("site/content/fi", 10); !errors.Is(err, ErrBadPath) {
		t.Errorf("CheckWrite of a directory = %v, want ErrBadPath", err)
	}
	if err := f.CheckRead("site/content/fi/"); !errors.Is(err, ErrBadPath) {
		t.Errorf("CheckRead of a trailing slash = %v, want ErrBadPath", err)
	}

	// ResolveRead hands back the path to open, inside the worktree.
	abs, err := f.ResolveRead("site/content/fi/index.md")
	if err != nil {
		t.Fatalf("ResolveRead: %v", err)
	}
	if want := filepath.Join(root, "site", "content", "fi", "index.md"); abs != want {
		// The root itself may be a symlink (macOS /var), so compare resolved.
		resolvedRoot, _ := filepath.EvalSymlinks(root)
		if want := filepath.Join(resolvedRoot, "site", "content", "fi", "index.md"); abs != want {
			t.Errorf("ResolveRead = %q, want %q", abs, want)
		}
	}
}

func TestSymlinkEscape(t *testing.T) {
	f, root := worktree(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("token\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	link := filepath.Join(root, "site", "content", "fi", "secret.md")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := f.CheckRead("site/content/fi/secret.md"); !errors.Is(err, ErrSymlink) {
		t.Errorf("CheckRead through an escaping symlink = %v, want ErrSymlink", err)
	}
	if err := f.CheckWrite("site/content/fi/secret.md", 8); !errors.Is(err, ErrSymlink) {
		t.Errorf("CheckWrite through an escaping symlink = %v, want ErrSymlink", err)
	}

	// A symlinked directory is the same escape one level up.
	dirLink := filepath.Join(root, "site", "content", "fi", "out")
	if err := os.Symlink(outside, dirLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := f.CheckWrite("site/content/fi/out/new.md", 8); !errors.Is(err, ErrSymlink) {
		t.Errorf("CheckWrite below an escaping directory symlink = %v, want ErrSymlink", err)
	}

	// A symlink to nothing is refused rather than followed: a write through it
	// would land wherever it points.
	broken := filepath.Join(root, "site", "content", "fi", "broken.md")
	if err := os.Symlink(filepath.Join(outside, "not-there.txt"), broken); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := f.CheckWrite("site/content/fi/broken.md", 8); !errors.Is(err, ErrSymlink) {
		t.Errorf("CheckWrite through a broken symlink = %v, want ErrSymlink", err)
	}

	// A symlink that stays inside is not an escape.
	inside := filepath.Join(root, "site", "content", "en", "alias.md")
	if err := os.Symlink(filepath.Join(root, "site", "content", "fi", "index.md"), inside); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := f.CheckRead("site/content/en/alias.md"); err != nil {
		t.Errorf("CheckRead through a symlink inside the worktree: %v", err)
	}
}

func TestSizeLimits(t *testing.T) {
	f, root := worktree(t)

	if err := f.CheckWrite("site/content/fi/index.md", MaxFileBytes); err != nil {
		t.Errorf("CheckWrite at the limit: %v", err)
	}
	if err := f.CheckWrite("site/content/fi/index.md", MaxFileBytes+1); !errors.Is(err, ErrTooLarge) {
		t.Errorf("CheckWrite over the limit = %v, want ErrTooLarge", err)
	}

	big := filepath.Join(root, "site", "assets", "images", "big.png")
	if err := os.WriteFile(big, nil, 0o644); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.Truncate(big, MaxFileBytes+1); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := f.CheckRead("site/assets/images/big.png"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("CheckRead of an oversized file = %v, want ErrTooLarge", err)
	}
	if err := os.Truncate(big, MaxFileBytes); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := f.CheckRead("site/assets/images/big.png"); err != nil {
		t.Errorf("CheckRead at the limit: %v", err)
	}
}

func TestCheckChange(t *testing.T) {
	f, _ := worktree(t)

	paths := make([]string, MaxChangeFiles)
	for i := range paths {
		paths[i] = "site/content/fi/page.md"
	}
	if err := f.CheckChange(paths, MaxTextBytes); err != nil {
		t.Errorf("CheckChange at the limits: %v", err)
	}
	if err := f.CheckChange(append(paths, "site/content/fi/one-more.md"), 10); !errors.Is(err, ErrTooManyFiles) {
		t.Errorf("CheckChange over the file count = %v, want ErrTooManyFiles", err)
	}
	if err := f.CheckChange(paths, MaxTextBytes+1); !errors.Is(err, ErrTooLarge) {
		t.Errorf("CheckChange over the text limit = %v, want ErrTooLarge", err)
	}
	if err := f.CheckChange([]string{"site/layouts/_default/baseof.html"}, 10); !errors.Is(err, ErrReadOnly) {
		t.Errorf("CheckChange of a layout = %v, want ErrReadOnly", err)
	}
	if err := f.CheckChange([]string{".github/workflows/preview.yml"}, 10); !errors.Is(err, ErrOutside) {
		t.Errorf("CheckChange of a workflow = %v, want ErrOutside", err)
	}
	if err := f.CheckChange([]string{"site/content/fi/../../../etc/passwd"}, 10); !errors.Is(err, ErrBadPath) {
		t.Errorf("CheckChange of a traversal = %v, want ErrBadPath", err)
	}
}

// The staging check is the lexical rule plus the filesystem: a symlink is
// exactly what a path pattern cannot see and what git would happily commit.
func TestCheckStage(t *testing.T) {
	f, root := worktree(t)

	page := filepath.Join(root, "site", "content", "fi", "index.md")
	if err := os.WriteFile(page, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if n, err := f.CheckStage("site/content/fi/index.md"); err != nil || n != 6 {
		t.Errorf("CheckStage of a page = %d, %v, want 6, nil", n, err)
	}
	// A path that is gone is a deletion, which is an ordinary edit.
	if n, err := f.CheckStage("site/content/fi/deleted.md"); err != nil || n != 0 {
		t.Errorf("CheckStage of a deletion = %d, %v, want 0, nil", n, err)
	}
	if _, err := f.CheckStage("site/layouts/index.html"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("CheckStage of a layout = %v, want ErrReadOnly", err)
	}
	if _, err := f.CheckStage(".github/workflows/preview.yml"); !errors.Is(err, ErrOutside) {
		t.Errorf("CheckStage of a workflow = %v, want ErrOutside", err)
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("token\n"), 0o600); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	escapes := filepath.Join(root, "site", "content", "passwd.md")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), escapes); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := f.CheckStage("site/content/passwd.md"); !errors.Is(err, ErrSymlink) {
		t.Errorf("CheckStage of an escaping symlink = %v, want ErrSymlink", err)
	}
	// A link that stays inside the worktree is readable, and still not
	// committable: git would store the link, not the file it names.
	inside := filepath.Join(root, "site", "content", "alias.md")
	if err := os.Symlink(page, inside); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := f.CheckStage("site/content/alias.md"); !errors.Is(err, ErrSymlink) {
		t.Errorf("CheckStage of an in-tree symlink = %v, want ErrSymlink", err)
	}
	dirLink := filepath.Join(root, "site", "data", "root")
	if err := os.Symlink(outside, dirLink); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := f.CheckStage("site/data/root"); !errors.Is(err, ErrSymlink) {
		t.Errorf("CheckStage of a symlinked directory = %v, want ErrSymlink", err)
	}
	if _, err := f.CheckStage("site/data/root/secret.txt"); !errors.Is(err, ErrSymlink) {
		t.Errorf("CheckStage below a symlinked directory = %v, want ErrSymlink", err)
	}
}

// Every denial is one of the named reasons, so a tool can tell the model what
// to do differently.
func TestDenialsAreNamedReasons(t *testing.T) {
	f, _ := worktree(t)
	cases := []struct {
		rel  string
		want error
	}{
		{"site/content/fi/../../secret", ErrBadPath},
		{"site/hugo.toml", ErrOutside},
		{"site/layouts/_default/baseof.html", ErrReadOnly},
	}
	for _, tc := range cases {
		err := f.CheckWrite(tc.rel, 10)
		if !errors.Is(err, tc.want) {
			t.Errorf("CheckWrite(%q) = %v, want %v", tc.rel, err, tc.want)
		}
		if !strings.HasPrefix(err.Error(), "fence: ") {
			t.Errorf("CheckWrite(%q) error %q does not name the fence", tc.rel, err)
		}
	}
}
