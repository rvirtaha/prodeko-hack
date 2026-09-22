package toolset

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prodeko/prodeko-hack/proxy/internal/mcpserver"
	"github.com/prodeko/prodeko-hack/proxy/internal/workdir"
)

// pekka is the second editor: what one person's change does must be invisible
// to everybody else's conversation.
var pekka = mcpserver.Identity{Username: "pekka", Name: "Pekka Prodeko", Email: "pekka@prodeko.org"}

// A conversation is a session here, and a session is a clock and a binding.
// Which tree a tool call lands in is as much git's answer as this package's, so
// the manager and its worktrees are real; only the clock is the test's.

// fixtureSite is the smallest thing hugo will build, with one page per
// language so a read has something to answer and a pair to find.
var fixtureSite = map[string]string{
	"hugo.toml":                    "baseURL = \"https://example.org/\"\ntitle = \"Fixture\"\n",
	"content/fi/tapahtumat.md":     "---\ntitle: Tapahtumat\ntranslationKey: events\n---\n\nTapahtumia tulossa.\n",
	"content/en/events.md":         "---\ntitle: Events\ntranslationKey: events\n---\n\nEvents coming up.\n",
	"layouts/_default/single.html": "<html><body><h1 class=\"otsikko\">{{ .Title }}</h1>{{ .Content }}</body></html>\n",
}

// fixture is a tool set over a real clone of that site, with a clock the test
// moves. A machine without git skips rather than pretends.
type fixture struct {
	ts    *Toolset
	mgr   *workdir.Manager
	root  string
	state string
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}

	root := t.TempDir()
	f := &fixture{
		root:  root,
		state: filepath.Join(root, "state"),
		now:   time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC),
	}
	origin, seed, repo := filepath.Join(root, "origin.git"), filepath.Join(root, "seed"), filepath.Join(root, "repo")
	for rel, body := range fixtureSite {
		writeFixtureFile(t, filepath.Join(seed, "site", filepath.FromSlash(rel)), body)
	}

	f.git(t, root, "init", "--bare", "--initial-branch=main", origin)
	f.git(t, seed, "init", "--initial-branch=main", ".")
	f.git(t, seed, "add", "-A")
	f.git(t, seed, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.org",
		"commit", "--quiet", "--message", "Seed the site")
	f.git(t, seed, "remote", "add", "origin", origin)
	f.git(t, seed, "push", "--quiet", "origin", "main")
	f.git(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	f.git(t, root, "clone", "--quiet", origin, repo)

	quiet := slog.New(slog.DiscardHandler)
	mgr, err := workdir.New(workdir.Config{
		RepoPath:  repo,
		StateDir:  f.state,
		Committer: workdir.Author{Name: "Prodeko media bot", Email: "media-bot@prodeko.org"},
		Logger:    quiet,
	})
	if err != nil {
		t.Fatalf("workdir.New: %v", err)
	}
	ts, err := New(Config{Workdir: mgr, Logger: quiet, Now: f.clock})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.mgr, f.ts = mgr, ts
	return f
}

// clock is what the tool set reads the time from, so a test can put an idle
// three quarters of an hour between two calls without waiting for it.
func (f *fixture) clock() time.Time { return f.now }

func (f *fixture) advance(d time.Duration) { f.now = f.now.Add(d) }

// worktree is where a change's tree would be, whether or not one was opened.
func (f *fixture) worktree(user, slug string) string {
	return filepath.Join(f.state, "wt", user, slug)
}

// baseView is the shared read-only checkout every read serves from until a
// change is bound.
func (f *fixture) baseView() string { return filepath.Join(f.state, "wt", ".base") }

// slugs is the changes one person has open on disk, which is the only record
// of them there is.
func (f *fixture) slugs(t *testing.T, user string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.state, "wt", user))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("reading the open changes: %v", err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func (f *fixture) git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + f.root,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeFixtureFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// A conversation that only looks at the site is the common one, and it must
// cost nothing: the read comes out of the shared checkout and leaves no change
// behind to count against the person's three.
func TestANewSessionStartsOnTheBase(t *testing.T) {
	f := newFixture(t)

	out, err := call(t, f.ts, ToolReadFile, maija, `{"path":"site/content/fi/tapahtumat.md"}`)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(out, "Tapahtumia tulossa.") {
		t.Fatalf("read_file answered:\n%s", out)
	}
	if slugs := f.slugs(t, "maija"); len(slugs) != 0 {
		t.Fatalf("a read opened %v", slugs)
	}
	if _, err := os.Stat(filepath.Join(f.baseView(), "site", "hugo.toml")); err != nil {
		t.Fatalf("the read did not come from the base view: %v", err)
	}
}

// The first write is what opens a change, and the second one belongs to the
// same piece of work: a person editing two files is not making two changes.
func TestTheFirstWriteOpensAFreshChange(t *testing.T) {
	f := newFixture(t)

	if _, err := call(t, f.ts, ToolWriteFile, maija, `{"path":"site/content/fi/uutinen.md","content":"# Uutinen\n"}`); err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if _, err := call(t, f.ts, ToolWriteFile, maija, `{"path":"site/content/fi/toinen.md","content":"# Toinen\n"}`); err != nil {
		t.Fatalf("the second write_file: %v", err)
	}

	if got := f.slugs(t, "maija"); len(got) != 1 || got[0] != "uutinen" {
		t.Fatalf("open changes = %v, want the one named after the first edit", got)
	}
	out, err := call(t, f.ts, ToolListMyChanges, maija, `{}`)
	if err != nil {
		t.Fatalf("list_my_changes: %v", err)
	}
	for _, want := range []string{"uutinen", "site/content/fi/uutinen.md", "site/content/fi/toinen.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("list_my_changes omits %q:\n%s", want, out)
		}
	}
}

