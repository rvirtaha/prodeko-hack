package forward

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// cmsBranchPrefix is the branch namespace Decap's editorial workflow owns.
// Every branch it creates is named cms/<collection>/<slug>.
const cmsBranchPrefix = "cms/"

// Decision is the outcome of an allowlist check.
type Decision struct {
	Allowed bool
	Reason  string // human-readable; returned in the 403 body and logged
}

func allowed() Decision { return Decision{Allowed: true} }

func denied(format string, args ...any) Decision {
	return Decision{Reason: fmt.Sprintf(format, args...)}
}

// Allow reports whether method may be forwarded for subPath, the
// percent-encoded path below /repos/{owner}/{repo} ("" means the repo itself).
// branch is the configured default branch, used to pin ref writes.
//
// Callers must pass the escaped path (r.URL.EscapedPath()), not r.URL.Path.
// Decap percent-encodes ref names and tree directories, so cms%2Fpages%2Fslug
// is one path segment on the wire; matching the decoded form would split it
// into three and change what the path means.
//
// Allow sees no request body, so the checks that depend on one — the force
// flag on a ref update, the ref name on a ref creation, the base and head of a
// pull request — live in checkBody and run in addition to this.
func Allow(method, subPath, branch string) Decision {
	m := strings.ToUpper(strings.TrimSpace(method))
	p := strings.Trim(subPath, "/")

	if p == "" {
		if m == http.MethodGet {
			return allowed()
		}
		return denied("only GET is allowed on the repository itself")
	}
	if d := checkSegments(p); !d.Allowed {
		return d
	}

	seg := strings.Split(p, "/")
	switch seg[0] {
	case "branches":
		// GET /branches/{branch}
		if len(seg) == 2 && m == http.MethodGet {
			return allowed()
		}

	case "commits":
		// GET /commits (list) and GET /commits/{sha}/status (deploy preview)
		if m == http.MethodGet && (len(seg) == 1 || (len(seg) == 3 && seg[2] == "status")) {
			return allowed()
		}

	case "compare":
		// GET /compare/{base}...{head}. head is "{owner}:cms/pages/slug",
		// which Decap does not encode, so the tail spans several segments.
		if m == http.MethodGet && len(seg) >= 2 {
			return allowed()
		}

	case "git":
		return allowGit(m, seg, branch)

	case "pulls":
		switch {
		case len(seg) == 1 && (m == http.MethodGet || m == http.MethodPost):
			return allowed()
		case len(seg) == 2 && isNumber(seg[1]) && (m == http.MethodGet || m == http.MethodPatch):
			return allowed()
		case len(seg) == 3 && isNumber(seg[1]) && seg[2] == "commits" && m == http.MethodGet:
			return allowed()
		case len(seg) == 3 && isNumber(seg[1]) && seg[2] == "merge" && m == http.MethodPut:
			return allowed()
		}

	case "issues":
		// The editorial workflow stores entry status as a decap-cms/<status>
		// label on the pull request, and the Workflow tab is driven entirely
		// by those labels. Without this, saving works and publishing does not.
		if len(seg) == 3 && isNumber(seg[1]) && seg[2] == "labels" && m == http.MethodPut {
			return allowed()
		}
	}

	return denied("%s below /repos is not on the allowlist", m)
}

// allowGit covers the git data API, which is how Decap performs every write.
// seg[0] is "git".
func allowGit(m string, seg []string, branch string) Decision {
	if len(seg) < 2 {
		return denied("%s /git is not on the allowlist", m)
	}
	switch seg[1] {
	case "blobs":
		if len(seg) == 2 && m == http.MethodPost {
			return allowed()
		}
		if len(seg) == 3 && m == http.MethodGet {
			return allowed()
		}

	case "trees":
		if len(seg) == 2 && m == http.MethodPost {
			return allowed()
		}
		// GET /git/trees/{branch}:{dir}. listFiles leaves the directory
		// unencoded, getFileSha encodes it, so both one and many segments.
		if len(seg) >= 3 && m == http.MethodGet {
			return allowed()
		}

	case "commits":
		if len(seg) == 2 && m == http.MethodPost {
			return allowed()
		}
		if len(seg) == 3 && m == http.MethodGet {
			return allowed()
		}

	case "matching-refs":
		if len(seg) >= 3 && m == http.MethodGet {
			return allowed()
		}

	case "ref":
		if len(seg) >= 3 && m == http.MethodGet {
			return allowed()
		}

	case "refs":
		return allowRefs(m, seg, branch)
	}

	return denied("%s /git/%s is not on the allowlist", m, seg[1])
}

