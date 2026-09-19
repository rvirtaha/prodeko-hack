// Package fence is the one allowlist that governs what the content editor MCP
// server may read and what it may write. Outside it nothing is legible: a path
// no rule covers does not exist, whether the caller asks to read it, write it
// or merely list it.
//
// The fence is the enforcement layer of the design. Site conventions are
// advice carried in tool descriptions; this is not advice. Every tool that
// touches the filesystem asks here first, and a check that cannot be completed
// denies.
//
// Path hygiene follows the same segment discipline as the proxy's GitHub
// allowlist (internal/forward/allow.go): no empty, dot or dot-dot segment in
// either literal or percent-encoded form, no control characters, no
// backslashes, nothing absolute, and no symlink that resolves back out of the
// worktree.
package fence

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Mechanical limits, from the design document. They bound what one change can
// be, so a mistake stays a mistake rather than becoming a denial of service on
// the build or on review.
const (
	// MaxFileBytes is the largest file the fence will read or write.
	MaxFileBytes = 5 << 20

	// MaxTextBytes is the most text one change may write in total.
	MaxTextBytes = 2 << 20

	// MaxChangeFiles is the most files one change may touch.
	MaxChangeFiles = 50
)

// maxPathLen bounds one repository-relative path. It is the same limit the
// tool schemas state, so a path a client validated is a path this accepts.
const maxPathLen = 512

// Rule is one allowlisted subtree, named by its slash-separated prefix
// relative to the repository root. Write implies read; there is no
// write-without-read rule and there never should be.
type Rule struct {
	Prefix string
	Write  bool
}

// String names the rule the way an error message wants to say it.
func (r Rule) String() string {
	if r.Write {
		return r.Prefix + "** (read and write)"
	}
	return r.Prefix + "** (read only)"
}

// rules is the whole allowlist, in the order a path is matched against it.
//
// site/layouts is readable and not writable on purpose: reading a template is
// how "the events header" resolves to a CSS selector, while writing one would
// make this a second developer interface and break the promise that editors
// and developers cannot break each other's half of the site.
//
// Everything absent is denied, and four of those absences carry the security
// of the feature: .github (the preview workflow runs with a deploy key in
// scope), site/hugo.toml and site/config (unsafe rendering, the public/member
// split), site/static/admin (what Decap editors may write), and
// site/check-trees.sh (the audit the build runs).
var rules = []Rule{
	{Prefix: "site/content/", Write: true},
	{Prefix: "site/content-members/", Write: true},
	{Prefix: "site/data/", Write: true},
	{Prefix: "site/assets/css/", Write: true},
	{Prefix: "site/assets/images/", Write: true},
	{Prefix: "site/layouts/", Write: false},
}

// Rules returns the allowlist. The slice is a copy; the fence is fixed at
// compile time and no caller may extend it at runtime.
func Rules() []Rule {
	out := make([]Rule, len(rules))
	copy(out, rules)
	return out
}

// Denial reasons. Every check names the rule it applied, so a refusal tells
// the model what to do differently instead of only that it may not.
var (
	ErrBadPath      = errors.New("fence: not a legal repository path")
	ErrOutside      = errors.New("fence: outside the editable tree")
	ErrReadOnly     = errors.New("fence: readable but not writable")
	ErrTooLarge     = errors.New("fence: over the size limit")
	ErrTooManyFiles = errors.New("fence: too many files in one change")
	ErrSymlink      = errors.New("fence: symlink leaves the editable tree")
)

// Fence applies the allowlist inside one tree. Root is a worktree, not the
// bare clone: every relative path is resolved against it and must still be
// inside it after symlinks are followed.
type Fence struct {
	root string
}

// NewFence fixes the allowlist to root, which must be an absolute path to an
// existing directory. The rules themselves are not configurable.
//
// The directory is not required to exist yet, because a worktree is created
// after the fence that will guard it. A root that cannot be resolved when a
// path is checked denies that check.
func NewFence(root string) (*Fence, error) {
	if root == "" {
		return nil, fmt.Errorf("%w: root must not be empty", ErrBadPath)
	}
	return &Fence{root: root}, nil
}

// Root is the tree this fence guards.
func (f *Fence) Root() string { return f.root }

// CheckRead reports whether rel may be read. rel is slash-separated and
// relative to the repository root, e.g. "site/content/fi/tapahtumat.md".
func (f *Fence) CheckRead(rel string) error {
	_, err := f.ResolveRead(rel)
	return err
}

// CheckWrite reports whether rel may be written with size bytes of content.
// It is the read check plus the write rule and the size limit.
func (f *Fence) CheckWrite(rel string, size int64) error {
	_, err := f.ResolveWrite(rel, size)
	return err
}

