// Package preview is how the model sees what it edited: the built HTML of one
// page, and a picture of it.
//
// Everything here reads the output of the change's last build. Nothing here runs
// hugo — build is the tool that does that, and a preview that built its own copy
// would be answering about a site the editor never validated.
//
// The pictures are taken over HTTP and not off the filesystem, because hugo's
// output assumes a server: the stylesheet, the fonts and every link are absolute
// paths from the site root, and file:// resolves those against the filesystem
// root, where none of them exist.
package preview

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ContentRoot is where the pages this can show live, spelled the way the fence
// and every tool spell it.
const ContentRoot = "site/content/"

// MembersRoot is the other content root. Its pages build into a tree of their
// own that is never served publicly, and it is not what this shows.
const MembersRoot = "site/content-members/"

// Refusals. They are errors rather than empty answers, because "which page?" and
// "build first" are two different things for the model to do next.
var (
	ErrNotBuilt = errors.New("preview: this change has not been built yet")
	ErrNoPage   = errors.New("preview: no such page in the build output")
	ErrBadPath  = errors.New("preview: not a page of this site")
)

// Site is one change as this package sees it: the tree it is edited in and the
// output of its last build.
type Site struct {
	// Worktree is the repository root, the directory site/ lives in. Front
	// matter is read from here, because the pairing between a Finnish page and
	// its English one is content and the addresses are not.
	Worktree string

	// Output is the built public tree: the directory the site's root maps to.
	Output string
}

// Page is one page of the built site.
type Page struct {
	Lang string // the content root it lives under: "fi", "en"
	URL  string // the address the site serves it at, "/fi/tapahtumat/"
	File string // absolute path of the built HTML

	// Content is the repository-relative content file, empty when the caller
	// asked by address and no file of that name is there to be found.
	Content string

	// TranslationKey pairs this page with the same page in another language. It
	// is empty when the front matter carries none, which is what "this page has
	// no counterpart" looks like.
	TranslationKey string
}

// Locate resolves what the model asked for to a built page.
//
// It takes either the content file the model edited,
// "site/content/fi/tapahtumat.md", or the address the site serves,
// "/fi/tapahtumat/". Those are the two things the model has in hand, and asking
// it to convert between them is asking it to guess: a page's address is front
// matter's to move, and only hugo's output settles it.
func (s Site) Locate(arg string) (Page, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return Page{}, fmt.Errorf("%w: no page was named", ErrBadPath)
	}
	if err := legible(arg); err != nil {
		return Page{}, err
	}
	if !dirExists(s.Output) {
		return Page{}, ErrNotBuilt
	}

	rel := strings.TrimPrefix(strings.TrimPrefix(arg, "./"), "/")
	switch {
	case strings.HasPrefix(rel, MembersRoot):
		return Page{}, fmt.Errorf("%w: %s builds into the member tree, which is not served here", ErrBadPath, rel)
	case strings.HasPrefix(rel, ContentRoot):
		return s.pageOfContent(rel)
	case strings.HasSuffix(rel, ".md"):
		// A file name is a content path spelled short, and the site serves no
		// address ending in .md. Naming the root it is missing is the whole fix.
		return Page{}, fmt.Errorf("%w: %s is a file name; content files are named from the repository root, %s...",
			ErrBadPath, rel, ContentRoot)
	default:
		return s.pageOfAddress(arg)
	}
}

// pageOfContent is a page named by the file the model edited.
func (s Site) pageOfContent(rel string) (Page, error) {
	lang, under, ok := strings.Cut(strings.TrimPrefix(rel, ContentRoot), "/")
	if !ok || lang == "" || under == "" {
		return Page{}, fmt.Errorf("%w: %s names no page under a language directory", ErrBadPath, rel)
	}
	abs := filepath.Join(s.Worktree, filepath.FromSlash(rel))
	fm, err := frontMatter(abs)
	if err != nil {
		return Page{}, err
	}

	page := Page{Lang: lang, Content: rel, URL: address(lang, under, fm), TranslationKey: fm.TranslationKey}
	if page.File, err = s.builtFile(page.URL); err != nil {
		return Page{}, err
	}
	return page, nil
}