// Silence is the only conversation boundary this server can see. An edit after
// the idle window is a new piece of work and gets a change of its own, rather
// than landing in yesterday's half-finished one.
func TestAnIdleSessionExpires(t *testing.T) {
	f := newFixture(t)

	if _, err := call(t, f.ts, ToolWriteFile, maija, `{"path":"site/content/fi/uutinen.md","content":"# Uutinen\n"}`); err != nil {
		t.Fatalf("write_file: %v", err)
	}

	f.advance(SessionIdle + time.Minute)
	if _, err := call(t, f.ts, ToolWriteFile, maija, `{"path":"site/content/fi/toinen.md","content":"# Toinen\n"}`); err != nil {
		t.Fatalf("the write after the idle window: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.worktree("maija", "toinen"), "site", "content", "fi", "toinen.md")); err != nil {
		t.Fatalf("the edit did not land in a fresh change: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.worktree("maija", "uutinen"), "site", "content", "fi", "toinen.md")); err == nil {
		t.Fatal("the edit landed in the expired session's change")
	}

	// The fresh session's clock started at the write that opened it, so the one
	// after it is still the same conversation.
	f.advance(time.Minute)
	if _, err := call(t, f.ts, ToolWriteFile, maija, `{"path":"site/content/fi/kolmas.md","content":"# Kolmas\n"}`); err != nil {
		t.Fatalf("the third write_file: %v", err)
	}
	if got := f.slugs(t, "maija"); len(got) != 2 {
		t.Fatalf("open changes = %v, want two", got)
	}
	if _, err := os.Stat(filepath.Join(f.worktree("maija", "toinen"), "site", "content", "fi", "kolmas.md")); err != nil {
		t.Fatalf("the third edit did not continue the second change: %v", err)
	}
}

// The window is idleness and not age: an afternoon of steady work is one
// conversation however long it runs.
func TestActivityKeepsASessionAlive(t *testing.T) {
	f := newFixture(t)

	for i, args := range []string{
		`{"path":"site/content/fi/uutinen.md","content":"# Uutinen\n"}`,
		`{"path":"site/content/fi/toinen.md","content":"# Toinen\n"}`,
		`{"path":"site/content/fi/kolmas.md","content":"# Kolmas\n"}`,
	} {
		if i > 0 {
			f.advance(30 * time.Minute)
		}
		if _, err := call(t, f.ts, ToolWriteFile, maija, args); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	if got := f.slugs(t, "maija"); len(got) != 1 || got[0] != "uutinen" {
		t.Fatalf("open changes = %v, want the one the session opened", got)
	}
}

// Once a change is bound, everything serves from it: a read of a file that
// exists only in the change has to find it, or the model would be shown the
// published site as though its own edit had not happened.
func TestReadsFollowTheBoundChange(t *testing.T) {
	f := newFixture(t)

	if _, err := call(t, f.ts, ToolWriteFile, maija, `{"path":"site/content/fi/uutinen.md","content":"# Uutinen\n"}`); err != nil {
		t.Fatalf("write_file: %v", err)
	}
	out, err := call(t, f.ts, ToolReadFile, maija, `{"path":"site/content/fi/uutinen.md"}`)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	if !strings.Contains(out, "# Uutinen") {
		t.Fatalf("read_file answered:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(f.baseView(), "site", "content", "fi", "uutinen.md")); err == nil {
		t.Fatal("the edit reached the shared base view")
	}

	// Another person's conversation is untouched by it: they are still reading
	// the published site.
	if _, err := call(t, f.ts, ToolReadFile, pekka, `{"path":"site/content/fi/uutinen.md"}`); err == nil {
		t.Fatal("a second person's read found the first person's unsubmitted edit")
	}
}

// Nothing is opened implicitly any more, so a tool that needs a change and has
// none answers in words. A conversation that has not edited anything is the
// ordinary state, not a failure of the transport.
func TestToolsThatNeedAChangeSayWhenThereIsNone(t *testing.T) {
	f := newFixture(t)

	for _, tc := range []struct{ tool, args string }{
		{ToolSubmit, `{"title":"Otsikko"}`},
		{ToolGetFeedback, `{}`},
	} {
		out, err := call(t, f.ts, tc.tool, maija, tc.args)
		if err != nil {
			t.Fatalf("%s with no change open: %v", tc.tool, err)
		}
		if !strings.Contains(strings.ToLower(out), "no change is open") {
			t.Errorf("%s answered:\n%s", tc.tool, out)
		}
		if !strings.Contains(out, "resume_change") {
			t.Errorf("%s does not say how to continue an existing change:\n%s", tc.tool, out)
		}
	}
	if got := f.slugs(t, "maija"); len(got) != 0 {
		t.Fatalf("a refusal opened %v", got)
	}
}
