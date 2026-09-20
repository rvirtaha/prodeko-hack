package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The CSS check. It reads the stylesheets an editor may write and reports the
// three ways one stops being a stylesheet: a comment that never closes, a string
// that never closes, and a brace that does not match.
//
// This matters more than it sounds. A browser recovers from a broken stylesheet
// by discarding declarations until it finds its footing again, silently and
// sometimes generously: a missing brace two hundred lines up can turn the rest of
// the file off without anything anywhere reporting an error. Hugo bundles the
// file as it finds it and says nothing either. Nothing in the loop catches it —
// not the build, and not a screenshot of the one page the editor was looking at,
// which may not be the page the discarded rules were for.
//
// Only unambiguous syntax is here. Whether a colour should have been a token is a
// judgement the conventions make and a reviewer enforces; this is the part a
// machine can be certain about.

// CSSRoot is the stylesheet tree, the one the fence lets an editor write.
const CSSRoot = "site/assets/css"

// CSS checks every stylesheet under the worktree's CSS root. Findings name the
// file the way every tool names it, relative to the repository root, so the path
// in the finding is the path to pass to read_file.
func CSS(worktree string) []Finding {
	var c collector
	root := filepath.Join(worktree, filepath.FromSlash(CSSRoot))
	walkFiles(root, ".css", func(abs, rel string) bool {
		data, err := os.ReadFile(abs)
		if err != nil {
			return true
		}
		where := CSSRoot + "/" + rel
		for _, f := range checkStylesheet(where, data) {
			c.add(f)
		}
		return !c.full()
	})
	return c.out
}

// checkStylesheet scans one file character by character, tracking the three
// things that nest: comments, strings and blocks.
//
// Character by character rather than line by line, because every one of these
// mistakes is the absence of something on a later line than the thing it belongs
// to, and because a brace inside a string or a comment is not a brace.
func checkStylesheet(where string, data []byte) []Finding {
	var out []Finding
	var blocks []int // the line each open brace is on
	src := string(data)
	line := 1

	for i := 0; i < len(src); i++ {
		switch {
		case src[i] == '\n':
			line++

		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				out = append(out, Finding{Where: where, Line: line,
					Text: "a comment opens here and is never closed, so the rest of the file is inside it"})
				return out
			}
			// Comments span lines freely; the ones they span still count.
			line += strings.Count(src[i:i+2+end+2], "\n")
			i += 2 + end + 1

		case src[i] == '"' || src[i] == '\'':
			end, ok := endOfString(src[i:])
			if !ok {
				out = append(out, Finding{Where: where, Line: line,
					Text: fmt.Sprintf("a %c quote opens here and is not closed on this line; a CSS string cannot span lines", src[i])})
				// The scan goes on from after the quote: one unterminated string
				// is a mistake, not a reason to stop reading the file.
				continue
			}
			i += end

		case src[i] == '{':
			blocks = append(blocks, line)

		case src[i] == '}':
			if len(blocks) == 0 {
				out = append(out, Finding{Where: where, Line: line,
					Text: "a } here closes nothing; everything after it is read as a selector"})
				continue
			}
			blocks = blocks[:len(blocks)-1]
		}
	}

	for _, at := range blocks {
		out = append(out, Finding{Where: where, Line: at,
			Text: "a { opens here and is never closed, so the declarations after it are dropped"})
	}
	return out
}

// endOfString is the offset of the closing quote, counted from the opening one.
// A backslash escapes the next character, and a newline ends the search: CSS
// strings do not span lines, and treating one that seems to as a string would
// swallow the rest of the file.
func endOfString(src string) (int, bool) {
	quote := src[0]
	for i := 1; i < len(src); i++ {
		switch src[i] {
		case '\\':
			i++
		case '\n':
			return 0, false
		case quote:
			return i, true
		}
	}
	return 0, false
}
