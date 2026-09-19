// Package toolset is the nine tools the MCP server exposes, built on the fence
// and the worktree manager.
//
// File-level tools, not semantic ones: CSS is file-level, and update_page(slug,
// body) cannot express "make the events header blue". Once file tools exist,
// semantic content tools would be a second way to do what Decap already does.
//
// The tools own no policy of their own. Every path they touch goes through
// [fence.Fence] first and every write goes through [workdir.Change]; what the
// tool layer adds is the argument contract in schema.go and the wording the
// model reads.
package toolset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/prodeko/prodeko-hack/proxy/internal/fence"
	"github.com/prodeko/prodeko-hack/proxy/internal/mcpserver"
	"github.com/prodeko/prodeko-hack/proxy/internal/workdir"
)

type Config struct {
	// Workdir is where every file operation happens.
	Workdir *workdir.Manager

	// Conventions overrides the built-in site guide. Empty means
	// [Conventions], which is the normal case; it exists so a test can assert
	// on a short text instead of the whole guide.
	Conventions string

	Logger *slog.Logger     // nil means slog.Default
	Now    func() time.Time // nil means time.Now
}

// Toolset holds the open change per person. One person has at most one current
// change at a time: the tools carry no change argument, because the person is
// talking about the thing they are working on and asking a model to thread an
// identifier through the conversation is how the identifier ends up wrong.
//
// A change is opened lazily by the first write, with a slug derived from what
// was asked for. submit pushes it; the change stays current afterwards, so
// "vähän vaaleampi" continues onto the same branch and the same pull request.
type Toolset struct {
	mgr         *workdir.Manager
	conventions string
	log         *slog.Logger
	now         func() time.Time

	mu      sync.Mutex
	current map[string]*workdir.Change // username -> the change being edited
}

func New(cfg Config) (*Toolset, error) {
	if cfg.Workdir == nil {
		return nil, errors.New("toolset: a workdir.Manager is required")
	}
	if cfg.Conventions == "" {
		cfg.Conventions = Conventions()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Toolset{
		mgr:         cfg.Workdir,
		conventions: cfg.Conventions,
		log:         cfg.Logger,
		now:         cfg.Now,
		current:     make(map[string]*workdir.Change),
	}, nil
}

// Instructions is what the MCP initialize response carries. The conventions
// are told once per session there and repeated by get_conventions for clients
// that drop them.
func (t *Toolset) Instructions() string { return t.conventions }

// Tools is the nine, in the order a session uses them.
func (t *Toolset) Tools() []mcpserver.Tool {
	return []mcpserver.Tool{
		{
			Name: ToolGetConventions,
			Description: "How prodeko.org is laid out and how to edit it: the two content roots, the Finnish/English " +
				"translationKey pairing, the design tokens, and what is editable and what is not. Read this first.",
			Schema: schemaGetConventions,
			Call:   t.getConventions,
		},
		{
			Name: ToolListFiles,
			Description: "List the editable files. The tree is a few hundred files, so listing it unfiltered is " +
				"reasonable; pass a glob to narrow it.",
			Schema: schemaListFiles,
			Call:   t.listFiles,
		},
		{
			Name: ToolReadFile,
			Description: "Read a file, optionally a line range. Prefer a range for large files: site/assets/css/main.css " +
				"is over a thousand lines and reading it whole every turn is the expensive habit.",
			Schema: schemaReadFile,
			Call:   t.readFile,
		},
		{
			Name: ToolSearch,
			Description: "Search the editable files with a regular expression, line by line. This is how you find which " +
				"template renders a heading and which rule styles it.",
			Schema: schemaSearch,
			Call:   t.search,
		},
		{
			Name: ToolWriteFile,
			Description: "Write a whole file. Use it for a new page or a wholesale rewrite; prefer edit_file for a change " +
				"inside an existing file.",
			Schema: schemaWriteFile,
			Call:   t.writeFile,
		},
		{
			Name: ToolEditFile,
			Description: "Replace an exact piece of text in a file. The old text must appear exactly once, so include " +
				"enough surrounding lines to make it unique.",
			Schema: schemaEditFile,
			Call:   t.editFile,
		},
		{
			Name: ToolBuild,
			Description: "Build the site and run its tree check. Returns the errors verbatim when it fails. Run it after " +
				"editing and always before submitting.",
			Schema: schemaBuild,
			Call:   t.build,
		},
		{
			Name: ToolSubmit,
			Description: "Commit the change in the signed-in person's name, push it, and open a draft pull request. " +
				"Returns the pull request number and the preview URL, which is ready about a minute later.",
			Schema: schemaSubmit,
			Call:   t.submit,
		},
		{
			Name: ToolListMyChanges,
			Description: "List the signed-in person's own open changes: branch, files touched, pull request, CI state and " +
				"preview link.",
			Schema: schemaListMyChanges,
			Call:   t.listMyChanges,
		},
	}
}

func (t *Toolset) getConventions(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	return t.conventions, nil
}

func (t *Toolset) listFiles(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	a, err := decode[listFilesArgs](args)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ToolListFiles, err)
	}
	c, err := t.open(id, "")
	if err != nil {
		return "", err
	}
	files, err := c.ListFiles(a.Glob)
	if err != nil {
		return "", err
	}
	return renderFiles(files, a.Glob), nil
}

