# Conversation-Scoped Changes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A new conversation with the content editor MCP starts on a clean checkout; continuing old work is explicit (`resume_change`), and merged work archives itself.

**Architecture:** The toolset's per-user current-change map becomes an idle-expiring session; disk resume is removed. Reads serve from a shared detached worktree of the base branch, so only writes open changes. The workdir manager gains a base view, an archive path driven by pull request state, and PR-number lookup.

**Tech Stack:** Go 1.x, stdlib only (os/exec git, net/http against a fake GitHub in tests). Packages: `proxy/internal/workdir`, `proxy/internal/toolset`.

**Spec:** `docs/superpowers/specs/2026-09-22-mcp-conversation-scoped-changes-design.md` — read it first; every task argues from it.

## Global Constraints

- Idle window: 45 minutes (`SessionIdle = 45 * time.Minute` in toolset).
- The base view lives at `<StateDir>/wt/.base`; the leading dot keeps it outside the username alphabet (`userPattern` in workdir forbids a leading dot).
- Nothing may ever commit, push, submit or abandon the base view.
- Tool names are stable; the new tool is `resume_change`. All schemas are closed objects (`additionalProperties: false`).
- Every git invocation goes through `Manager.git`/`Manager.run` (no shell). Never force-push; `assertNamespace` before any push (unchanged code, keep it that way).
- Archive never touches GitHub: it closes nothing and deletes no remote ref. (`Abandon` keeps doing both; the two must not be merged.)
- In dry-run mode (`Manager.DryRun()`), PR state is unknown: no archiving, `resume_change` accepts only a slug.
- Comments describe the present, never the change history ("no longer", "previously" are forbidden in code and docs).
- Style: match the existing prose-comment style of these packages; run `gofmt -l` and `go vet ./...` in `proxy/` before every commit; all commits on branch `feat/mcp-conversation-scoped-changes`.
- Commit trailer: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

Run tests from the `proxy/` directory: `go test ./internal/workdir/ ./internal/toolset/`.

---

### Task 1: Base view in workdir

A shared read-only detached worktree of the origin default branch, refusing everything write-shaped.

**Files:**
- Create: `proxy/internal/workdir/base.go`
- Create: `proxy/internal/workdir/base_test.go`
- Modify: `proxy/internal/workdir/workdir.go` (guard in `Submit`), `proxy/internal/workdir/feedback.go` (guard in `Abandon`, `Feedback`)

**Interfaces:**
- Consumes: `Manager.git`, `Manager.baseRef`, `Manager.checkClone`, `isWorktree`, `fence.NewFence`, the `Change` struct (all existing).
- Produces (later tasks rely on these exact names):
  - `func (m *Manager) Base() (*Change, error)` — the shared view, created at `<StateDir>/wt/.base` on first use, reattached afterwards. Its `Change` has `User: ".base"`, `Slug: "base"`, `Branch: ""`.
  - `func (m *Manager) RefreshBase(ctx context.Context) error` — fetch origin, hard-reset the view to the base ref.
  - `func (c *Change) IsBase() bool` — true exactly when `c.Branch == ""`.
  - `var ErrBaseView = errors.New("workdir: the base view is read-only; open a change by editing a file")`

- [ ] **Step 1: Write the failing tests**

In `base_test.go` (use the existing `newFixture(t)` and `newManager`-style setup from `workdir_test.go` — read that file first and follow its helpers exactly):

```go
func TestBaseViewReadsTheDefaultBranch(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t) // if workdir_test has no such helper, construct Manager as its other tests do
	b, err := m.Base()
	if err != nil {
		t.Fatalf("Base: %v", err)
	}
	if !b.IsBase() {
		t.Fatal("the base view does not report IsBase")
	}
	if b.User != ".base" || b.Branch != "" {
		t.Fatalf("base view is %q on branch %q", b.User, b.Branch)
	}
	if _, err := os.Stat(filepath.Join(b.Dir, "site", "hugo.toml")); err != nil {
		t.Fatalf("the base view has no checkout: %v", err)
	}
	// A second call reattaches rather than remaking.
	b2, err := m.Base()
	if err != nil || b2 != b {
		t.Fatalf("Base is not idempotent: %v", err)
	}
}

func TestBaseViewRefusesSubmitAndAbandon(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	b, _ := m.Base()
	author := Author{Name: "Test", Email: "t@example.org"}
	if _, err := m.Submit(context.Background(), b, author, "title", ""); !errors.Is(err, ErrBaseView) {
		t.Fatalf("Submit on the base view: %v, want ErrBaseView", err)
	}
	if _, err := m.Abandon(context.Background(), b); !errors.Is(err, ErrBaseView) {
		t.Fatalf("Abandon on the base view: %v, want ErrBaseView", err)
	}
}

func TestRefreshBasePicksUpAMovedOrigin(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	b, _ := m.Base()
	// Move origin/main: commit a new file through a second clone (follow the
	// fixture's own pattern for pushing to f.origin).
	f.pushNewFile(t, "site/content/fi/uusi.md", "# Uusi\n")
	if err := m.RefreshBase(context.Background()); err != nil {
		t.Fatalf("RefreshBase: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.Dir, "site", "content", "fi", "uusi.md")); err != nil {
		t.Fatalf("the refreshed base view is stale: %v", err)
	}
}
```