// pageOfAddress is a page named by the address the site serves it at. The
// content file is looked for afterwards and is allowed not to be there: an
// address that resolves in the output is a page whether or not this can name the
// file that produced it, and only the translation pairing depends on the file.
func (s Site) pageOfAddress(arg string) (Page, error) {
	url := canonicalAddress(arg)
	file, err := s.builtFile(url)
	if err != nil {
		return Page{}, err
	}
	page := Page{Lang: firstSegment(url), URL: url, File: file}
	if rel, fm, ok := s.contentOf(page.Lang, url); ok {
		page.Content, page.TranslationKey = rel, fm.TranslationKey
	}
	return page, nil
}

// Counterparts is the same page in the site's other languages, which is what
// makes one screenshot call cover both. Two files are one page when their front
// matter shares a translationKey; their addresses differ, which is the point of
// the key.
//
// The other language's content root is walked rather than its address guessed:
// "tapahtumat" is "events" in English, and nothing about the Finnish path says
// so. A counterpart whose page is not in the output is not returned — an
// unbuilt language is not something to report as a missing translation.
func (s Site) Counterparts(p Page) ([]Page, error) {
	if p.TranslationKey == "" {
		return nil, nil
	}
	langs, err := s.languages()
	if err != nil {
		return nil, err
	}
	var out []Page
	for _, lang := range langs {
		if lang == p.Lang {
			continue
		}
		rel, ok, err := s.pageWithKey(lang, p.TranslationKey)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		other, err := s.pageOfContent(rel)
		if errors.Is(err, ErrNoPage) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, other)
	}
	return out, nil
}

// languages is the content roots there are, which are the directories under
// site/content. It is read off the tree rather than out of hugo.toml: the
// configuration is outside the fence, and the directories are the same fact.
func (s Site) languages() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Worktree, filepath.FromSlash(ContentRoot)))
	if err != nil {
		return nil, fmt.Errorf("preview: reading the content roots: %w", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// pageWithKey is the page in one language whose front matter carries key. The
// first match wins: two pages of one language sharing a translationKey is a
// content mistake, and picking one of them is no worse than refusing both.
func (s Site) pageWithKey(lang, key string) (string, bool, error) {
	root := filepath.Join(s.Worktree, filepath.FromSlash(ContentRoot), lang)
	var found string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !isPageFile(d.Name()) {
			return nil
		}
		fm, err := frontMatter(p)
		if err != nil || fm.TranslationKey != key {
			// A page this cannot read is not a page that pairs with anything.
			// Front matter is the editor's to break, and a broken one is
			// reported by build, not by a missing screenshot.
			return nil
		}
		rel, err := filepath.Rel(s.Worktree, p)
		if err != nil {
			return nil
		}
		found = filepath.ToSlash(rel)
		return fs.SkipAll
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", false, fmt.Errorf("preview: looking for the %s counterpart: %w", lang, err)
	}
	return found, found != "", nil
}

// contentOf is the content file an address came from, found by trying the names
// hugo would have built it from. It is best effort by design: a page whose front
// matter moved its address is found by [Site.pageOfContent] and not by this.
func (s Site) contentOf(lang, url string) (string, FrontMatter, bool) {
	under := strings.Trim(strings.TrimPrefix(strings.Trim(url, "/"), lang), "/")
	stem := path.Join(ContentRoot, lang, under)
	for _, name := range candidateContentFiles(stem) {
		abs := filepath.Join(s.Worktree, filepath.FromSlash(name))
		if !fileExists(abs) {
			continue
		}
		fm, err := frontMatter(abs)
		if err != nil {
			continue
		}
		return name, fm, true
	}
	return "", FrontMatter{}, false
}

// candidateContentFiles is the files hugo builds one address from: the page
// itself, and the two names a section index goes by.
func candidateContentFiles(stem string) []string {
	stem = strings.TrimSuffix(stem, "/")
	return []string{
		stem + ".md",
		stem + "/_index.md",
		stem + "/index.md",
		stem + ".html",
		stem + "/_index.html",
	}
}

func isPageFile(name string) bool {
	return strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".html")
}