// CheckChange applies the per-change limits: at most MaxChangeFiles paths and
// MaxTextBytes of text across all of them. Every path must be one the change
// was allowed to write, so a change cannot be committed through a path no tool
// would have accepted.
func (f *Fence) CheckChange(paths []string, totalBytes int64) error {
	if len(paths) > MaxChangeFiles {
		return fmt.Errorf("%w: %d files, at most %d in one change", ErrTooManyFiles, len(paths), MaxChangeFiles)
	}
	if totalBytes > MaxTextBytes {
		return fmt.Errorf("%w: %d bytes in one change, at most %d", ErrTooLarge, totalBytes, MaxTextBytes)
	}
	for _, p := range paths {
		clean, rule, err := lexical(p)
		if err != nil {
			return err
		}
		if !rule.Write {
			return fmt.Errorf("%w: %s matches %s", ErrReadOnly, clean, rule)
		}
	}
	return nil
}

// CheckStage reports whether rel may go into a commit and returns the bytes it
// contributes to the per-change total. A path that is gone from the worktree is
// a deletion: it contributes nothing and is allowed, because removing a file
// inside the allowlist is an ordinary edit.
//
// This is the filesystem half of the staging check, and it is deliberately
// stricter than ResolveWrite: the size comes from Lstat and anything that is
// not a regular file is refused outright. git stores a symlink as a blob whose
// content is its target, so a link under site/content/ would ship an
// out-of-tree path into a pull request and be followed by whatever checks out
// the branch. The tools cannot create one; the fence is what makes that true of
// the commit as well.
func (f *Fence) CheckStage(rel string) (int64, error) {
	clean, rule, err := lexical(rel)
	if err != nil {
		return 0, err
	}
	if !rule.Write {
		return 0, fmt.Errorf("%w: %s matches %s", ErrReadOnly, clean, rule)
	}
	// locate resolves every symlink on the path and refuses one that leaves the
	// worktree; Lstat then refuses one that stays inside it.
	if _, err := f.locate(clean); err != nil {
		return 0, err
	}
	info, err := os.Lstat(filepath.Join(f.root, filepath.FromSlash(clean)))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("%w: %s cannot be inspected: %v", ErrBadPath, clean, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return 0, fmt.Errorf("%w: %s is a symlink and cannot be committed", ErrSymlink, clean)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("%w: %s is not a regular file", ErrBadPath, clean)
	}
	if info.Size() > MaxFileBytes {
		return 0, fmt.Errorf("%w: %s is %d bytes, over the %d byte file limit", ErrTooLarge, clean, info.Size(), MaxFileBytes)
	}
	return info.Size(), nil
}

// ResolveRead runs CheckRead and returns the absolute path to open. Callers
// must open the path it returns and never one they built themselves, because
// the symlink check is only true of the resolved path.
func (f *Fence) ResolveRead(rel string) (string, error) {
	clean, _, err := lexical(rel)
	if err != nil {
		return "", err
	}
	abs, err := f.locate(clean)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		// A path that does not exist is the caller's error to report, not a
		// denial: the fence has said everything it has to say about it.
		return abs, nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s cannot be inspected: %v", ErrBadPath, clean, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a regular file", ErrBadPath, clean)
	}
	if info.Size() > MaxFileBytes {
		return "", fmt.Errorf("%w: %s is %d bytes, over the %d byte file limit", ErrTooLarge, clean, info.Size(), MaxFileBytes)
	}
	return abs, nil
}

// ResolveWrite runs CheckWrite and returns the absolute path to write.
func (f *Fence) ResolveWrite(rel string, size int64) (string, error) {
	clean, rule, err := lexical(rel)
	if err != nil {
		return "", err
	}
	if !rule.Write {
		return "", fmt.Errorf("%w: %s matches %s", ErrReadOnly, clean, rule)
	}
	if size > MaxFileBytes {
		return "", fmt.Errorf("%w: %d bytes for %s, at most %d in one file", ErrTooLarge, size, clean, MaxFileBytes)
	}
	abs, err := f.locate(clean)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		// Writing a file that is not there yet is the ordinary case.
		return abs, nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s cannot be inspected: %v", ErrBadPath, clean, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a regular file", ErrBadPath, clean)
	}
	return abs, nil
}

// Visible reports whether rel is covered by any rule, by inspection of the
// path alone and without touching the filesystem. It is how a listing decides
// what to show; it is not an authorisation and never replaces CheckRead.
func (f *Fence) Visible(rel string) bool {
	_, ok := Match(rel)
	return ok
}

