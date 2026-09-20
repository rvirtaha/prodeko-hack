package workdir

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixture's site carries fi and en pages already; these tests add pages
// with known keys and commit them at spaced times through the fixture's own
// git, so the ages the report reads are the ones written here.

func writePage(t *testing.T, f *fixture, c *Change, rel, key string) {
	t.Helper()
	abs := filepath.Join(c.Dir, filepath.FromSlash(rel))
	mustMkdir(t, filepath.Dir(abs))
	body := "---\ntitle: T\n"
	if key != "" {
		body += "translationKey: " + key + "\n"
	}
	body += "---\n\nSisältö.\n"
	mustWrite(t, abs, body)
}

// commitAt commits everything in the change's worktree with both git dates
// pinned, so the ages the report reads are the ones written here.
func commitAt(t *testing.T, f *fixture, c *Change, when time.Time, msg string) {
	t.Helper()
	f.git(t, c.Dir, "add", "-A")
	cmd := exec.Command("git",
		"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org",
		"commit", "--quiet", "--message", msg)
	cmd.Dir = c.Dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + f.home,
		"GIT_CONFIG_NOSYSTEM=1",
		"LC_ALL=C",
		"GIT_AUTHOR_DATE=" + when.Format(time.RFC3339),
		"GIT_COMMITTER_DATE=" + when.Format(time.RFC3339),
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit at %s: %v\n%s", when, err, out)
	}
}

func TestTranslationStatusPairsAndReportsDrift(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	c := openChange(t, m)

	old := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	writePage(t, f, c, "site/content/fi/vanha.md", "old-pair")
	writePage(t, f, c, "site/content/en/old.md", "old-pair")
	writePage(t, f, c, "site/content/fi/yksin.md", "alone")
	writePage(t, f, c, "site/content/fi/avaimeton.md", "")
	commitAt(t, f, c, old, "both sides")

	writePage(t, f, c, "site/content/fi/vanha.md", "old-pair") // touch fi again
	abs := filepath.Join(c.Dir, "site/content/fi/vanha.md")
	mustWrite(t, abs, strings.Replace(readFile(t, abs), "Sisältö.", "Uusi sisältö.", 1))
	commitAt(t, f, c, newer, "fi moved on")

	s, err := m.TranslationStatus(t.Context(), c, "")
	if err != nil {
		t.Fatalf("TranslationStatus: %v", err)
	}
	var missing []string
	for _, p := range s.Missing {
		missing = append(missing, p.Path)
	}
	if !contains(missing, "site/content/fi/yksin.md") {
		t.Errorf("Missing = %v, want yksin.md in it", missing)
	}
	if !contains(s.Unkeyed, "site/content/fi/avaimeton.md") {
		t.Errorf("Unkeyed = %v", s.Unkeyed)
	}
	var found bool
	for _, pair := range s.Stale {
		if pair.Key == "old-pair" {
			found = true
			if pair.Newer.Path != "site/content/fi/vanha.md" || pair.Older.Path != "site/content/en/old.md" {
				t.Errorf("pair = %+v", pair)
			}
		}
	}
	if !found {
		t.Errorf("Stale = %+v, want the old-pair drift", s.Stale)
	}
}

func TestTranslationStatusHonoursThePathFilter(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	c := openChange(t, m)

	writePage(t, f, c, "site/content/fi/rajattu/sivu.md", "scoped")
	commitAt(t, f, c, time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), "one page")

	s, err := m.TranslationStatus(t.Context(), c, "site/content/fi/rajattu")
	if err != nil {
		t.Fatalf("TranslationStatus: %v", err)
	}
	if s.Total != 1 {
		t.Errorf("Total = %d under the filter, want 1", s.Total)
	}
	if len(s.Missing) != 1 || s.Missing[0].Path != "site/content/fi/rajattu/sivu.md" {
		t.Errorf("Missing = %+v", s.Missing)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func readFile(t *testing.T, abs string) string {
	t.Helper()
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("reading %s: %v", abs, err)
	}
	return string(data)
}