func (t *Toolset) readFile(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	a, err := decode[readFileArgs](args)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ToolReadFile, err)
	}
	rel, err := checkPath(a.Path, false)
	if err != nil {
		return "", err
	}
	if a.Start < 0 || a.End < 0 {
		return "", fmt.Errorf("%s: lines are numbered from 1; %d and %d are not lines", ToolReadFile, a.Start, a.End)
	}
	if a.Start > 0 && a.End > 0 && a.End < a.Start {
		return "", fmt.Errorf("%s: end %d is before start %d", ToolReadFile, a.End, a.Start)
	}
	c, err := t.open(id, "")
	if err != nil {
		return "", err
	}
	// Returned exactly as it is on disk, with nothing prepended: edit_file
	// matches an exact string, and decoration is what the model would copy
	// into it by mistake.
	return c.ReadFile(rel, a.Start, a.End)
}

func (t *Toolset) search(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	a, err := decode[searchArgs](args)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ToolSearch, err)
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return "", fmt.Errorf("%s: the pattern is empty", ToolSearch)
	}
	if _, err := regexp.Compile(a.Pattern); err != nil {
		return "", fmt.Errorf("%s: %q is not a usable regular expression: %w", ToolSearch, a.Pattern, err)
	}
	max := a.MaxResults
	if max <= 0 {
		max = DefaultSearchResults
	}
	if max > MaxSearchResults {
		max = MaxSearchResults
	}
	c, err := t.open(id, "")
	if err != nil {
		return "", err
	}
	matches, err := c.Search(a.Pattern, a.Glob, max)
	if err != nil {
		return "", err
	}
	return renderMatches(matches, a.Pattern, max), nil
}

func (t *Toolset) writeFile(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	a, err := decode[writeFileArgs](args)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ToolWriteFile, err)
	}
	rel, err := checkPath(a.Path, true)
	if err != nil {
		return "", err
	}
	if len(a.Content) > fence.MaxTextBytes {
		return "", fmt.Errorf("%w: %s got %d bytes of content, at most %d in one write",
			fence.ErrTooLarge, ToolWriteFile, len(a.Content), fence.MaxTextBytes)
	}
	c, err := t.open(id, hintFor(rel))
	if err != nil {
		return "", err
	}
	if err := c.Fence().CheckWrite(rel, int64(len(a.Content))); err != nil {
		return "", err
	}
	if err := c.WriteFile(rel, []byte(a.Content)); err != nil {
		return "", err
	}
	return fmt.Sprintf("Wrote %s, %d bytes. Build before you submit.", rel, len(a.Content)), nil
}