// address is where hugo serves a content file, which is its path with the
// extension dropped, an index file naming its own directory, and a trailing
// slash. Front matter overrides it two ways, and both are honoured here: url
// replaces the address outright, slug replaces its last segment.
func address(lang, under string, fm FrontMatter) string {
	if fm.URL != "" {
		return canonicalAddress(fm.URL)
	}
	under = strings.TrimSuffix(under, path.Ext(under))
	if base := path.Base(under); base == "_index" || base == "index" {
		under = path.Dir(under)
		if under == "." {
			under = ""
		}
	}
	if fm.Slug != "" {
		under = path.Join(path.Dir(under), fm.Slug)
	}
	return canonicalAddress("/" + path.Join(lang, under))
}

// canonicalAddress is one spelling of an address: absolute, no index.html, and a
// trailing slash unless it names a file. Hugo serves pretty URLs, so
// "/fi/tapahtumat", "fi/tapahtumat/" and "/fi/tapahtumat/index.html" are the
// same page asked for three ways.
func canonicalAddress(url string) string {
	url = strings.TrimSpace(url)
	if i := strings.IndexAny(url, "?#"); i >= 0 {
		url = url[:i]
	}
	url = "/" + strings.Trim(path.Clean("/"+strings.Trim(url, "/")), "/")
	if url == "/" {
		return "/"
	}
	if base := path.Base(url); base == "index.html" {
		url = path.Dir(url)
	}
	if path.Ext(url) == "" && !strings.HasSuffix(url, "/") {
		url += "/"
	}
	return url
}

// builtFile is the file in the output tree an address maps to. Pretty URLs mean
// a directory with an index.html in it, which is all hugo writes here unless a
// page asked for a name of its own.
func (s Site) builtFile(url string) (string, error) {
	var tried []string
	for _, rel := range []string{path.Join(url, "index.html"), url} {
		if path.Ext(rel) != ".html" {
			continue
		}
		abs := filepath.Join(s.Output, filepath.FromSlash(rel))
		if !within(s.Output, abs) {
			return "", fmt.Errorf("%w: %s leaves the build output", ErrBadPath, url)
		}
		if fileExists(abs) {
			return abs, nil
		}
		tried = append(tried, rel)
	}
	return "", fmt.Errorf("%w: nothing at %s (looked for %s); build again if you have just moved the page",
		ErrNoPage, url, strings.Join(tried, " and "))
}

// legible refuses the paths that are not paths: an address is a page of this
// site or it is nothing, and a traversal has to be refused before it is joined
// to anything.
func legible(arg string) error {
	if len(arg) > 512 {
		return fmt.Errorf("%w: %d characters is not a page of this site", ErrBadPath, len(arg))
	}
	if !utf8.ValidString(arg) {
		return fmt.Errorf("%w: the path is not valid UTF-8", ErrBadPath)
	}
	if strings.ContainsAny(arg, "\\\x00\n\r\t") {
		return fmt.Errorf("%w: %q contains what no page path does", ErrBadPath, arg)
	}
	for _, seg := range strings.Split(arg, "/") {
		if seg == ".." {
			return fmt.Errorf("%w: %q climbs out of the site", ErrBadPath, arg)
		}
	}
	return nil
}

// within reports whether p is inside root once both are resolved. The output
// tree is the server's own, but it holds whatever hugo put there, and a path
// joined out of an address is checked rather than trusted.
func within(root, p string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	p, err = filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func firstSegment(url string) string {
	seg := strings.Split(strings.Trim(url, "/"), "/")
	return seg[0]
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
