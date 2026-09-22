package workdir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Archiving is the quiet end of a change: its pull request was merged or
// closed on GitHub, so the worktree, the local branch and the build output are
// litter. Nothing on GitHub is touched — that side is already in the state
// review left it in, which is exactly what separates this from Abandon.
//
// It happens wherever the server is talking to GitHub about a person's changes
// anyway: listing them, resuming one, and finding the open-change cap full. A
// change therefore never has to be tidied away by hand once the work in it has
// been published.

// OnArchive registers a function to be told that a change has stopped
// existing. The tool set listens, because a sweep can end the change a
// conversation is in the middle of: a binding that outlives its worktree turns
// every later call in that conversation into a puzzle about a directory nobody
// mentioned.
func (m *Manager) OnArchive(fn func(*Change)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.archived = append(m.archived, fn)
}

// announceArchived tells the listeners, outside the manager's lock: what they
// do with the news is their own business and none of it belongs here.
func (m *Manager) announceArchived(c *Change) {
	m.mu.Lock()
	listeners := append([]func(*Change){}, m.archived...)
	m.mu.Unlock()
	for _, fn := range listeners {
		fn(c)
	}
}

// Archive removes a change's local state and says so. The caller established
// that the change is finished; this only cleans up after it. The listeners are
// told once the change is unlocked and gone, so what one of them does with it
// cannot wait on the removal that prompted it.
func (m *Manager) Archive(ctx context.Context, c *Change) error {
	if err := m.archive(ctx, c); err != nil {
		return err
	}
	m.announceArchived(c)
	return nil
}

func (m *Manager) archive(ctx context.Context, c *Change) error {
	if c == nil {
		return ErrNoChange
	}
	if c.IsBase() {
		return ErrBaseView
	}
	// The same invariant as everywhere else, here because the next few lines
	// delete a branch by name.
	if err := assertNamespace(c.User, c.Branch); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, GitTimeout)
	defer cancel()
	if _, err := m.git(ctx, m.cfg.RepoPath, "worktree", "remove", "--force", c.Dir); err != nil {
		// Whatever git could not remove, os can: everything in the tree is
		// either published or was turned down, and a slot held by a broken
		// worktree helps nobody.
		if rmErr := os.RemoveAll(c.Dir); rmErr != nil {
			return fmt.Errorf("workdir: removing the worktree: %w", err)
		}
		if _, err := m.git(ctx, m.cfg.RepoPath, "worktree", "prune"); err != nil {
			m.log.Warn("workdir: pruning after an archive", "branch", c.Branch, "err", err)
		}
	}
	// The user directory is kept only while it has changes in it.
	_ = os.Remove(filepath.Dir(c.Dir))
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

// ArchiveFinished sweeps one person's open changes against GitHub and archives
// the ones review has finished with, answering the slugs it removed. GitHub
// being unreachable fails the sweep and not the errand the caller was actually
// running, so callers log the error and carry on.
//
// A dry run knows nothing about pull requests and archives nothing; there
// abandon_change stays the only way out of a change.
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
		// change rather than Change: a sweep that hit the cap would start a
		// second sweep, and the cap is one of the things that calls this.
		c, err := m.change(user, slug)
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
		// Work that was never submitted is nobody's litter. A sweep runs behind
		// the person's back, so what it takes away has to be work GitHub already
		// has; edits made after the merge are left where they are, and
		// abandon_change is how somebody says out loud that they are done with
		// them.
		if dirty, err := c.dirtyPaths(ctx); err != nil {
			m.log.Warn("workdir: reading the worktree status in the sweep", "branch", c.Branch, "err", err)
			continue
		} else if len(dirty) > 0 {
			m.log.Info("workdir: sparing a finished change with uncommitted edits", "branch", c.Branch)
			continue
		}
		if err := m.Archive(ctx, c); err != nil {
			return archived, err
		}
		archived = append(archived, slug)
	}
	return archived, nil
}
