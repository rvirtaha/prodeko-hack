package workdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// What has been done to a change, and when. Three moments answer everything
// submit needs to know: when it was last edited, when it last built cleanly, and
// when anybody last looked at the result.
//
// They are files on the state volume rather than fields on the Change, because a
// worktree outlives the process. A restart must not turn "already built" into
// "never built", nor the other way round: the first would send an editor round
// the loop again for nothing, and the second would let an unbuilt change through
// the gate the whole loop exists for.

// Stamps is what the submit gate reads. A zero time means it never happened, and
// that is also what an unreadable stamp reads as: the safe answer to "has this
// been built" and "has anybody looked at it" is no.
type Stamps struct {
	EditedAt time.Time
	BuiltAt  time.Time
	ShotAt   time.Time
}

// Stamps reads all three at once, which is how the gate wants them: the
// comparisons are between them, and reading them one tool call apart would
// compare two different moments.
func (c *Change) Stamps() Stamps {
	return Stamps{
		EditedAt: c.readStamp(editStamp),
		BuiltAt:  c.readStamp(buildStamp),
		ShotAt:   c.ScreenshotAt(),
	}
}

// The stamp file names. They sit beside the build output, under previewRoot.
const (
	editStamp       = "edit.at"
	buildStamp      = "build.at"
	screenshotStamp = "screenshot.at"
)

// markEdit records that the change was written to at t. Every write goes through
// it, and it is unexported because nothing outside this package writes files in
// a change.
func (c *Change) markEdit(t time.Time) error {
	return c.mark(editStamp, "the edit", t)
}

// markBuild records a build that passed at t. A build that failed leaves the
// stamp alone: "built since the last edit" has to mean the site still builds,
// and a failing build is the one thing the gate exists to keep out of a pull
// request.
func (c *Change) markBuild(t time.Time) error {
	return c.mark(buildStamp, "the build", t)
}

// MarkScreenshot records that a screenshot of this change was taken at t. It is
// exported because screenshot is a tool of the tool layer rather than an
// operation of this one: the picture is taken from the build output by the
// preview package, and this is where the fact of it is kept.
func (c *Change) MarkScreenshot(t time.Time) error {
	return c.mark(screenshotStamp, "the screenshot", t)
}

// ScreenshotAt is when this change was last screenshotted, or the zero time when
// it never was.
func (c *Change) ScreenshotAt() time.Time { return c.readStamp(screenshotStamp) }

// mark writes one stamp. what names the thing being recorded, so a failure says
// which piece of bookkeeping could not be done.
func (c *Change) mark(name, what string, t time.Time) error {
	if err := os.MkdirAll(c.previewRoot(), 0o755); err != nil {
		return fmt.Errorf("workdir: making the preview directory: %w", err)
	}
	stamp := []byte(t.UTC().Format(time.RFC3339Nano) + "\n")
	if err := os.WriteFile(filepath.Join(c.previewRoot(), name), stamp, 0o644); err != nil {
		return fmt.Errorf("workdir: recording %s: %w", what, err)
	}
	return nil
}

// readStamp is one stamp, or the zero time. An unreadable or unparsable file is
// the zero time as well: a stamp nobody can read is not evidence that anything
// happened.
func (c *Change) readStamp(name string) time.Time {
	data, err := os.ReadFile(filepath.Join(c.previewRoot(), name))
	if err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}
	}
	return t
}
