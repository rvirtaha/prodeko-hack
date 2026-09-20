// Package lint is what build notices about a change besides whether hugo ran:
// the markup the templates produced, and the stylesheets the editor wrote.
//
// Hugo answers one question — does the site build — and a template that builds
// can still emit a div that is never closed, two elements with the same id, or a
// stylesheet with an unbalanced brace that every browser silently discards half
// of. Those are exactly the mistakes a media person makes in a partial, and
// exactly the ones neither hugo nor a screenshot of the edited page shows.
//
// Both checks are Go, with no binary to install and nothing fetched at build
// time: an editor's build runs in the same container as the OAuth endpoints, and
// a validator that wants a JVM or a network round trip is a validator that
// breaks the server it runs in. What they look for is narrow on purpose. A
// finding has to be a mistake, not a matter of taste, because a check that cries
// wolf on every page of the site is a check the model learns to skip.
package lint

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// MaxFindings bounds what one check reports. A single unclosed tag in a partial
// is a finding on every page that uses it; the report is for a person to act on,
// not an inventory.
const MaxFindings = 20

// Finding is one thing a check noticed, named the way the file it is in is
// named: a built page by its address, a stylesheet by its repository path.
type Finding struct {
	Where string
	Line  int
	Text  string
}

// String is the form every compiler and every linter has printed since 1978,
// because it is the form a model and a maintainer both already read.
func (f Finding) String() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", f.Where, f.Line, f.Text)
	}
	return fmt.Sprintf("%s: %s", f.Where, f.Text)
}

// collector gathers findings under the cap and drops the repeats.
//
// The same mistake in a partial shows on every page that renders it, and thirty
// copies of one finding are still one fix. The first page to carry it is the one
// reported, because it is the one somebody can look at.
type collector struct {
	out  []Finding
	seen map[string]bool
}

func (c *collector) add(f Finding) {
	if c.seen == nil {
		c.seen = make(map[string]bool)
	}
	if c.seen[f.Text] || len(c.out) >= MaxFindings {
		return
	}
	c.seen[f.Text] = true
	c.out = append(c.out, f)
}

func (c *collector) full() bool { return len(c.out) >= MaxFindings }

// walkFiles visits every file under root whose name ends in ext, naming each one
// relative to root, and stops early when visit answers false.
//
// The order is WalkDir's own, which is lexical: the same build reports the same
// findings in the same order, and a cap that cut the list in a different place
// every time would be worse than no cap.
//
// A root that is not there is nothing to check. A build that produced no output
// is reported by the build; a lint of it has nothing to add.
func walkFiles(root, ext string, visit func(abs, rel string) bool) {
	stop := fs.SkipAll
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory is not a finding about the site. The walk
			// goes on: half a check is worth more here than none.
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ext) {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		if !visit(p, filepath.ToSlash(rel)) {
			return stop
		}
		return nil
	})
}
