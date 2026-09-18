package forward

import (
	"net/url"
	"strings"
)

// rewriteLink points GitHub's pagination links back at this proxy, or drops
// them.
//
// Relaying the header untouched leaks the session token: Decap's
// getAllResponses reads rel="next" off the response and re-issues the request
// with the same Authorization header, so the browser would send our token to
// api.github.com. It is reachable on the Workflow tab as soon as the repository
// has more than thirty open pull requests.
//
// GitHub emits the numeric form — /repositories/{id}/pulls?page=2 — not
// /repos/{owner}/{repo}/..., so a rewriter that only matches the named form
// silently misses every link. Both forms are handled here. Anything that does
// not map onto the pinned repository is dropped, and with no PublicBase
// configured the whole header is dropped: Decap then stops after one page,
// which is the right way for this to fail.
func (h *Handler) rewriteLink(values []string) string {
	if h.publicRepoPrefix == "" || len(values) == 0 {
		return ""
	}
	var out []string
	for _, value := range values {
		for _, entry := range splitLinkEntries(value) {
			if rewritten, ok := h.rewriteLinkEntry(entry); ok {
				out = append(out, rewritten)
			}
		}
	}
	return strings.Join(out, ", ")
}

// rewriteLinkEntry rewrites one `<url>; rel="next"` entry.
func (h *Handler) rewriteLinkEntry(entry string) (string, bool) {
	entry = strings.TrimSpace(entry)
	open := strings.Index(entry, "<")
	closed := strings.Index(entry, ">")
	if open != 0 || closed < 0 {
		return "", false
	}
	raw := entry[open+1 : closed]
	params := entry[closed+1:]

	u, err := url.Parse(raw)
	if err != nil || u.Host != h.apiRoot.Host || u.Scheme != h.apiRoot.Scheme {
		return "", false
	}

	rest, ok := h.stripUpstreamRepo(u.EscapedPath())
	if !ok {
		return "", false
	}

	rewritten := h.publicRepoPrefix + rest
	if u.RawQuery != "" {
		rewritten += "?" + u.RawQuery
	}
	return "<" + rewritten + ">" + params, true
}

// stripUpstreamRepo removes whichever repository prefix GitHub used and returns
// the remainder, including its leading slash.
func (h *Handler) stripUpstreamRepo(escapedPath string) (string, bool) {
	base := strings.TrimSuffix(h.apiRoot.EscapedPath(), "/")
	rest, ok := strings.CutPrefix(escapedPath, base)
	if !ok {
		return "", false
	}
	seg := strings.Split(strings.TrimPrefix(rest, "/"), "/")

	switch {
	case len(seg) >= 3 && seg[0] == "repos":
		// The pinned repo is the only one we ever call, so the owner and repo
		// here can only be ours.
		return joinTail(seg[3:]), true
	case len(seg) >= 2 && seg[0] == "repositories" && isNumber(seg[1]):
		return joinTail(seg[2:]), true
	}
	return "", false
}

func joinTail(seg []string) string {
	if len(seg) == 0 {
		return ""
	}
	return "/" + strings.Join(seg, "/")
}

// splitLinkEntries splits on the commas that separate entries rather than on
// commas inside a URL, by only breaking where the next entry's "<" begins.
func splitLinkEntries(value string) []string {
	var entries []string
	depth := 0
	start := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 && nextIsEntry(value[i+1:]) {
				entries = append(entries, value[start:i])
				start = i + 1
			}
		}
	}
	if start < len(value) {
		entries = append(entries, value[start:])
	}
	return entries
}

func nextIsEntry(rest string) bool {
	return strings.HasPrefix(strings.TrimLeft(rest, " \t"), "<")
}
