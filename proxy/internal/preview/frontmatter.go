package preview

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// FrontMatter is the part of a page's front matter this package needs: the
// translation pairing, and the two keys that can move a page's address.
type FrontMatter struct {
	TranslationKey string
	Slug           string
	URL            string
}

// frontMatter reads the YAML block a page opens with.
//
// It is a line scanner and not a YAML parser. The three keys it looks for are
// top-level scalars in every page of this site, a dependency that understood the
// rest of YAML would earn nothing here, and the file is content the fence lets
// the editor write — so what this must never do is more than look.
//
// A page with no front matter at all is not an error; it has no translation key
// and no address of its own, which is a fact and not a failure. TOML front
// matter is an error, because reading +++ as YAML would quietly return nothing
// and a missing counterpart would then read as a missing translation.
func frontMatter(path string) (FrontMatter, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return FrontMatter{}, fmt.Errorf("%w: no content file at %s", ErrNoPage, path)
	}
	if err != nil {
		return FrontMatter{}, fmt.Errorf("preview: reading front matter: %w", err)
	}
	defer f.Close()

	var fm FrontMatter
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)

	if !sc.Scan() {
		return fm, nil
	}
	switch strings.TrimSpace(sc.Text()) {
	case "---":
	case "+++":
		return fm, fmt.Errorf("preview: %s opens with TOML front matter, which this does not read", path)
	default:
		return fm, nil
	}

	for sc.Scan() {
		line := sc.Text()
		if t := strings.TrimSpace(line); t == "---" || t == "..." {
			break
		}
		// A line that begins with whitespace belongs to the value above it. The
		// keys wanted here are top-level, and a nested "slug:" under some other
		// mapping is not this page's slug.
		if line != strings.TrimLeft(line, " \t") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "translationKey":
			fm.TranslationKey = scalar(value)
		case "slug":
			fm.Slug = scalar(value)
		case "url":
			fm.URL = scalar(value)
		}
	}
	if err := sc.Err(); err != nil {
		return FrontMatter{}, fmt.Errorf("preview: reading front matter of %s: %w", path, err)
	}
	return fm, nil
}

// scalar is a YAML scalar as far as this needs one: trimmed, unquoted, and
// without the comment a key may carry after it.
func scalar(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}