// Match returns the rule covering rel, if any. It is purely lexical.
func Match(rel string) (Rule, bool) {
	clean, err := Clean(rel)
	if err != nil {
		return Rule{}, false
	}
	for _, r := range rules {
		// The prefixes end in a slash, so site/content-members/ is not matched
		// by the rule for site/content/ and a bare directory name matches
		// nothing: a rule covers files under a root, not the root itself.
		if strings.HasPrefix(clean, r.Prefix) {
			return r, true
		}
	}
	return Rule{}, false
}

// Clean validates one relative repository path and returns it in the canonical
// slash-separated form the rest of the server uses. It touches no filesystem,
// so it can be applied to anything a client sends before that value is used to
// build a path at all.
func Clean(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("%w: the path is empty", ErrBadPath)
	}
	if len(rel) > maxPathLen {
		return "", fmt.Errorf("%w: the path is %d bytes, at most %d", ErrBadPath, len(rel), maxPathLen)
	}
	if !utf8.ValidString(rel) {
		return "", fmt.Errorf("%w: the path is not valid UTF-8", ErrBadPath)
	}
	if strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: %q is absolute; paths are relative to the repository root", ErrBadPath, rel)
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" {
			return "", fmt.Errorf("%w: %q has an empty segment", ErrBadPath, rel)
		}
		dec, err := url.PathUnescape(seg)
		if err != nil {
			return "", fmt.Errorf("%w: %q is not valid percent-encoding", ErrBadPath, rel)
		}
		// Both forms are checked: site/content/%2e%2e/x is one segment on the
		// wire and names something else entirely once it is used as a path.
		for _, s := range [2]string{seg, dec} {
			if err := checkSegment(rel, s); err != nil {
				return "", err
			}
		}
	}
	return rel, nil
}

// checkSegment rejects the characters and the names that cannot appear in a
// path this server will open.
func checkSegment(rel, seg string) error {
	for _, r := range seg {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: %q contains a control character", ErrBadPath, rel)
		}
	}
	if strings.ContainsRune(seg, '\\') {
		return fmt.Errorf("%w: %q contains a backslash; paths are slash-separated", ErrBadPath, rel)
	}
	for _, part := range strings.Split(seg, "/") {
		if part == "." || part == ".." {
			return fmt.Errorf("%w: %q contains a traversal segment", ErrBadPath, rel)
		}
	}
	return nil
}

// lexical is the part of every check that needs no filesystem: the path is
// legal and some rule covers it.
func lexical(rel string) (string, Rule, error) {
	clean, err := Clean(rel)
	if err != nil {
		return "", Rule{}, err
	}
	rule, ok := Match(clean)
	if !ok {
		return "", Rule{}, fmt.Errorf("%w: %s is under no rule; the editable roots are %s", ErrOutside, clean, summary())
	}
	return clean, rule, nil
}

// summary names the allowlist for a refusal, so a model that asked for
// site/hugo.toml learns where it may work instead.
func summary() string {
	names := make([]string, len(rules))
	for i, r := range rules {
		names[i] = r.String()
	}
	return strings.Join(names, ", ")
}

// locate turns a checked relative path into the absolute path to open, with
// every symlink on it resolved and the result confirmed to be inside the
// worktree. It is the only place a path this package hands out is built.
func (f *Fence) locate(clean string) (string, error) {
	root, err := filepath.EvalSymlinks(f.root)
	if err != nil {
		return "", fmt.Errorf("%w: the worktree %s cannot be resolved: %v", ErrOutside, f.root, err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: the worktree %s cannot be resolved: %v", ErrOutside, f.root, err)
	}
	resolved, err := resolve(filepath.Join(root, filepath.FromSlash(clean)))
	if err != nil {
		return "", err
	}
	if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %s resolves to %s, outside %s", ErrSymlink, clean, resolved, root)
	}
	return resolved, nil
}

// resolve follows the symlinks on abs, which may name a file that does not
// exist yet. It walks up to the deepest component that exists, resolves that,
// and appends the rest; components that do not exist cannot be symlinks, and a
// component that exists only as a symlink to nothing is refused rather than
// followed blindly, because a write through it would land wherever it points.
func resolve(abs string) (string, error) {
	var rest []string
	cur := abs
	for {
		info, err := os.Lstat(cur)
		if err == nil {
			target, evalErr := filepath.EvalSymlinks(cur)
			if evalErr != nil {
				if info.Mode()&fs.ModeSymlink != 0 {
					return "", fmt.Errorf("%w: %s does not resolve: %v", ErrSymlink, cur, evalErr)
				}
				return "", fmt.Errorf("%w: %s does not resolve: %v", ErrBadPath, cur, evalErr)
			}
			return filepath.Join(append([]string{target}, rest...)...), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", fmt.Errorf("%w: %s cannot be resolved", ErrBadPath, abs)
		}
		rest = append([]string{filepath.Base(cur)}, rest...)
		cur = parent
	}
}
