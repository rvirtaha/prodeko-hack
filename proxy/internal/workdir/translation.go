package workdir

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The fi/en bookkeeping behind translation_status. Pages pair by the
// translationKey in their front matter, within one content root: a Finnish
// page and its English side share a key, and a key alone on its side is a
// missing translation. Age is the last commit that touched the file, read in
// one pass over the worktree's history rather than one git call per page.

// TranslationPage is one content page as the pairing sees it.
type TranslationPage struct {
	Path string // repository-relative
	Lang string // "fi", "en", ...
	At   time.Time
}

// TranslationPair is a key with pages on both sides, reported when one side
// kept moving after the other stopped.
type TranslationPair struct {
	Key   string
	Newer TranslationPage
	Older TranslationPage
}

// TranslationStatus is what the tool reports.
type TranslationStatus struct {
	Missing []TranslationPage // pages whose key has no page on the other side
	Stale   []TranslationPair // pairs whose sides were last touched at different times, widest gap first
	Unkeyed []string          // pages with no translationKey at all; they can never pair
	Total   int               // pages considered
}

// contentRoots are where pages live. Each root pairs within itself: a public
// page and a member page are different pages even on the same key.
var contentRoots = []string{"site/content", "site/content-members"}

// maxFrontMatterBytes bounds how much of a page is read looking for its front
// matter. A key further in than this is a page nothing else can parse either.
const maxFrontMatterBytes = 8 << 10

// TranslationStatus pairs the change's content pages by translationKey.
// pathFilter narrows the report to pages under one repository-relative
// prefix; empty means the whole content tree.
func (m *Manager) TranslationStatus(ctx context.Context, c *Change, pathFilter string) (TranslationStatus, error) {
	if c == nil {
		return TranslationStatus{}, ErrNoChange
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	ages, err := m.commitAges(ctx, c)
	if err != nil {
		return TranslationStatus{}, err
	}

	type side struct{ pages map[string]TranslationPage } // lang -> page
	pairs := make(map[string]*side)                      // root + "\x00" + key
	var status TranslationStatus

	for _, root := range contentRoots {
		rootDir := filepath.Join(c.Dir, filepath.FromSlash(root))
		err := filepath.WalkDir(rootDir, func(abs string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil //nolint:nilerr // a root the site lacks is simply empty
			}
			rel := root + "/" + filepath.ToSlash(strings.TrimPrefix(abs, rootDir+string(filepath.Separator)))
			if pathFilter != "" && !strings.HasPrefix(rel, pathFilter) {
				return nil
			}
			lang, ok := langOf(rel, root)
			if !ok {
				return nil
			}
			status.Total++

			key, err := translationKey(abs)
			if err != nil {
				return err
			}
			at, committed := ages[rel]
			if !committed {
				// A page this change created has no commit yet; it is as new
				// as it gets, which is exactly how it should compare.
				at = m.cfg.Now()
			}
			page := TranslationPage{Path: rel, Lang: lang, At: at}
			if key == "" {
				status.Unkeyed = append(status.Unkeyed, rel)
				return nil
			}
			id := root + "\x00" + key
			if pairs[id] == nil {
				pairs[id] = &side{pages: make(map[string]TranslationPage)}
			}
			pairs[id].pages[lang] = page
			return nil
		})
		if err != nil {
			return TranslationStatus{}, fmt.Errorf("workdir: walking %s: %w", root, err)
		}
	}

	for id, s := range pairs {
		key := id[strings.IndexByte(id, '\x00')+1:]
		if len(s.pages) == 1 {
			for _, p := range s.pages {
				status.Missing = append(status.Missing, p)
			}
			continue
		}
		var newest, oldest TranslationPage
		for _, p := range s.pages {
			if newest.Path == "" || p.At.After(newest.At) {
				newest = p
			}
			if oldest.Path == "" || p.At.Before(oldest.At) {
				oldest = p
			}
		}
		if newest.At.After(oldest.At) {
			status.Stale = append(status.Stale, TranslationPair{Key: key, Newer: newest, Older: oldest})
		}
	}

	sort.Slice(status.Missing, func(i, j int) bool { return status.Missing[i].Path < status.Missing[j].Path })
	sort.Slice(status.Stale, func(i, j int) bool {
		gi := status.Stale[i].Newer.At.Sub(status.Stale[i].Older.At)
		gj := status.Stale[j].Newer.At.Sub(status.Stale[j].Older.At)
		if gi != gj {
			return gi > gj
		}
		return status.Stale[i].Key < status.Stale[j].Key
	})
	sort.Strings(status.Unkeyed)
	return status, nil
}

// commitAges is when each tracked file was last touched, in one walk of the
// history: newest commit first, so the first time a path appears is its
// answer.
func (m *Manager) commitAges(ctx context.Context, c *Change) (map[string]time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, GitTimeout)
	defer cancel()
	out, err := m.git(ctx, c.Dir, "log", "--format=%x01%ct", "--name-only", "--no-renames")
	if err != nil {
		return nil, fmt.Errorf("workdir: reading the history: %w", err)
	}
	ages := make(map[string]time.Time)
	var at time.Time
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "\x01") {
			if secs, err := strconv.ParseInt(strings.TrimPrefix(line, "\x01"), 10, 64); err == nil {
				at = time.Unix(secs, 0)
			}
			continue
		}
		if line == "" || at.IsZero() {
			continue
		}
		if _, seen := ages[line]; !seen {
			ages[line] = at
		}
	}
	return ages, nil
}

// langOf reads the language out of root/<lang>/..., for pages below a
// language directory. site/content/fi/_index.md is fi's; a file directly
// under the root belongs to no language and no pairing.
func langOf(rel, root string) (string, bool) {
	rest := strings.TrimPrefix(rel, root+"/")
	lang, _, ok := strings.Cut(rest, "/")
	if !ok || lang == "" {
		return "", false
	}
	return lang, true
}

// translationKey is the page's translationKey, "" when it has none. The front
// matter is scanned as lines rather than parsed as YAML: the one key wanted
// here is a scalar on its own line, and a YAML dependency for it would be the
// heavier way to be exactly as approximate.
func translationKey(abs string) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(io.LimitReader(f, maxFrontMatterBytes))
	inFrontMatter := false
	for sc.Scan() {
		line := sc.Text()
		switch {
		case !inFrontMatter:
			if strings.TrimSpace(line) != "---" {
				return "", nil // no front matter, no key
			}
			inFrontMatter = true
		case strings.TrimSpace(line) == "---":
			return "", nil
		case strings.HasPrefix(line, "translationKey:"):
			v := strings.TrimSpace(strings.TrimPrefix(line, "translationKey:"))
			return strings.Trim(v, `"'`), nil
		}
	}
	return "", sc.Err()
}
