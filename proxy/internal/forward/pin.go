package forward

import (
	"encoding/json"
	"net/http"
	"strings"
)

// bodyRule says what has to happen to a request body before it is forwarded.
type bodyRule int

const (
	bodyPassThrough bodyRule = iota // stream it, do not buffer
	bodyAuthor                      // rewrite: inject author and committer
	bodyRefCreate                   // inspect: pin the new ref to cms/*
	bodyRefUpdate                   // inspect: no force pushes over the branch
	bodyPullCreate                  // inspect: pin base and head
	bodyPullUpdate                  // inspect: pin base if it is being changed
)

// classifyBody decides whether subPath's body must be buffered, and why. The
// second return value is the ref name from the path, only set for bodyRefUpdate.
func classifyBody(method, subPath string) (bodyRule, string) {
	m := strings.ToUpper(strings.TrimSpace(method))
	p := strings.Trim(subPath, "/")
	seg := strings.Split(p, "/")

	switch {
	case AcceptsAuthor(m, p):
		return bodyAuthor, ""

	case m == http.MethodPost && p == "git/refs":
		return bodyRefCreate, ""

	case m == http.MethodPatch && len(seg) >= 4 && seg[0] == "git" && seg[1] == "refs":
		name, err := unescapeJoin(seg[3:])
		if err != nil {
			// Allow already rejected this; be conservative anyway.
			return bodyRefUpdate, ""
		}
		return bodyRefUpdate, name

	case m == http.MethodPost && p == "pulls":
		return bodyPullCreate, ""

	case m == http.MethodPatch && len(seg) == 2 && seg[0] == "pulls":
		return bodyPullUpdate, ""
	}

	return bodyPassThrough, ""
}

func (r bodyRule) buffers() bool { return r != bodyPassThrough }

// checkBody applies the rule's pinning. It never mutates; InjectAuthor does the
// one rewrite there is.
func (h *Handler) checkBody(rule bodyRule, refName string, body []byte) Decision {
	switch rule {
	case bodyRefCreate:
		return pinRefCreate(body)
	case bodyRefUpdate:
		return pinRefUpdate(body, refName, h.branch)
	case bodyPullCreate:
		return pinPullCreate(body, h.owner, h.branch)
	case bodyPullUpdate:
		return pinPullUpdate(body, h.branch)
	}
	return allowed()
}

// pinRefCreate restricts POST /git/refs to the editorial workflow's own branch
// namespace. The ref name travels in the body here, so the path allowlist
// cannot see it.
func pinRefCreate(body []byte) Decision {
	var req struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return denied("request body is not valid JSON")
	}
	want := "refs/heads/" + cmsBranchPrefix
	if !strings.HasPrefix(req.Ref, want) || len(req.Ref) == len(want) {
		return denied("new branches must be named refs/heads/%s*", cmsBranchPrefix)
	}
	return allowed()
}

// pinRefUpdate keeps force pushes off the default branch. Decap only ever
// fast-forwards it; force is reserved for cms/* branches during a rebase.
func pinRefUpdate(body []byte, refName, branch string) Decision {
	var req struct {
		Force *bool `json:"force"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return denied("request body is not valid JSON")
	}
	if strings.HasPrefix(refName, cmsBranchPrefix) {
		return allowed()
	}
	if refName != branch {
		return denied("only the configured branch and refs/heads/%s* may be updated", cmsBranchPrefix)
	}
	if req.Force != nil && *req.Force {
		return denied("the configured branch may not be force updated")
	}
	return allowed()
}

// pinPullCreate stops a modified browser opening a pull request from somewhere
// other than one of our own cms/* branches, or targeting a branch other than
// the configured one.
func pinPullCreate(body []byte, owner, branch string) Decision {
	var req struct {
		Base string `json:"base"`
		Head string `json:"head"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return denied("request body is not valid JSON")
	}
	if req.Base != branch {
		return denied("pull requests must target the configured branch")
	}
	// Decap builds head as "{owner}:{branch}", taking the owner from the repo
	// response so its capitalisation is GitHub's, not ours.
	head := req.Head
	if headOwner, rest, found := strings.Cut(head, ":"); found {
		if !strings.EqualFold(headOwner, owner) {
			return denied("pull request head must come from the pinned repository")
		}
		head = rest
	}
	if !strings.HasPrefix(head, cmsBranchPrefix) {
		return denied("pull requests must come from a refs/heads/%s* branch", cmsBranchPrefix)
	}
	return allowed()
}

// pinPullUpdate allows the state and title changes Decap makes, but not
// retargeting an open pull request at a different base.
func pinPullUpdate(body []byte, branch string) Decision {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return denied("request body is not valid JSON")
	}
	raw, ok := fields["base"]
	if !ok {
		return allowed()
	}
	var base string
	if err := json.Unmarshal(raw, &base); err != nil {
		return denied("base must be a string")
	}
	if base != branch {
		return denied("pull requests must target the configured branch")
	}
	return allowed()
}