// allowRefs pins which refs may be written. Decap only checks the cms/ prefix
// in the browser before force-pushing, so nothing but this stops a hand-built
// force push over the default branch.
func allowRefs(m string, seg []string, branch string) Decision {
	if len(seg) == 2 {
		// POST /git/refs carries the ref name in the body, not the path;
		// checkBody pins it to refs/heads/cms/*.
		if m == http.MethodPost {
			return allowed()
		}
		return denied("%s /git/refs is not on the allowlist", m)
	}

	refType := seg[2]
	name, err := unescapeJoin(seg[3:])
	if err != nil {
		return denied("ref name is not valid percent-encoding")
	}

	switch m {
	case http.MethodGet:
		return allowed()

	case http.MethodPatch, http.MethodDelete:
		if refType != "heads" {
			return denied("only refs under heads may be modified")
		}
		if name == "" {
			return denied("ref name is empty")
		}
		if strings.HasPrefix(name, cmsBranchPrefix) {
			return allowed()
		}
		if m == http.MethodPatch && name == branch {
			// Fast-forwarding the default branch is the normal save path for
			// a non-editorial workflow. checkBody rejects force on it.
			return allowed()
		}
		return denied("only the configured branch and refs/heads/%s* may be modified", cmsBranchPrefix)
	}

	return denied("%s /git/refs/... is not on the allowlist", m)
}

// AcceptsAuthor reports whether the endpoint creates a commit and therefore
// takes author and committer objects. In Decap 3.8.x this is only
// POST /git/commits: the backend never writes through the contents API, and
// no other endpoint it calls records a git identity.
func AcceptsAuthor(method, subPath string) bool {
	return strings.EqualFold(method, http.MethodPost) &&
		strings.Trim(subPath, "/") == "git/commits"
}

// checkSegments rejects paths that cannot legitimately come from Decap:
// empty, dot and dot-dot segments in either literal or percent-encoded form,
// bad percent-encoding, and characters that cannot appear in a request path.
// http.ServeMux normalises literal traversal before a handler runs, but this
// package must not depend on being mounted behind one.
func checkSegments(p string) Decision {
	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			return denied("path contains an empty segment")
		}
		if strings.ContainsAny(seg, "?#\\") {
			return denied("path contains a disallowed character")
		}
		for _, r := range seg {
			if r < 0x20 || r == 0x7f {
				return denied("path contains a control character")
			}
		}
		dec, err := url.PathUnescape(seg)
		if err != nil {
			return denied("path is not valid percent-encoding")
		}
		if strings.Contains(dec, "\x00") {
			return denied("path contains a null byte")
		}
		// Decoded too: cms%2F..%2Fmain is one segment on the wire but names
		// something else entirely once a ref name is read out of it.
		for _, part := range strings.Split(dec, "/") {
			if part == "." || part == ".." {
				return denied("path contains a traversal segment")
			}
		}
	}
	return allowed()
}

// unescapeJoin decodes each escaped segment and rejoins them with slashes,
// yielding the ref name as git sees it. Decap sends cms/pages/slug either as
// one segment "cms%2Fpages%2Fslug" or as three, and both mean the same ref.
func unescapeJoin(seg []string) (string, error) {
	parts := make([]string, len(seg))
	for i, s := range seg {
		dec, err := url.PathUnescape(s)
		if err != nil {
			return "", err
		}
		parts[i] = dec
	}
	return strings.Join(parts, "/"), nil
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
