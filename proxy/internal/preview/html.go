package preview

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// ErrNoMatch is a selector that matches nothing on the page. It is not an empty
// answer: a selector that misses usually means the element is named something
// else, and the model has to be told that rather than shown nothing.
var ErrNoMatch = errors.New("preview: nothing on the page matches the selector")

// HTML is the built page: the file as hugo wrote it, or the subtrees a CSS
// selector matches.
//
// Whole pages come back byte for byte, with nothing reformatted, because what a
// template produced is the thing being examined. A selector goes through a real
// HTML parser and a real selector engine: the answer has to be the subtree a
// browser would style, and a regular expression over generated markup agrees
// with the browser right up until it does not.
func (p Page) HTML(selector string) (string, error) {
	data, err := os.ReadFile(p.File)
	if err != nil {
		return "", fmt.Errorf("preview: reading the built %s: %w", p.URL, err)
	}
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return string(data), nil
	}

	sel, err := cascadia.Compile(selector)
	if err != nil {
		return "", fmt.Errorf("preview: %q is not a usable CSS selector: %w", selector, err)
	}
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("preview: parsing the built %s: %w", p.URL, err)
	}
	nodes := cascadia.QueryAll(doc, sel)
	if len(nodes) == 0 {
		return "", fmt.Errorf("%w: %s has nothing matching %q", ErrNoMatch, p.URL, selector)
	}

	var b strings.Builder
	for i, n := range nodes {
		if i > 0 {
			b.WriteByte('\n')
		}
		if err := html.Render(&b, n); err != nil {
			return "", fmt.Errorf("preview: rendering the match for %q: %w", selector, err)
		}
	}
	return b.String(), nil
}
