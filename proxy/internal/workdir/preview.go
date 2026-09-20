package workdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Where a change's build output lives, and the bookkeeping that records what has
// been done to it.
//
// The output does not belong in the worktree. The fence would refuse to commit
// it, git status would carry twelve megabytes of it through every submit, and a
// public/ inside a tree the tools can write to is the one place a media person
// could fill the state volume twice over. It lives beside the worktree on the
// state volume instead, one directory per change, which is what render reads and
// what screenshot serves.

// previewRoot is the per-change directory on the state volume: the last build's
// output, and the stamps that say what has happened to the change since.
// Everything under it is reconstructible by building again, so nothing under it
// is backed up and nothing under it is ever committed.
func (c *Change) previewRoot() string {
	return filepath.Join(c.mgr.cfg.StateDir, "preview", c.User, c.Slug)
}

// BuildRoot is where hugo builds: public/ beside public-members/, which is the
// shape check-trees.sh expects to find them in.
func (c *Change) BuildRoot() string { return filepath.Join(c.previewRoot(), "build") }

// Output is the built public site as of this change's last build. It is what
// render reads and what screenshot serves; until a build has run there is
// nothing there, which is what [Change.Built] answers.
func (c *Change) Output() string { return filepath.Join(c.BuildRoot(), "public") }

// Built reports whether there is output to look at. The build root is emptied at
// the start of every build, so the directory exists exactly when hugo has
// written into it — a failed build leaves nothing behind to be mistaken for the
// current site.
func (c *Change) Built() bool { return dirExists(c.Output()) }

// screenshotStamp is where the time of the last screenshot is kept.
func (c *Change) screenshotStamp() string { return filepath.Join(c.previewRoot(), "screenshot.at") }

// MarkScreenshot records that a screenshot of this change was taken at t. It is
// the bookkeeping half of the guardrail on layout changes: submit compares this
// against the time of the last edit and refuses a template change nobody has
// looked at.
//
// It is a file rather than a field, because a worktree outlives the process and
// a restart must not turn "already seen" into "never seen" or the other way
// round.
func (c *Change) MarkScreenshot(t time.Time) error {
	if err := os.MkdirAll(c.previewRoot(), 0o755); err != nil {
		return fmt.Errorf("workdir: making the preview directory: %w", err)
	}
	stamp := []byte(t.UTC().Format(time.RFC3339Nano) + "\n")
	if err := os.WriteFile(c.screenshotStamp(), stamp, 0o644); err != nil {
		return fmt.Errorf("workdir: recording the screenshot: %w", err)
	}
	return nil
}

// ScreenshotAt is when this change was last screenshotted, or the zero time when
// it never was. An unreadable or unparsable stamp is the zero time too: the
// safe answer to "has anybody looked at this" is no.
func (c *Change) ScreenshotAt() time.Time {
	data, err := os.ReadFile(c.screenshotStamp())
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}
	}
	return t
}