If the fixture lacks `manager` / `pushNewFile` helpers, add them to `workdir_test.go` following its existing `git`/`commit` helpers.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd proxy && go test ./internal/workdir/ -run 'TestBaseView|TestRefreshBase' -v`
Expected: compile failure — `Base`, `RefreshBase`, `IsBase`, `ErrBaseView` undefined.

- [ ] **Step 3: Implement `base.go`**

```go
// The shared read-only view of the origin default branch. Reads that belong to
// no change serve from here, so a conversation that only looks at the site
// never opens a change. One view for everyone: it holds no edits, so there is
// nothing per-person about it.
package workdir

// (imports as needed)

// baseDirName keeps the view outside every user's directory: userPattern
// refuses a leading dot, so no username can collide with it.
const baseDirName = ".base"

// ErrBaseView answers anything write-shaped asked of the view.
var ErrBaseView = errors.New("workdir: the base view is read-only; open a change by editing a file")

// IsBase reports whether this Change is the shared view. The view is the one
// Change with no branch, and having no branch is exactly why nothing can be
// committed or pushed from it.
func (c *Change) IsBase() bool { return c.Branch == "" }

// Base is the shared view, created on first use and reattached afterwards.
func (m *Manager) Base() (*Change, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.base != nil {
		return m.base, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), GitTimeout)
	defer cancel()
	if err := m.checkClone(ctx); err != nil {
		return nil, err
	}
	base, baseBranch, err := m.baseRef(ctx)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(m.cfg.StateDir, "wt", baseDirName)
	if !isWorktree(dir) {
		if _, err := m.git(ctx, m.cfg.RepoPath, "worktree", "prune"); err != nil {
			m.log.Warn("workdir: pruning worktrees", "err", err)
		}
		if _, err := m.git(ctx, m.cfg.RepoPath, "fetch", "--quiet", "origin"); err != nil {
			m.log.Warn("workdir: fetching origin", "err", err)
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return nil, fmt.Errorf("workdir: making the base view directory: %w", err)
		}
		if _, err := m.git(ctx, m.cfg.RepoPath, "worktree", "add", "--quiet", "--detach", dir, base); err != nil {
			return nil, fmt.Errorf("workdir: opening the base view: %w", err)
		}
	}
	f, err := fence.NewFence(dir)
	if err != nil {
		return nil, fmt.Errorf("workdir: fencing %s: %w", dir, err)
	}
	m.base = &Change{
		User: baseDirName, Slug: "base", Branch: "", Dir: dir,
		fence: f, mgr: m, base: base, baseBranch: baseBranch,
	}
	return m.base, nil
}

// RefreshBase brings the view to origin's current default branch. A hard
// reset, because the view holds no edits worth keeping by definition.
func (m *Manager) RefreshBase(ctx context.Context) error {
	b, err := m.Base()
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, GitTimeout)
	defer cancel()
	if _, err := m.git(ctx, m.cfg.RepoPath, "fetch", "--quiet", "origin"); err != nil {
		m.log.Warn("workdir: fetching origin for the base view", "err", err)
	}
	if _, err := m.git(ctx, b.Dir, "reset", "--hard", "--quiet", b.base); err != nil {
		return fmt.Errorf("workdir: refreshing the base view: %w", err)
	}
	return nil
}
```

Add the field to `Manager` in `workdir.go`: `base *Change // the shared read-only view, made once` (under the existing `mu`).

- [ ] **Step 4: Add the guards**

At the top of `Manager.Submit` (workdir.go), right after the nil check:

```go
	if c.IsBase() {
		return SubmitResult{}, ErrBaseView
	}
```

