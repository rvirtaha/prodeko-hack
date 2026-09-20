package fence

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
)

// The layout fence. site/layouts is the one root where being writable is a
// property of the file rather than of the directory, because two different
// things live side by side there: what a page looks like, and how the site is
// wired.
//
// The partials a page is assembled from and the three page layouts are the
// first thing. Editing them is editing an appearance, the edit loop shows the
// result in a screenshot, and a maintainer reviewing the pull request is
// looking at the same pixels the editor was.
//
// The skeleton every page is rendered through, the templates that build assets,
// the render hooks that rewrite every link in every Markdown file, the
// shortcodes content files call by name and the language switch are the second.
// Breaking one of those breaks pages nobody was looking at, in ways a
// screenshot of the edited page does not show, so they stay outside the fence.

// LayoutsRoot is the template tree, spelled the way every rule and every tool
// spells it.
const LayoutsRoot = "site/layouts/"

// LayoutGroup is one group of templates and what the fence says about it: what
// it holds when it is writable, why it is fenced when it is not. get_conventions
// prints the groups in this order and is the only statement of the list.
type LayoutGroup struct {
	// Paths names the group the way a reader wants to hear it, which is not
	// always the pattern the match is made of.
	Paths string

	// Write is whether an editor may write the files in this group.
	Write bool

	// Why is the one line get_conventions gives the group.
	Why string

	// match takes the path under [LayoutsRoot], so a group is written in the
	// terms Hugo names templates in rather than in whole repository paths.
	match func(under string) bool
}

// layoutGroups is the whole layout fence, in the order a path is matched
// against it: the fenced specifics first, then the writable patterns, then a
// group that catches everything left.
//
// The order is the rule. partials/head.html is fenced and partials/** is
// writable, and the first group to match is the one that answers.
var layoutGroups = []LayoutGroup{
	{
		Paths: "site/layouts/baseof.html",
		Why: "the shell every page is rendered into: the document head, the header, the footer " +
			"and the scripts. Every page of both languages is inside it.",
		match: func(under string) bool { return path.Base(under) == "baseof.html" },
	},
	{
		Paths: "site/layouts/partials/head.html, site/layouts/partials/footer.html",
		Why: "the head and footer skeleton: the stylesheet bundle, the meta tags, the hreflang " +
			"pairing and the footer frame. They are the page's frame rather than its content.",
		match: oneOf("partials/head.html", "partials/footer.html"),
	},
	{
		Paths: "site/layouts/partials/lang-switch.html",
		Why: "the language switch: it resolves a page to its counterpart in the other language, " +
			"which is the bilingual pairing itself and not a look.",
		match: oneOf("partials/lang-switch.html"),
	},
	{
		Paths: "site/layouts/_markup/**",
		Why: "the render hooks: they rewrite every link and every image in every Markdown file, " +
			"so one mistake there changes pages nobody edited.",
		match: hasSegment("_markup"),
	},
	{
		Paths: "site/layouts/_shortcodes/**",
		Why: "the shortcodes: content files call them by name, and renaming or removing one " +
			"breaks the pages that call it rather than the template.",
		match: hasSegment("_shortcodes", "shortcodes"),
	},
	{
		Paths: "site/layouts/partials/**",
		Why:   "the pieces a page is assembled from: the header, the breadcrumbs, the people grid, the page shell.",
		Write: true,
		match: func(under string) bool { return firstSegment(under) == "partials" },
	},
	{
		Paths: "site/layouts/home.html, site/layouts/section.html, site/layouts/page.html",
		Why:   "the page layouts: the front page, a section listing, a single page.",
		Write: true,
		match: pageLayout,
	},
	{
		Paths: "everything else under site/layouts/**",
		Why: "page types of their own (the hub layouts, people.html, archive.html) and whatever else " +
			"the theme is wired with. Which layout a page is rendered with is a developer's choice.",
		match: func(string) bool { return true },
	},
}

// pipelineGroup is the one layout rule that is about content rather than about a
// path: it is enforced by [Fence.CheckTemplate] against the bytes a write would
// leave behind, and it is stated here so get_conventions states it too.
var pipelineGroup = LayoutGroup{
	Paths: "any template that builds an asset or caches a partial",
	Why: "concatenating, minifying, fingerprinting and compiling assets is how the site is " +
		"assembled, and partialCached is how it is wired; pointing at an image with " +
		"resources.Get is not that and stays writable.",
}

// LayoutGroups is what get_conventions states: every group of templates, the
// writable ones and the fenced ones, each with its one line. The slice is a
// copy, and the match functions are not part of it — the fence is fixed at
// compile time and no caller may extend it at runtime.
func LayoutGroups() []LayoutGroup {
	out := make([]LayoutGroup, 0, len(layoutGroups)+1)
	for _, g := range layoutGroups {
		out = append(out, LayoutGroup{Paths: g.Paths, Write: g.Write, Why: g.Why})
	}
	return append(out, pipelineGroup)
}

// WritableLayouts is the sentence a refusal uses to name the templates an
// editor may write. It comes from the same table get_conventions prints, so
// there is one list of them and not two.
func WritableLayouts() string {
	var names []string
	for _, g := range layoutGroups {
		if g.Write {
			names = append(names, g.Paths)
		}
	}
	return strings.Join(names, ", ")
}

