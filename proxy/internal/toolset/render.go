package toolset

import (
	"fmt"
	"strings"
	"time"

	"github.com/prodeko/prodeko-hack/proxy/internal/workdir"
)

// What a tool returns is prose the model reads, so it is written for that
// reader: the result first, then what to do next where there is something to
// do. Nothing here interprets hugo's or git's output; a build error and a
// diffstat are quoted exactly as they came.

// maxMatchLine bounds one line of a search result. A stylesheet has no long
// lines, but a data file can carry one, and five hundred of them would fill
// the model's context with something it did not ask for.
const maxMatchLine = 300

func renderFiles(files []string, glob string) string {
	if len(files) == 0 {
		if glob != "" {
			return fmt.Sprintf("No editable file matches %q.", glob)
		}
		return "The editable tree is empty."
	}
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\n%d files", len(files))
	if glob != "" {
		fmt.Fprintf(&b, " matching %q", glob)
	}
	b.WriteByte('.')
	return b.String()
}

func renderMatches(matches []workdir.Match, pattern string, capped int) string {
	if len(matches) == 0 {
		return fmt.Sprintf("No line matches %s.", pattern)
	}
	var b strings.Builder
	for _, m := range matches {
		text := m.Text
		if len(text) > maxMatchLine {
			text = text[:maxMatchLine] + " ..."
		}
		fmt.Fprintf(&b, "%s:%d: %s\n", m.Path, m.Line, text)
	}
	if len(matches) >= capped {
		fmt.Fprintf(&b, "\nStopped at %d matches. Narrow the pattern or pass a glob.", capped)
	}
	return b.String()
}

func renderBuild(res workdir.Result) string {
	out := strings.TrimRight(res.Output, "\n")
	if res.OK {
		if out == "" {
			return fmt.Sprintf("Build OK in %s.", res.Duration.Round(time.Millisecond))
		}
		return fmt.Sprintf("Build OK in %s.\n\n%s", res.Duration.Round(time.Millisecond), out)
	}
	if out == "" {
		out = "(the build failed and said nothing)"
	}
	return fmt.Sprintf("Build failed after %s. The output is verbatim:\n\n%s",
		res.Duration.Round(time.Millisecond), out)
}

func renderSubmit(res workdir.SubmitResult) string {
	var b strings.Builder
	// A dry run says what it did once, in the note the workdir wrote; a second
	// headline here would be a second, differently worded claim about the same
	// push.
	if res.PRNumber > 0 {
		fmt.Fprintf(&b, "Draft pull request #%d: %s\n", res.PRNumber, res.PRURL)
		if res.PreviewURL != "" {
			fmt.Fprintf(&b, "Preview: %s (ready in about a minute)\n", res.PreviewURL)
		}
	} else if !res.DryRun {
		b.WriteString("Committed and pushed.\n")
	}
	if res.NothingNew {
		b.WriteString("Nothing had changed since the last submit, so no new commit was made.\n")
	}
	fmt.Fprintf(&b, "Branch: %s\n", res.Branch)
	if res.Commit != "" {
		fmt.Fprintf(&b, "Commit: %s\n", res.Commit)
	}
	if len(res.Files) > 0 {
		b.WriteString("Files:\n")
		for _, f := range res.Files {
			fmt.Fprintf(&b, "  %s\n", f)
		}
	}
	if d := strings.TrimRight(res.Diffstat, "\n"); d != "" {
		fmt.Fprintf(&b, "%s\n", d)
	}
	if res.Note != "" {
		fmt.Fprintf(&b, "%s\n", res.Note)
	}
	b.WriteString("\nAsking for another change continues this branch and the same pull request.")
	return b.String()
}

func renderChanges(infos []workdir.Info) string {
	if len(infos) == 0 {
		return "You have no open changes. The first edit starts one."
	}
	var b strings.Builder
	for i, in := range infos {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s\n  branch: %s\n", in.Slug, in.Branch)
		if !in.UpdatedAt.IsZero() {
			fmt.Fprintf(&b, "  updated: %s\n", in.UpdatedAt.Format(time.RFC3339))
		}
		if len(in.Files) > 0 {
			fmt.Fprintf(&b, "  files: %s\n", strings.Join(in.Files, ", "))
		}
		// A change with no pull request is not necessarily unsubmitted: in a dry
		// run there is never one to find, and saying "not submitted yet" about
		// a change that is committed and pushed would send the editor back to
		// submit a second time. Files with a clean worktree is what "committed"
		// looks like from here.
		switch {
		case in.PRNumber > 0:
			fmt.Fprintf(&b, "  pull request: #%d %s\n", in.PRNumber, in.PRURL)
			if in.CIState != "" {
				fmt.Fprintf(&b, "  checks: %s\n", in.CIState)
			}
			if in.PreviewURL != "" {
				fmt.Fprintf(&b, "  preview: %s\n", in.PreviewURL)
			}
		case in.Dirty || len(in.Files) == 0:
			b.WriteString("  not submitted yet\n")
		default:
			b.WriteString("  committed to the branch, with no pull request\n")
		}
		if in.Dirty && len(in.Files) > 0 {
			b.WriteString("  has edits that were never submitted\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