Same two lines (with each function's zero return) at the top of `Manager.Abandon` and `Manager.Feedback` in feedback.go.

- [ ] **Step 5: Run the tests, then the whole package**

Run: `cd proxy && go test ./internal/workdir/ -v -run 'TestBaseView|TestRefreshBase'` then `go test ./internal/workdir/`
Expected: PASS, and no existing test broken.

- [ ] **Step 6: gofmt, vet, commit**

```bash
cd proxy && gofmt -l . && go vet ./... && cd .. \
&& git add proxy/internal/workdir \
&& git commit -m "Base view: a shared read-only checkout of the default branch" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Self-archiving and PR lookup in workdir

A change whose pull request is merged or closed is archived — worktree, local branch and preview root removed, GitHub untouched — wherever the server already consults GitHub.

**Files:**
- Create: `proxy/internal/workdir/archive.go`
- Create: `proxy/internal/workdir/archive_test.go`
- Modify: `proxy/internal/workdir/workdir.go` (`List` archives first; `createWorktree` archives before refusing on the cap)
- Modify: `proxy/internal/workdir/github.go` (PR by number)

**Interfaces:**
- Consumes: `pullRequestForBranchState(ctx, branch, "all")`, `m.api`, `Manager.git`, `openSlugs`, the `PullRequest` struct (fields `Number`, `HTMLURL`, `State`, `MergedAt`, `Head.Ref`, `Head.SHA` — verify in `github.go`).
- Produces:
  - `func (m *Manager) Archive(ctx context.Context, c *Change) error` — local removal only.
  - `func (m *Manager) ArchiveFinished(ctx context.Context, user string) (archived []string, err error)` — consults GitHub per open change; archives merged/closed; returns their slugs; `(nil, nil)` in dry run.
  - `func (m *Manager) PullRequestByNumber(ctx context.Context, number int) (PullRequest, error)` — `GET /repos/{repo}/pulls/{number}`.
  - `func Finished(pr PullRequest) bool` — merged or closed.

- [ ] **Step 1: Write the failing tests**

In `archive_test.go`, follow the fake-GitHub pattern of `feedback_test.go` (an `httptest.Server` whose handler answers the exact REST paths, wired through `Config.APIRoot`, `GitHubToken: "t"`, `GitHubRepo: "prodeko/site"`):

```go
func TestArchiveFinishedRemovesMergedChanges(t *testing.T) {
	// Fake GitHub: /repos/prodeko/site/pulls?head=prodeko:media/u/merged-one...
	// answers a merged PR ("state":"closed","merged_at":"2026-09-22T10:00:00Z"),
	// the query for media/u/still-open answers an open PR, and the query for
	// media/u/never-pushed answers [].
	// Fixture: three changes for user "u": merged-one, still-open, never-pushed.
	// Call: archived, err := m.ArchiveFinished(ctx, "u")
	// Assert: archived == ["merged-one"]; its worktree dir is gone; its local
	// branch is gone (git rev-parse --verify fails); still-open and
	// never-pushed remain in openSlugs; nothing was DELETEd or PATCHed on the
	// fake server (record methods in the handler and assert only GETs).
}

func TestArchiveFinishedIsNilInDryRun(t *testing.T) {
	// Manager with no token: ArchiveFinished returns (nil, nil) and the
	// worktrees stay.
}

func TestTheCapSelfHeals(t *testing.T) {
	// Three changes open, all with merged PRs on the fake server. A fourth
	// Change(user, "fresh") succeeds because createWorktree archived the
	// finished three instead of refusing.
}

func TestPullRequestByNumber(t *testing.T) {
	// Fake GitHub answers GET /repos/prodeko/site/pulls/7 with head.ref
	// "media/u/thing". Assert the returned PullRequest carries it and that
	// Finished is false for an open PR, true for closed and for merged.
}
```

Write these as real tests, not comments — the comments above specify the behaviour; the fixture plumbing comes from `workdir_test.go` and `feedback_test.go`.

- [ ] **Step 2: Run to verify failure**

Run: `cd proxy && go test ./internal/workdir/ -run 'TestArchive|TestTheCapSelfHeals|TestPullRequestByNumber' -v`
Expected: compile failure — `Archive`, `ArchiveFinished`, `PullRequestByNumber`, `Finished` undefined.

- [ ] **Step 3: Implement**

`github.go`:

```go
// PullRequestByNumber is one pull request, any state. It is how resume_change
// turns a number the person read on GitHub back into a branch.
func (m *Manager) PullRequestByNumber(ctx context.Context, number int) (PullRequest, error) {
	var pr PullRequest
	err := m.api(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d", m.cfg.GitHubRepo, number), nil, &pr)
	return pr, err
}

// Finished reports work that review has ended: merged into the site, or
// closed without merging. Either way the change under it is done.
func Finished(pr PullRequest) bool {
	return pr.MergedAt != "" || pr.State == "closed"
}
```

`archive.go`:

```go
// Archiving is the quiet end of a change: the pull request was merged or
// closed on GitHub, so the local worktree, branch and preview are litter.
// Nothing on GitHub is touched — that side is already in its final state,
// which is exactly what distinguishes this from Abandon.

// Archive removes a change's local state. The caller established that the
// change is finished; this function only cleans.
func (m *Manager) Archive(ctx context.Context, c *Change) error {
	if c == nil {
		return ErrNoChange
	}
	if c.IsBase() {
		return ErrBaseView
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, GitTimeout)
	defer cancel()
	if _, err := m.git(ctx, m.cfg.RepoPath, "worktree", "remove", "--force", c.Dir); err != nil {
		if rmErr := os.RemoveAll(c.Dir); rmErr != nil {
			return fmt.Errorf("workdir: removing the worktree: %w", err)
		}
		if _, err := m.git(ctx, m.cfg.RepoPath, "worktree", "prune"); err != nil {
			m.log.Warn("workdir: pruning after an archive", "branch", c.Branch, "err", err)
		}
	}
	_ = os.Remove(filepath.Dir(c.Dir)) // kept only while it has changes in it
	if _, err := m.git(ctx, m.cfg.RepoPath, "branch", "-D", c.Branch); err != nil {
		m.log.Warn("workdir: deleting the archived local branch", "branch", c.Branch, "err", err)
	}
	if err := os.RemoveAll(c.previewRoot()); err != nil {
		m.log.Warn("workdir: removing the archived preview", "branch", c.Branch, "err", err)
	}
	m.mu.Lock()
	delete(m.changes, c.Branch)
	m.mu.Unlock()
	m.log.Info("workdir: change archived", "user", c.User, "branch", c.Branch)
	return nil
}

// ArchiveFinished sweeps one person's open changes against GitHub and
// archives the finished ones. GitHub being unreachable fails the sweep, not
// the caller's actual errand, so callers log the error and move on.
func (m *Manager) ArchiveFinished(ctx context.Context, user string) ([]string, error) {
	if m.DryRun() {
		return nil, nil
	}
	slugs, err := m.openSlugs(user)
	if err != nil {
		return nil, err
	}
	var archived []string
	for _, slug := range slugs {
		c, err := m.Change(user, slug)
		if err != nil {
			m.log.Warn("workdir: skipping an unreadable change in the sweep", "user", user, "slug", slug, "err", err)
			continue
		}
		pr, ok, err := m.pullRequestForBranchState(ctx, c.Branch, "all")
		if err != nil {
			return archived, err
		}
		if !ok || !Finished(pr) {
			continue
		}
		if err := m.Archive(ctx, c); err != nil {
			return archived, err
		}
		archived = append(archived, slug)
	}
	return archived, nil
}
```

Note: `Archive` takes `GitTimeout` around git work only; `ArchiveFinished` passes its own ctx to the API calls as the existing code does.

- [ ] **Step 4: Wire the sweep in**

In `Manager.List` (workdir.go), after validating the user and before `openSlugs`:

```go
	if _, err := m.ArchiveFinished(ctx, user); err != nil {
		m.log.Warn("workdir: sweeping finished changes", "user", user, "err", err)
	}
```

In `createWorktree`, replace the cap refusal:

```go
	open, err := m.openSlugs(user)
	if err != nil {
		return err
	}
	if len(open) >= MaxOpenChanges {
		if _, err := m.ArchiveFinished(ctx, user); err != nil {
			m.log.Warn("workdir: sweeping finished changes", "user", user, "err", err)
		}
		if open, err = m.openSlugs(user); err != nil {
			return err
		}
	}
	if len(open) >= MaxOpenChanges {
		return fmt.Errorf("%w: %s already has %d open (%s); submit or abandon one first",
			ErrTooManyOpen, user, len(open), strings.Join(open, ", "))
	}
```

Watch for recursion: `ArchiveFinished` calls `m.Change`, which can re-enter worktree creation only for a slug already on disk (`isWorktree` true), so `createWorktree` is not re-entered. Also note `m.mu` is held by `Change()` when `createWorktree` runs — `ArchiveFinished` → `m.Change` would deadlock on `m.mu`. Resolve this by having `ArchiveFinished` take an unexported variant that assumes the lock, or by looking up worktrees without `m.Change`: read `openSlugs`, build the branch name with `BranchFor`, check `m.changes` map for an existing `*Change`, and construct a minimal one (dir, branch, fence) when absent. Choose the smaller change and write a test that would deadlock if wrong (`TestTheCapSelfHeals` covers it — run with `-timeout 60s`).

- [ ] **Step 5: Run the tests**

Run: `cd proxy && go test ./internal/workdir/ -timeout 120s`
Expected: PASS, including `TestTheCapSelfHeals` (the deadlock canary) and every pre-existing test.

- [ ] **Step 6: gofmt, vet, commit**

```bash
cd proxy && gofmt -l . && go vet ./... && cd .. \
&& git add proxy/internal/workdir \
&& git commit -m "Archive finished changes wherever GitHub is consulted" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Idle-expiring sessions in the toolset

The per-user binding becomes a session with a 45-minute idle expiry; disk resume goes away; reads without a bound change serve from the base view.

**Files:**
- Create: `proxy/internal/toolset/session.go`
- Create: `proxy/internal/toolset/session_test.go`
- Modify: `proxy/internal/toolset/toolset.go` (every handler; delete `open`/`openUser`'s resume path)
- Modify: `proxy/internal/workdir/workdir.go` (delete `Resume`), `proxy/internal/workdir/workdir_test.go` (delete its tests)

**Interfaces:**
- Consumes: `Manager.Base()`, `Manager.RefreshBase(ctx)` (Task 1), `workdir.Slug`, `Manager.Change`, `Manager.Existing`.
- Produces (Task 4 and 5 rely on these exact names):
  - `const SessionIdle = 45 * time.Minute`
  - `type session struct { change *workdir.Change; lastUsed time.Time; notePending bool; baseFresh bool }`
  - `func (t *Toolset) sessionFor(user string) (*session, bool)` — returns the live session and whether it was freshly started; starting one sets `notePending: true`.
  - `func (t *Toolset) reading(ctx context.Context, id mcpserver.Identity) (*workdir.Change, error)` — the bound change, else the base view (refreshed once per session).
  - `func (t *Toolset) writing(id mcpserver.Identity, hint string) (*workdir.Change, error)` — the bound change, else a freshly opened one, which becomes bound.
  - `func (t *Toolset) bind(user string, c *workdir.Change)` — sets the session's change (used by resume_change in Task 4).

- [ ] **Step 1: Write the failing tests**

`session_test.go` needs a git-backed fixture. `testToolset` in `toolset_test.go` builds a Manager on bare temp dirs; check `preview_test.go` for a git-backed toolset fixture first and reuse it. If none exists, build one modelled on workdir's `newFixture` (bare origin, seeded site with `site/hugo.toml` and one content file, clone as RepoPath). Inject the clock through `Config.Now`.

```go
func TestANewSessionStartsOnTheBase(t *testing.T) {
	// read_file through the toolset with no prior write answers the base
	// checkout's content, and openSlugs-visible worktrees for the user stay
	// absent: a read-only conversation mints no change.
}

func TestTheFirstWriteOpensAFreshChange(t *testing.T) {
	// write_file opens a change; a second write lands in the same one
	// (list_my_changes shows one change with both files).
}

func TestAnIdleSessionExpires(t *testing.T) {
	// now = t0: write_file binds change A.
	// now = t0 + 46min: write_file opens change B, not A.
	// now = t0 + 46min + 1min: a third write still lands in B (the fresh
	// session's clock started at second write).
}

func TestActivityKeepsASessionAlive(t *testing.T) {
	// Writes at t0, t0+30m, t0+60m all land in one change: each call
	// refreshed lastUsed.
}

func TestReadsFollowTheBoundChange(t *testing.T) {
	// After write_file creates site/content/fi/x.md in the bound change,
	// read_file("site/content/fi/x.md") answers it (the base view does not
	// have the file).
}
```

Also in this task's tests: `submit` with no bound change and no open changes answers prose containing "no change" and does not error as a transport failure; `get_feedback` with no slug and no bound change likewise.

- [ ] **Step 2: Run to verify failure**

Run: `cd proxy && go test ./internal/toolset/ -run 'TestANewSession|TestTheFirstWrite|TestAnIdleSession|TestActivity|TestReadsFollow' -v`
Expected: FAIL — behaviour not implemented (reads currently open changes).

- [ ] **Step 3: Implement `session.go`**

```go
// A session is one working stretch: the change being edited, if any, and when
// the person was last heard from. It expires by going idle, which is the only
// conversation boundary a stateless transport lets this server see. One
// person's simultaneous conversations share a session — the server cannot
// tell them apart without MCP session ids.
package toolset

// SessionIdle is how long a session survives silence. Longer than a coffee
// break, shorter than "yesterday": a person answering review feedback the
// next morning starts fresh and resumes deliberately.
const SessionIdle = 45 * time.Minute

type session struct {
	change      *workdir.Change // nil until the first write or resume_change
	lastUsed    time.Time
	notePending bool // the open-changes note has not been delivered
	baseFresh   bool // the base view was refreshed for this session
}

// sessionFor returns the caller's live session, starting one when none is
// live. The second return says a session was started, which is what the
// proactive note and the base refresh key on.
func (t *Toolset) sessionFor(user string) (*session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	s, ok := t.sessions[user]
	if ok && now.Sub(s.lastUsed) <= SessionIdle {
		s.lastUsed = now
		return s, false
	}
	s = &session{lastUsed: now, notePending: true}
	t.sessions[user] = s
	return s, true
}

// reading is the tree a read serves from: the bound change, or the base view.
func (t *Toolset) reading(ctx context.Context, id mcpserver.Identity) (*workdir.Change, error) {
	user, err := userOf(id)
	if err != nil {
		return nil, err
	}
	s, _ := t.sessionFor(user)
	if s.change != nil {
		return s.change, nil
	}
	if !s.baseFresh {
		if err := t.mgr.RefreshBase(ctx); err != nil {
			t.log.Warn("toolset: refreshing the base view", "err", err)
		}
		t.mu.Lock()
		s.baseFresh = true
		t.mu.Unlock()
	}
	return t.mgr.Base()
}

// writing is the tree an edit lands in: the bound change, or a fresh one
// named after what was asked for, which becomes the session's change.
func (t *Toolset) writing(id mcpserver.Identity, hint string) (*workdir.Change, error) {
	user, err := userOf(id)
	if err != nil {
		return nil, err
	}
	return t.writingUser(user, hint)
}

// writingUser is writing for a caller that already holds the namespaced
// username: the upload handler.
func (t *Toolset) writingUser(user, hint string) (*workdir.Change, error) {
	s, _ := t.sessionFor(user)
	if s.change != nil {
		return s.change, nil
	}
	c, err := t.mgr.Change(user, workdir.Slug(hint, t.now()))
	if err != nil {
		return nil, err
	}
	t.bind(user, c)
	return c, nil
}

// bind sets the session's change. resume_change and the first write are the
// only callers; nothing else may decide what a session is working on.
func (t *Toolset) bind(user string, c *workdir.Change) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.sessions[user]; ok {
		s.change = c
		s.lastUsed = t.now()
	}
}
```

- [ ] **Step 4: Rewire the toolset**

In `toolset.go`:
- Struct: replace `current map[string]*workdir.Change` with `sessions map[string]*session`; initialise in `New`.
- Delete `open` and `openUser`; update every caller:
  - `listFiles`, `readFile`, `search`, `build`, `render`, `screenshot`, `translationStatus`: `c, err := t.reading(ctx, id)` (thread ctx; `screenshot` and `build` already have it, the others receive it).
  - `writeFile`, `editFile`: `c, err := t.writing(id, hintFor(rel))`.
  - `submit`: no implicit open. Read the session:
    ```go
    user, err := userOf(id)
    // ...
    s, _ := t.sessionFor(user)
    if s.change == nil {
        return fmt.Sprintf("No change is open in this conversation, so there is nothing to submit. "+
            "An edit opens one; %s continues an existing one.", ToolResumeChange), nil
    }
    c := s.change
    ```
    (Until Task 4 defines `ToolResumeChange`, spell the string `"resume_change"` and switch to the constant in Task 4.)
  - `getFeedback` with an empty slug: same shape — bound change or the sentence above adapted ("nothing to read feedback on").
  - `change(id, slug)` helper: empty slug → bound change or that refusal; named slug → `t.mgr.Existing(user, slug)` (unchanged).
  - `abandonChange`: clear the binding through the sessions map (`if s, ok := t.sessions[user]; ok && s.change == c { s.change = nil }`).
  - `beginImageUpload` and `SaveImage`: `t.writingUser(user, ...)`.
- `Manager.Build` refuses nothing new — the base view builds fine into `preview/.base/base/build`, which is what render/screenshot read. No change needed there; confirm `previewRoot` produces that path.

In `workdir.go`: delete `Manager.Resume` and its comment block; delete its tests in `workdir_test.go` (`grep -n Resume` to find them).

- [ ] **Step 5: Run everything**

Run: `cd proxy && go test ./internal/... -timeout 180s`
Expected: PASS. Pre-existing toolset tests that asserted reads open changes must be updated to the new behaviour in this task, deliberately, not deleted (each keeps testing its tool's contract; only the workspace expectation moves).

- [ ] **Step 6: gofmt, vet, commit**

```bash
cd proxy && gofmt -l . && go vet ./... && cd .. \
&& git add proxy/internal \
&& git commit -m "Sessions: an idle-expiring binding replaces disk resume" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: The resume_change tool

Reattach to an open change by slug or pull request number; refuse and archive finished ones.

**Files:**
- Modify: `proxy/internal/toolset/schema.go` (constant, schema, args type)
- Modify: `proxy/internal/toolset/toolset.go` (tool registration + handler)
- Modify: `proxy/internal/toolset/render.go` (`renderResume`)
- Test: `proxy/internal/toolset/session_test.go` (extend)

**Interfaces:**
- Consumes: `Manager.Existing`, `Manager.Change`, `Manager.PullRequestByNumber`, `workdir.Finished`, `Manager.Archive`, `pull request state via Manager.List`'s pattern, `t.bind` (Task 3), `workdir.BranchPrefix`.
- Produces: `ToolResumeChange = "resume_change"`; the tool appears in `Tools()` after `list_my_changes`.

- [ ] **Step 1: Write the failing tests**

```go
func TestResumeBySlugRebinds(t *testing.T) {
	// Open change A by writing, expire the session (advance the clock past
	// SessionIdle), call resume_change{"slug":"..."} — the next write lands
	// in A, not in a fresh change.
}

func TestResumeByPRNumber(t *testing.T) {
	// Fake GitHub: GET /pulls/7 answers head.ref media/<user>/<slug of A>,
	// state open. resume_change{"pr":7} rebinds to A.
}

func TestResumeRefusesAnotherUsersPR(t *testing.T) {
	// GET /pulls/8 answers head.ref "media/somebody-else/thing".
	// resume_change{"pr":8} answers an error naming the namespace, and no
	// binding changes.
}

func TestResumeRefusesAndArchivesAFinishedChange(t *testing.T) {
	// Change A submitted (or its branch pushed in the fixture), fake GitHub
	// says merged. resume_change{"slug":A} answers prose containing
	// "finished"; A's worktree is gone afterwards.
}

func TestResumeArgsAreExactlyOne(t *testing.T) {
	// {} and {"slug":"x","pr":1} both refuse with a sentence naming the rule.
}

func TestResumeByPRInDryRun(t *testing.T) {
	// No token: {"pr":7} answers prose saying only a slug works here.
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd proxy && go test ./internal/toolset/ -run TestResume -v`
Expected: compile failure — `ToolResumeChange` undefined.

- [ ] **Step 3: Schema and args**

In `schema.go`:

```go
ToolResumeChange = "resume_change" // in the const block

var schemaResumeChange = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "slug": {
      "type": "string",
      "description": "Which change to continue, as list_my_changes names them. Give exactly one of slug and pr.",
      "maxLength": 64
    },
    "pr": {
      "type": "integer",
      "description": "The pull request number to continue from, e.g. 26 for a change whose review asked for fixes.",
      "minimum": 1
    }
  },
  "additionalProperties": false
}`)

type resumeChangeArgs struct {
	Slug string `json:"slug"`
	PR   int    `json:"pr"`
}
```

- [ ] **Step 4: Handler and registration**

In `toolset.go`, register after `list_my_changes`:

```go
{
	Name: ToolResumeChange,
	Description: "Continue an existing change instead of starting a new one: by slug as list_my_changes names " +
		"them, or by pull request number. Edits then land on that change's branch and pull request. A merged or " +
		"closed change is finished and cannot be resumed.",
	Schema: schemaResumeChange,
	Call:   text(t.resumeChange),
},
```

Handler:

```go
func (t *Toolset) resumeChange(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (string, error) {
	a, err := decode[resumeChangeArgs](args)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ToolResumeChange, err)
	}
	if (a.Slug == "") == (a.PR == 0) {
		return "", fmt.Errorf("%s: give exactly one of slug and pr; %s lists the slugs", ToolResumeChange, ToolListMyChanges)
	}
	user, err := userOf(id)
	if err != nil {
		return "", err
	}

	slug := strings.TrimSpace(a.Slug)
	if a.PR != 0 {
		if t.mgr.DryRun() {
			return "", fmt.Errorf("%s: this server has no GitHub access, so a pull request number cannot be looked up; use the slug from %s", ToolResumeChange, ToolListMyChanges)
		}
		pr, err := t.mgr.PullRequestByNumber(ctx, a.PR)
		if err != nil {
			return "", err
		}
		prefix := workdir.BranchPrefix + user + "/"
		if !strings.HasPrefix(pr.Head.Ref, prefix) {
			return "", fmt.Errorf("%s: pull request #%d is %s, which is not one of your changes", ToolResumeChange, a.PR, pr.Head.Ref)
		}
		slug = strings.TrimPrefix(pr.Head.Ref, prefix)
	}

	c, err := t.mgr.Existing(user, slug)
	if errors.Is(err, workdir.ErrNoChange) && a.PR != 0 {
		// The pull request proves the branch exists; a lost worktree is
		// reattached from it.
		c, err = t.mgr.Change(user, slug)
	}
	if err != nil {
		return "", err
	}

	if !t.mgr.DryRun() {
		if fb, err := t.mgr.Feedback(ctx, c); err == nil && (fb.State == "merged" || fb.State == "closed") {
			if err := t.mgr.Archive(ctx, c); err != nil {
				t.log.Warn("toolset: archiving a finished change", "slug", slug, "err", err)
			}
			return fmt.Sprintf("%s is finished: its pull request #%d was %s. It has been tidied away; your next edit starts a new change.",
				slug, fb.PRNumber, fb.State), nil
		}
	}

	t.bind(user, c)
	return renderResume(c, t.mgr, ctx)
}
```

(`Feedback` is the existing state lookup; if pulling full feedback is too heavy, use `pullRequestForBranchState` through a small exported wrapper instead — but prefer reusing what exists. Adjust to what compiles cleanly; the contract is: merged/closed → archive + the sentence; open or never-submitted → bind + summary.)

- [ ] **Step 5: renderResume**

In `render.go`, following its style (read `renderChanges` first):

```go
// renderResume says what the person just picked up: the state a resumed
// change is in is the first thing to act on.
func renderResume(c *workdir.Change, m *workdir.Manager, ctx context.Context) (string, error) {
	// Lines: "Resumed <slug> (branch <branch>)."
	// Files touched (c.Touched()), whether uncommitted edits exist, and when
	// not in dry run: PR number, URL, CI state, preview URL via the same
	// lookups List uses. Close with: "Edits now land on this change."
}
```

Write the real implementation, matching `renderChanges`' tone and helpers.

- [ ] **Step 6: Run tests, whole package**

Run: `cd proxy && go test ./internal/toolset/ -timeout 180s -v -run TestResume` then `go test ./internal/...`
Expected: PASS. Update `TestTheFifteenTools` (now sixteen) — rename it honestly (`TestTheSixteenTools`) and adjust the count and comment.

- [ ] **Step 7: gofmt, vet, commit**

```bash
cd proxy && gofmt -l . && go vet ./... && cd .. \
&& git add proxy/internal/toolset \
&& git commit -m "resume_change: continue a change by slug or pull request" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: The proactive note

The first tool answer of a session mentions the person's open changes.

**Files:**
- Modify: `proxy/internal/toolset/session.go` (note computation + delivery wrapper)
- Modify: `proxy/internal/toolset/toolset.go` (wrap registrations)
- Test: `proxy/internal/toolset/session_test.go` (extend)

**Interfaces:**
- Consumes: `Manager.List` (which archives finished changes as it lists — Task 2), `session.notePending` (Task 3), `renderChanges`-style formatting.
- Produces: `func (t *Toolset) noted(fn func(context.Context, mcpserver.Identity, json.RawMessage) (mcpserver.Result, error)) func(...) same` — a wrapper applied to every tool's `Call` in `Tools()`.

- [ ] **Step 1: Write the failing tests**

```go
func TestTheNoteAppearsOnceASession(t *testing.T) {
	// User has one open change (from an earlier session; expire the clock).
	// First call of the new session (a read_file) carries "open change" and
	// the slug in its text; the second call does not.
}

func TestNoNoteWithoutOpenChanges(t *testing.T) {
	// Fresh user, first call: no note appended.
}

func TestListMyChangesSwallowsTheNote(t *testing.T) {
	// First call of a session is list_my_changes: its answer is the listing
	// alone, and the note is spent (a following read carries none).
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd proxy && go test ./internal/toolset/ -run 'TestTheNote|TestNoNote|TestListMyChanges' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `session.go`:

```go
// note is the once-per-session mention of open changes. It rides on the first
// answer rather than being its own message, because a stateless transport has
// no way to speak first.
func (t *Toolset) note(ctx context.Context, id mcpserver.Identity) string {
	user, err := userOf(id)
	if err != nil {
		return ""
	}
	t.mu.Lock()
	s, ok := t.sessions[user]
	pending := ok && s.notePending
	if pending {
		s.notePending = false // spent whether or not anything is appended
	}
	t.mu.Unlock()
	if !pending {
		return ""
	}
	infos, err := t.mgr.List(ctx, user)
	if err != nil || len(infos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nNote: you have open changes from before this conversation:\n")
	for _, in := range infos {
		// one line: slug, file count, dirty marker, PR number and CI when known
	}
	fmt.Fprintf(&b, "%s continues one of them; otherwise your first edit starts a new change.", ToolResumeChange)
	return b.String()
}

// noted appends the session's note to a tool's first answer.
func (t *Toolset) noted(fn func(context.Context, mcpserver.Identity, json.RawMessage) (mcpserver.Result, error)) func(context.Context, mcpserver.Identity, json.RawMessage) (mcpserver.Result, error) {
	return func(ctx context.Context, id mcpserver.Identity, args json.RawMessage) (mcpserver.Result, error) {
		res, err := fn(ctx, id, args)
		if err != nil {
			return res, err // a refusal is confusing enough without a postscript
		}
		if n := t.note(ctx, id); n != "" {
			res.Text += n
		}
		return res, nil
	}
}
```

In `Tools()`, wrap every `Call`: `Call: t.noted(text(t.listFiles))`, `Call: t.noted(t.screenshot)`, and so on — every tool. For `list_my_changes`, spend the note without appending: in its handler, call `t.spendNote(user)` (a three-line helper that clears `notePending`) before answering, and register it wrapped like the others (the wrapper then finds the note spent).

Note ordering: the session is created by the tool body (via `reading`/`writing`/`sessionFor`), which runs before the wrapper's `t.note` — so the note is delivered on the same first call. `get_conventions` never touches a session; that is fine — the note waits for the first call that does.

- [ ] **Step 4: Run tests**

Run: `cd proxy && go test ./internal/toolset/ -timeout 180s`
Expected: PASS, existing tests included (some assert exact tool answers — the note only appears when an *earlier* session left changes behind, which fresh-fixture tests do not trigger).

- [ ] **Step 5: gofmt, vet, commit**

```bash
cd proxy && gofmt -l . && go vet ./... && cd .. \
&& git add proxy/internal/toolset \
&& git commit -m "Say once per session which changes are open" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Conventions, docs, and the full gate

**Files:**
- Modify: `proxy/internal/toolset/conventions.go`
- Modify: `docs/superpowers/specs/2026-09-19-content-editor-mcp-design.md` (only if it states the resumed-workspace behaviour as current — check; if its lifecycle section contradicts the new one, rewrite that section to describe the present)
- Modify: `plugin/skills/` — `grep -rn "resume\|workspace\|change" plugin/skills/editing-prodeko-site/` and align any workflow guidance with fresh-by-default + `resume_change`

**Interfaces:** none produced; this task makes the words match the behaviour.

- [ ] **Step 1: Update the conventions text**

Read `conventions.go` whole. Add (or rewrite the change-lifecycle paragraph to say): a conversation starts on a clean checkout of the published site; the first edit opens a change; `resume_change` continues an existing change by slug or pull request number; a person saying "jatka" or asking to fix review feedback is the cue to call `list_my_changes` and offer to resume rather than edit afresh; merged and closed changes disappear on their own. Keep the document's voice.

- [ ] **Step 2: Check the sibling docs and skill**

The editing-prodeko-site skill (`plugin/skills/`) documents submit/list behaviour for models using the server — update its workspace guidance to match. State the present; no history.

- [ ] **Step 3: The full gate**

Run: `cd proxy && gofmt -l . && go vet ./... && go test ./... -timeout 300s`
Expected: nothing from gofmt, nothing from vet, all packages PASS (mcpserver, fence, preview and friends included — they must be untouched but run them all).

- [ ] **Step 4: Commit**

```bash
git add proxy docs plugin \
&& git commit -m "Teach the conventions the fresh-by-default lifecycle" -m "Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Self-Review (done at planning time)

- Spec coverage: binding+expiry → Task 3; session-start definition → Task 3 (`sessionFor`) and Task 5 (note timing); base view incl. build root → Task 1 + Task 3 step 4; `resume_change` incl. dry-run and namespace refusal → Task 4; self-archiving at List/resume/cap → Task 2 (+ Task 4 for resume); proactive note once per session → Task 5; conventions guidance → Task 6; testing list → distributed per task.
- Known judgment calls left to the implementer, named here so they are decisions rather than accidents: the `m.mu` re-entrancy resolution in Task 2 step 4, and whether Task 4 reads PR state through `Feedback` or a lighter wrapper.
