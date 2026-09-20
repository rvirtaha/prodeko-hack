package workdir

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// render and screenshot read what build produced, so the output has to outlive
// the build that made it — and it has to live outside the worktree, or submit
// would carry the whole site into the commit.
func TestBuildKeepsItsOutputOutsideTheWorktree(t *testing.T) {
	if _, err := exec.LookPath("hugo"); err != nil {
		t.Skip("hugo is not on PATH")
	}
	f := newFixture(t)
	m := f.manager(t)
	c := openChange(t, m)

	if c.Built() {
		t.Fatal("a change reports output before anything was built")
	}
	res, err := m.Build(t.Context(), c)
	if err != nil || !res.OK {
		t.Fatalf("Build = %+v, %v", res, err)
	}
	if !c.Built() {
		t.Fatalf("no output at %s after a clean build", c.Output())
	}
	page := filepath.Join(c.Output(), "fi", "tapahtumat", "index.html")
	if _, err := os.Stat(page); err != nil {
		t.Fatalf("the built page is not where render looks for it: %v", err)
	}
	if rel, err := filepath.Rel(c.Dir, c.Output()); err == nil && !filepath.IsAbs(rel) && rel[0] != '.' {
		t.Fatalf("the output at %s is inside the worktree %s", c.Output(), c.Dir)
	}

	// A page the editor deleted has to leave the output with it, or render would
	// answer out of a file the site no longer has.
	if err := os.Remove(filepath.Join(c.Dir, "site", "content", "fi", "tapahtumat.md")); err != nil {
		t.Fatalf("removing the page: %v", err)
	}
	if res, err := m.Build(t.Context(), c); err != nil || !res.OK {
		t.Fatalf("second Build = %+v, %v", res, err)
	}
	if _, err := os.Stat(page); !os.IsNotExist(err) {
		t.Fatalf("the deleted page is still in the output: %v", err)
	}
}

// The gate submit applies is "a screenshot since the last edit", and a worktree
// outlives the process, so the stamp is read off disk rather than remembered.
func TestScreenshotStampIsReadOffDisk(t *testing.T) {
	f := newFixture(t)
	c := openChange(t, f.manager(t))

	if at := c.ScreenshotAt(); !at.IsZero() {
		t.Fatalf("a change nobody screenshotted reports %s", at)
	}

	want := time.Date(2026, 9, 20, 11, 4, 25, 0, time.UTC)
	if err := c.MarkScreenshot(want); err != nil {
		t.Fatalf("MarkScreenshot: %v", err)
	}
	if got := c.ScreenshotAt(); !got.Equal(want) {
		t.Fatalf("ScreenshotAt = %s, want %s", got, want)
	}

	// A second manager is the restart: the same user and slug find the same
	// stamp, because nothing about it lives in this process.
	other := f.manager(t)
	again, err := other.Change(c.User, c.Slug)
	if err != nil {
		t.Fatalf("Change: %v", err)
	}
	if got := again.ScreenshotAt(); !got.Equal(want) {
		t.Fatalf("after a restart ScreenshotAt = %s, want %s", got, want)
	}
}

// Building is not looking. The stamp says whether anybody has seen the change
// rendered, and clearing the output must not clear that answer.
func TestBuildLeavesTheScreenshotStampAlone(t *testing.T) {
	if _, err := exec.LookPath("hugo"); err != nil {
		t.Skip("hugo is not on PATH")
	}
	f := newFixture(t)
	m := f.manager(t)
	c := openChange(t, m)

	want := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	if err := c.MarkScreenshot(want); err != nil {
		t.Fatalf("MarkScreenshot: %v", err)
	}
	if res, err := m.Build(t.Context(), c); err != nil || !res.OK {
		t.Fatalf("Build = %+v, %v", res, err)
	}
	if got := c.ScreenshotAt(); !got.Equal(want) {
		t.Fatalf("after a build ScreenshotAt = %s, want %s", got, want)
	}
}

// An unreadable stamp answers "nobody has looked at this", because that is the
// answer that makes submit ask for a screenshot rather than skip one.
func TestAnUnparsableScreenshotStampReadsAsNever(t *testing.T) {
	f := newFixture(t)
	c := openChange(t, f.manager(t))

	mustWrite(t, c.screenshotStamp(), "viime viikolla\n")
	if at := c.ScreenshotAt(); !at.IsZero() {
		t.Fatalf("an unparsable stamp reads as %s, want the zero time", at)
	}
}