// Writable reports whether rel may be written, by inspection of the path alone.
// It is the write half of every check: the path is legal, some rule covers it,
// and the rule — or, under site/layouts/, the group — permits writing.
//
// A template may pass this and still be refused by [Fence.CheckTemplate], which
// reads what is in the file and what a write would put there.
func Writable(rel string) error {
	clean, rule, err := lexical(rel)
	if err != nil {
		return err
	}
	return writable(clean, rule)
}

// writable is Writable for a path a caller has already cleaned and matched.
func writable(clean string, rule Rule) error {
	if strings.HasPrefix(clean, LayoutsRoot) {
		return layoutWritable(clean)
	}
	if !rule.Write {
		return fmt.Errorf("%w: %s matches %s", ErrReadOnly, clean, rule)
	}
	return nil
}

// layoutWritable applies the layout groups to one path under site/layouts/. A
// refusal quotes the group's own line, because "why not" is the thing the model
// has to hear: it decides whether to edit something else or to hand the request
// to a developer.
//
// The line is quoted after a dash rather than read into the sentence, because
// the groups are named in the plural as often as in the singular and a template
// that "is the render hooks" reads like a mistake in the tool rather than in the
// request.
func layoutWritable(clean string) error {
	g := layoutGroupFor(clean)
	if g.Write {
		return nil
	}
	return fmt.Errorf("%w: %s — %s It is outside the fence, deliberately; writable templates are %s",
		ErrReadOnly, clean, g.Why, WritableLayouts())
}

// layoutGroupFor is the group covering one path under site/layouts/. The last
// group matches everything, so there is always one.
func layoutGroupFor(clean string) LayoutGroup {
	under := strings.TrimPrefix(clean, LayoutsRoot)
	for _, g := range layoutGroups {
		if g.match(under) {
			return g
		}
	}
	// Unreachable while the table ends in a group that matches everything, and
	// a fence whose table stopped matching denies rather than permits.
	return LayoutGroup{Paths: clean, Why: "under no layout group."}
}

// pipelineCalls are the template functions that build an asset rather than
// point at one, plus partialCached.
//
// resources.Get is deliberately absent. Every hero picture and every partner
// logo on this site is addressed with it, home.html is writable by design, and
// a rule that fenced it would fence the layouts the media team is here to edit.
// What is fenced is the machinery around it: concatenating, minifying,
// fingerprinting, compiling, fetching over the network, and caching a partial
// under a key.
var pipelineCalls = regexp.MustCompile(`\b(partialCached|` +
	`resources\.(ByType|Concat|Copy|ExecuteAsTemplate|FromString|GetMatch|GetRemote|Match|Minify|PostProcess)|` +
	`js\.(Babel|Build)|css\.(Sass|TailwindCSS)|templates\.Defer|toCSS|babel|fingerprint|minify)\b`)

// CheckTemplate is the content half of the layout fence: a template that runs
// the asset pipeline is read-only whatever its name is, and a writable one does
// not become a pipeline template because an editor wrote one call into it.
//
// It is content and not path because the fence is compiled into this server
// while the site goes on evolving: a partial that starts bundling the
// stylesheet tomorrow has to close behind it without a release here.
//
// Paths outside site/layouts/ pass without being read. A stylesheet is not a
// template, and the words this looks for are ordinary ones in CSS and Markdown.
func (f *Fence) CheckTemplate(rel string, content []byte) error {
	clean, err := Clean(rel)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(clean, LayoutsRoot) {
		return nil
	}
	abs, err := f.locate(clean)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(abs)
	switch {
	case err == nil:
		if call := pipelineCalls.FindString(string(existing)); call != "" {
			return fmt.Errorf("%w: %s calls %s, which makes it an asset pipeline template: %s",
				ErrReadOnly, clean, call, pipelineGroup.Why)
		}
	case errors.Is(err, fs.ErrNotExist):
		// A template that is not there yet is a new partial, which is an
		// ordinary thing to write.
	default:
		return fmt.Errorf("%w: %s cannot be inspected: %v", ErrBadPath, clean, err)
	}
	if call := pipelineCalls.FindString(string(content)); call != "" {
		return fmt.Errorf("%w: this would put %s into %s: %s",
			ErrReadOnly, call, clean, pipelineGroup.Why)
	}
	return nil
}

// oneOf matches an exact template path, under _default/ as well as at the
// layouts root: Hugo accepts both spellings and the fence has to mean the same
// thing in either.
func oneOf(names ...string) func(string) bool {
	return func(under string) bool {
		under = strings.TrimPrefix(under, "_default/")
		for _, name := range names {
			if under == name {
				return true
			}
		}
		return false
	}
}

// hasSegment matches a whole subtree by the directory that names it, wherever
// that directory sits: Hugo has moved the render hooks and the shortcodes
// between layouts/ and layouts/_default/ across versions.
func hasSegment(names ...string) func(string) bool {
	return func(under string) bool {
		for _, seg := range strings.Split(under, "/") {
			for _, name := range names {
				if seg == name {
					return true
				}
			}
		}
		return false
	}
}

// pageLayout matches the page layouts an editor may write, under both of Hugo's
// naming schemes: home.html with section.html and page.html, and the older
// list.html and single.html that mean the same two things.
func pageLayout(under string) bool {
	return oneOf("home.html", "section.html", "page.html", "list.html", "single.html")(under)
}

func firstSegment(under string) string {
	seg, _, _ := strings.Cut(under, "/")
	return seg
}
