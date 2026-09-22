package toolset

import (
	"context"
	"time"

	"github.com/prodeko/prodeko-hack/proxy/internal/mcpserver"
	"github.com/prodeko/prodeko-hack/proxy/internal/workdir"
)

// A session is one working stretch: the change being edited, if any, and when
// the person was last heard from. It expires by going idle, which is the only
// conversation boundary a stateless transport lets this server see.
//
// One person's simultaneous conversations share a session. The server cannot
// tell them apart without MCP session ids, and binding by person is the shape
// a session key would slot into.

// SessionIdle is how long a session survives silence. Longer than a coffee
// break, shorter than "yesterday": a person answering review feedback the next
// morning starts fresh and picks the old change up deliberately.
const SessionIdle = 45 * time.Minute

// session is what one person's conversation is working on. Every field is read
// and written under the tool set's mutex: two calls from the same person can
// be in flight at once, and the binding is the thing they would disagree
// about.
type session struct {
	change      *workdir.Change // nil until the first write or resume_change
	lastUsed    time.Time
	notePending bool // the open-changes note has not been delivered
	baseFresh   bool // the base view was brought up to date for this session
}

// sessionFor returns the caller's live session, starting one when none is.
// The second return says a session was started, which is what the note and the
// base refresh key on.
func (t *Toolset) sessionFor(user string) (*session, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	if s, ok := t.sessions[user]; ok && now.Sub(s.lastUsed) <= SessionIdle {
		s.lastUsed = now
		return s, false
	}
	s := &session{lastUsed: now, notePending: true}
	t.sessions[user] = s
	return s, true
}

// reading is the tree a read serves from: the change this conversation is
// working on, or the shared view of the published site when it is working on
// nothing yet. Looking at the site therefore costs no change at all.
func (t *Toolset) reading(ctx context.Context, id mcpserver.Identity) (*workdir.Change, error) {
	user, err := userOf(id)
	if err != nil {
		return nil, err
	}
	s, _ := t.sessionFor(user)

	t.mu.Lock()
	c := s.change
	refresh := c == nil && !s.baseFresh
	if refresh {
		s.baseFresh = true
	}
	t.mu.Unlock()

	if c != nil {
		return c, nil
	}
	if refresh {
		// Once a session, because origin moves when other people's work is
		// merged and a conversation starting today must not read yesterday's
		// site. A refresh that failed leaves a stale view, which is still an
		// answer; the read goes on.
		if err := t.mgr.RefreshBase(ctx); err != nil {
			t.log.Warn("toolset: refreshing the base view", "err", err)
		}
	}
	return t.mgr.Base()
}

// writing is the tree an edit lands in: the change this conversation is
// working on, or a fresh one named after what was asked for, which becomes the
// conversation's change.
func (t *Toolset) writing(id mcpserver.Identity, hint string) (*workdir.Change, error) {
	user, err := userOf(id)
	if err != nil {
		return nil, err
	}
	return t.writingUser(user, hint)
}

// writingUser is writing for a caller that already holds the namespaced
// username: the upload handler, whose token carries it instead of a bearer
// identity.
func (t *Toolset) writingUser(user, hint string) (*workdir.Change, error) {
	if c := t.boundChange(user); c != nil {
		return c, nil
	}
	c, err := t.mgr.Change(user, workdir.Slug(hint, t.now()))
	if err != nil {
		return nil, err
	}
	t.bind(user, c)
	return c, nil
}

// boundChange is the change this conversation is working on, or nil when it
// has not opened one. A tool that needs a change and is given no name answers
// with that nil rather than opening something nobody asked for.
func (t *Toolset) boundChange(user string) *workdir.Change {
	s, _ := t.sessionFor(user)
	t.mu.Lock()
	defer t.mu.Unlock()
	return s.change
}

// bind sets the session's change. The first write and resume_change are the
// only callers: nothing else decides what a conversation is working on.
func (t *Toolset) bind(user string, c *workdir.Change) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.sessions[user]; ok {
		s.change = c
		s.lastUsed = t.now()
	}
}

// unbind drops the change a conversation was working on, so the next edit
// starts fresh instead of landing in a worktree that is gone.
func (t *Toolset) unbind(user string, c *workdir.Change) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.sessions[user]; ok && s.change == c {
		s.change = nil
	}
}

// noChange is the answer to a tool that needs the conversation's change when
// there is none. A conversation starts on a clean checkout, so having nothing
// open is its ordinary state and not a failure worth an error.
func noChange(errand string) string {
	return "No change is open in this conversation, so there is nothing to " + errand +
		". An edit opens one; " + ToolResumeChange + " continues an existing one."
}