func (t *Toolset) editFile(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	a, err := decode[editFileArgs](args)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ToolEditFile, err)
	}
	rel, err := checkPath(a.Path, true)
	if err != nil {
		return "", err
	}
	if a.Old == nil || a.New == nil {
		return "", fmt.Errorf("%s: old and new are both required; they are the exact text to replace and the text to put in its place", ToolEditFile)
	}
	old, replacement := *a.Old, *a.New
	if old == "" {
		return "", fmt.Errorf("%s: the text to replace is empty; write_file replaces a whole file", ToolEditFile)
	}
	if old == replacement {
		return "", fmt.Errorf("%s: the replacement is the text it replaces", ToolEditFile)
	}
	if len(replacement) > fence.MaxTextBytes {
		return "", fmt.Errorf("%w: %s got %d bytes of replacement, at most %d",
			fence.ErrTooLarge, ToolEditFile, len(replacement), fence.MaxTextBytes)
	}
	c, err := t.open(id, hintFor(rel))
	if err != nil {
		return "", err
	}
	if err := c.Fence().CheckWrite(rel, int64(len(replacement))); err != nil {
		return "", err
	}
	if err := c.EditFile(rel, old, replacement); err != nil {
		return "", err
	}
	return fmt.Sprintf("Edited %s. Build before you submit.", rel), nil
}

func (t *Toolset) build(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	c, err := t.open(id, "")
	if err != nil {
		return "", err
	}
	res, err := t.mgr.Build(ctx, c)
	if err != nil {
		return "", err
	}
	// A failing build is a result, not a transport failure: its output is the
	// thing the model has to read.
	return renderBuild(res), nil
}

func (t *Toolset) submit(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	a, err := decode[submitArgs](args)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ToolSubmit, err)
	}
	title := strings.TrimSpace(a.Title)
	if title == "" {
		return "", fmt.Errorf("%s: the title is empty; it is what a maintainer reads first", ToolSubmit)
	}
	if len(title) > MaxTitleLen {
		return "", fmt.Errorf("%s: the title is %d bytes, at most %d", ToolSubmit, len(title), MaxTitleLen)
	}
	description := strings.TrimSpace(a.Description)
	if len(description) > MaxDescriptionLen {
		return "", fmt.Errorf("%s: the description is %d bytes, at most %d", ToolSubmit, len(description), MaxDescriptionLen)
	}
	author, err := authorOf(id)
	if err != nil {
		return "", err
	}
	c, err := t.open(id, title)
	if err != nil {
		return "", err
	}
	res, err := t.mgr.Submit(ctx, c, author, title, description)
	if err != nil {
		return "", err
	}
	return renderSubmit(res), nil
}

func (t *Toolset) listMyChanges(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	user, err := userOf(id)
	if err != nil {
		return "", err
	}
	infos, err := t.mgr.List(ctx, user)
	if err != nil {
		return "", err
	}
	return renderChanges(infos), nil
}

// open returns the caller's current change, creating one named after hint when
// there is none. Reads need a tree as much as writes do, so this is on the
// path of every tool except get_conventions and list_my_changes.
func (t *Toolset) open(id mcpserver.Identity, hint string) (*workdir.Change, error) {
	user, err := userOf(id)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.current[user]; ok {
		return c, nil
	}
	// The map is a cache, not the record: the worktrees on the state volume
	// are, so that a restart continues the change somebody is in the middle of
	// instead of quietly opening a second branch for the same work.
	c, ok, err := t.mgr.Resume(user)
	if err != nil {
		return nil, err
	}
	if !ok {
		if c, err = t.mgr.Change(user, workdir.Slug(hint, t.now())); err != nil {
			return nil, err
		}
	}
	t.current[user] = c
	return c, nil
}

