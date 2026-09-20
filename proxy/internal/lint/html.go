package lint

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"golang.org/x/net/html"
)

// The HTML check. It reads the pages hugo wrote and reports the two things a
// browser does not: an element that is never closed, and two elements answering
// to the same id.
//
// It is a check of nesting rather than of conformance. A browser's parser
// recovers from a missing </div> by guessing where it went, and the guess is why
// an editor's unclosed tag shows up as the whole page sliding into a card
// somewhere further down instead of as an error. Recovering silently is the
// browser's job; saying so is this one's.
//
// What it does not do is judge. Neither an attribute a validator dislikes nor a
// heading order somebody would argue about is here, because this runs on every
// page of a site it did not write, and a check that is wrong about the pages
// nobody touched is a check the model learns to scroll past.

// HTML checks every built page under output. Findings name the page by the file
// hugo wrote, which is the address the site serves it at.
func HTML(output string) []Finding {
	var c collector
	walkFiles(output, ".html", func(abs, rel string) bool {
		data, err := os.ReadFile(abs)
		if err != nil {
			// A page that cannot be read is the build's problem, not this
			// check's; the other pages are still worth reading.
			return true
		}
		for _, f := range checkPage(rel, data) {
			c.add(f)
		}
		return !c.full()
	})
	return c.out
}

// element is one open tag and the line it opened on, which is the line worth
// naming: a person fixing an unclosed div needs where it started.
type element struct {
	name string
	line int
}

// checkPage walks one page's tokens, keeping the stack of open elements.
//
// The tokenizer is used rather than the parser because the parser's answer is
// the tree a browser would build, mistakes repaired and gone. What is wanted here
// is the repair itself.
func checkPage(where string, data []byte) []Finding {
	var out []Finding
	z := html.NewTokenizer(bytes.NewReader(data))
	var stack []element
	ids := make(map[string]int)
	line := 1

	for {
		tt := z.Next()
		// Raw is the source of the token just read, so the token began on the
		// line the count stood at before it.
		at := line
		line += bytes.Count(z.Raw(), []byte{'\n'})

		switch tt {
		case html.ErrorToken:
			// Every error the tokenizer reports is the end of the input: it
			// recovers from everything else. What is left on the stack is what
			// the page never closed, outermost first, because the outermost one
			// is usually the mistake and the rest are its consequence.
			for _, e := range stack {
				if mustClose(e.name) {
					out = append(out, Finding{Where: where, Line: e.line, Text: unclosed(e.name)})
				}
			}
			return out

		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if hasAttr {
				if dup, at, ok := duplicateID(z, ids, at); ok {
					out = append(out, Finding{Where: where, Line: at,
						Text: fmt.Sprintf("id=%q is on two elements; a link, a label or a script that names it reaches only the first", dup)})
				}
			}
			if tt == html.StartTagToken && !void(tag) {
				stack = append(stack, element{name: tag, line: at})
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			i := indexOf(stack, tag)
			if i < 0 {
				if mustClose(tag) {
					out = append(out, Finding{Where: where, Line: at,
						Text: fmt.Sprintf("</%s> closes nothing: there is no <%s> open here", tag, tag)})
				}
				continue
			}
			// Everything above the match is closed by this tag whether it said so
			// or not. For the elements whose end tag is optional that is the
			// language working as designed; for the rest it is a missing tag.
			//
			// Reaching </body> or </html> with something still open is the page
			// ending, not two tags in the wrong order, and it reads as the same
			// finding as reaching the end of the file: one mistake, one sentence.
			for _, e := range stack[i+1:] {
				if !mustClose(e.name) {
					continue
				}
				text := fmt.Sprintf("<%s> is still open where </%s> closes, so one of the two is misplaced", e.name, tag)
				if tag == "body" || tag == "html" {
					text = unclosed(e.name)
				}
				out = append(out, Finding{Where: where, Line: e.line, Text: text})
			}
			stack = stack[:i]
		}
	}
}

// unclosed is the one sentence for an element the page never closes, whether
// that shows at </body> or at the end of the file. One wording, so one mistake
// is one finding however the page happens to end.
func unclosed(name string) string {
	return fmt.Sprintf("<%s> is never closed, so the browser guesses where it ends", name)
}

// duplicateID reports an id this page has already used. ids remembers the line
// each one was first seen on, so the message can be about the second.
func duplicateID(z *html.Tokenizer, ids map[string]int, at int) (string, int, bool) {
	for {
		key, val, more := z.TagAttr()
		if string(key) == "id" {
			id := strings.TrimSpace(string(val))
			if id != "" {
				if _, seen := ids[id]; seen {
					return id, at, true
				}
				ids[id] = at
			}
		}
		if !more {
			return "", 0, false
		}
	}
}

// indexOf is the innermost open element with this name, or -1.
func indexOf(stack []element, name string) int {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].name == name {
			return i
		}
	}
	return -1
}

// voidElements have no content and no end tag. A </br> is not a mistake worth
// anybody's turn, so they are silent here in both directions.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// optionalEnd are the elements HTML5 lets a document leave open: a list item
// ends where the next one begins, and a paragraph ends where a block does.
// Templates all over this site rely on that, correctly, so an unclosed one of
// these is not a finding.
//
// html, head and body are here too. A browser supplies them whether the
// document does or not, and a page that omits one is not broken by omitting it.
var optionalEnd = map[string]bool{
	"p": true, "li": true, "dt": true, "dd": true, "option": true,
	"optgroup": true, "thead": true, "tbody": true, "tfoot": true, "tr": true,
	"td": true, "th": true, "caption": true, "colgroup": true, "rt": true,
	"rp": true, "html": true, "head": true, "body": true,
}

func void(name string) bool { return voidElements[name] }

// mustClose is whether leaving this element open is worth reporting.
func mustClose(name string) bool { return !voidElements[name] && !optionalEnd[name] }