// userOf is the branch namespace and the worktree directory both. An identity
// with no username is a bug in whoever minted the token, and a change under an
// empty name would be a change under everyone's name.
//
// The Keycloak realm uses the email address as the username, and an address
// is not a branch name: workdir refuses anything outside its narrow pattern.
// The name is derived here, at the identity boundary, rather than loosening
// workdir's rule — deterministically, so the same person always lands in the
// same namespace.
func userOf(id mcpserver.Identity) (string, error) {
	raw := strings.TrimSpace(id.Username)
	if raw == "" {
		return "", errors.New("toolset: the signed-in identity carries no username")
	}
	user := namespaceName(raw)
	if user == "" {
		return "", fmt.Errorf("toolset: no usable branch name can be made of username %q", raw)
	}
	return user, nil
}

// namespaceName maps a username onto workdir's branch-safe alphabet:
// lowercased, every excluded rune becomes a hyphen, and the result is trimmed
// to start and end on a letter or digit. "rvirtaha@hotmail.com" becomes
// "rvirtaha-hotmail.com". Distinct addresses collide only if they differ
// solely in excluded runes, which addresses of the same realm do not.
func namespaceName(raw string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, raw)
	// ".." is a traversal to workdir no matter how it got in.
	for strings.Contains(mapped, "..") {
		mapped = strings.ReplaceAll(mapped, "..", ".")
	}
	mapped = strings.Trim(mapped, "._-")
	if len(mapped) > 64 {
		mapped = strings.Trim(mapped[:64], "._-")
	}
	return mapped
}

// authorOf is the commit author: the verified Keycloak identity and never
// anything the client sent. A missing address is refused rather than replaced
// with the bot's, which would put a media person's change in the bot's name.
func authorOf(id mcpserver.Identity) (workdir.Author, error) {
	user, err := userOf(id)
	if err != nil {
		return workdir.Author{}, err
	}
	email := strings.TrimSpace(id.Email)
	if email == "" {
		return workdir.Author{}, errors.New("toolset: the signed-in identity carries no email address, so the commit cannot be authored")
	}
	name := strings.TrimSpace(id.Name)
	if name == "" {
		name = user
	}
	return workdir.Author{Name: name, Email: email}, nil
}

// checkPath is the lexical half of the fence, applied before a worktree is
// opened: a path no rule covers is refused without costing a clone, and the
// refusal names where work is possible instead. The authoritative check is the
// change's own fence, which also follows symlinks; this never replaces it.
func checkPath(rel string, write bool) (string, error) {
	clean, err := fence.Clean(rel)
	if err != nil {
		return "", err
	}
	rule, ok := fence.Match(clean)
	if !ok {
		return "", fmt.Errorf("%w: %s is under no rule; the editable roots are %s", fence.ErrOutside, clean, editableRoots())
	}
	if write && !rule.Write {
		return "", fmt.Errorf("%w: %s matches %s", fence.ErrReadOnly, clean, rule)
	}
	return clean, nil
}

// editableRoots is the allowlist as the model should hear it named.
func editableRoots() string {
	rules := fence.Rules()
	names := make([]string, len(rules))
	for i, r := range rules {
		names[i] = r.String()
	}
	return strings.Join(names, ", ")
}

// hintFor names a change after the file the first edit touched, which is as
// close as this layer gets to what was asked for. An index page is named after
// its directory, because "_index" names nothing.
func hintFor(rel string) string {
	base := path.Base(rel)
	name := strings.TrimSuffix(base, path.Ext(base))
	if name == "" || name == "index" || name == "_index" {
		name = path.Base(path.Dir(rel))
	}
	if name == "." || name == "/" {
		return ""
	}
	return name
}

// decode unmarshals a tool's arguments. An absent arguments object is an empty
// one: clients differ on whether they send "arguments": {} for a tool that
// takes none.
//
// Unknown properties are refused, because every schema is closed and a client
// that did not validate against it would otherwise have its argument silently
// dropped. A mistyped name has to come back as a mistyped name; answering a
// missing "old" as though the caller had sent an empty one sends the model
// looking for the wrong mistake.
func decode[T any](args json.RawMessage) (T, error) {
	var v T
	if len(strings.TrimSpace(string(args))) == 0 || string(args) == "null" {
		return v, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, err
	}
	return v, nil
}
