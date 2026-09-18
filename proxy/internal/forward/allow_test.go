package forward

import "testing"

const testBranch = "main"

func TestAllow(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		subPath string
		want    bool
	}{
		// The repository itself. Decap gates its whole UI on this response.
		{"repo root read", "GET", "", true},
		{"repo root write", "PATCH", "", false},
		{"repo root delete", "DELETE", "", false},

		// Reads Decap makes while browsing.
		{"branch", "GET", "branches/main", true},
		{"branch encoded", "GET", "branches/cms%2Fpages%2Fslug", true},
		{"commit list", "GET", "commits", true},
		{"commit status", "GET", "commits/abc123/status", true},
		{"commit detail", "GET", "commits/abc123", false},
		{"compare", "GET", "compare/main...prodeko:cms/pages/slug", true},

		// The write path: blobs, tree, commit, ref.
		{"blob upload", "POST", "git/blobs", true},
		{"blob read", "GET", "git/blobs/abc123", true},
		{"blob delete", "DELETE", "git/blobs/abc123", false},
		{"tree create", "POST", "git/trees", true},
		{"tree read unencoded dir", "GET", "git/trees/main:site/content", true},
		{"tree read encoded dir", "GET", "git/trees/main:site%2Fcontent", true},
		{"commit create", "POST", "git/commits", true},
		{"commit read", "GET", "git/commits/abc123", true},
		{"ref create", "POST", "git/refs", true},
		{"ref list", "GET", "git/refs/meta/_decap_cms", true},
		{"matching refs", "GET", "git/matching-refs/heads/cms/pages", true},

		// Pull requests and the labels the editorial workflow runs on.
		{"pull list", "GET", "pulls", true},
		{"pull create", "POST", "pulls", true},
		{"pull read", "GET", "pulls/12", true},
		{"pull update", "PATCH", "pulls/12", true},
		{"pull commits", "GET", "pulls/12/commits", true},
		{"pull merge", "PUT", "pulls/12/merge", true},
		{"pull delete", "DELETE", "pulls/12", false},
		{"pull number must be numeric", "GET", "pulls/..%2Fsecrets", false},
		{"labels", "PUT", "issues/12/labels", true},
		{"labels post", "POST", "issues/12/labels", false},

		// Explicitly out of reach.
		{"collaborators", "GET", "collaborators/bot/permission", false},
		{"collaborator add", "PUT", "collaborators/attacker", false},
		{"webhooks", "GET", "hooks", false},
		{"webhook create", "POST", "hooks", false},
		{"actions secrets", "PUT", "actions/secrets/DEPLOY_KEY", false},
		{"dependabot secrets", "PUT", "dependabot/secrets/X", false},
		{"deploy keys", "POST", "keys", false},
		{"environments", "PUT", "environments/production", false},
		{"actions workflows", "GET", "actions/workflows", false},
		{"forks", "POST", "forks", false},
		{"merge upstream", "POST", "merge-upstream", false},
		{"transfer", "POST", "transfer", false},

		// Decap never writes through the contents API, and its one legacy read
		// is already wrapped in a catch, so denying it costs nothing.
		{"contents read", "GET", "contents/site/content/index.md", false},
		{"contents write", "PUT", "contents/site/content/index.md", false},

		// Notes, off by default; denial is logged rather than silent.
		{"issue comments", "POST", "issues/12/comments", false},
		{"issues", "POST", "issues", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Allow(tc.method, tc.subPath, testBranch)
			if got.Allowed != tc.want {
				t.Fatalf("Allow(%q, %q) = %v (%s), want allowed=%v",
					tc.method, tc.subPath, got.Allowed, got.Reason, tc.want)
			}
			if !got.Allowed && got.Reason == "" {
				t.Fatal("a denial must carry a reason")
			}
		})
	}
}

// Decap only checks the cms/ prefix in the browser before force-pushing, so the
// proxy is the only thing between an authenticated editor and a rewritten main.
func TestAllowPinsRefWrites(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		subPath string
		want    bool
	}{
		{"update default branch", "PATCH", "git/refs/heads/main", true},
		{"update cms branch", "PATCH", "git/refs/heads/cms/pages/slug", true},
		{"update cms branch encoded", "PATCH", "git/refs/heads/cms%2Fpages%2Fslug", true},
		{"update unrelated branch", "PATCH", "git/refs/heads/release", false},
		{"update develop", "PATCH", "git/refs/heads/develop", false},
		{"delete default branch", "DELETE", "git/refs/heads/main", false},
		{"delete cms branch", "DELETE", "git/refs/heads/cms%2Fpages%2Fslug", true},
		{"delete cms branch unencoded", "DELETE", "git/refs/heads/cms/pages/slug", true},
		{"delete a tag", "DELETE", "git/refs/tags/v1.0.0", false},
		{"update a tag", "PATCH", "git/refs/tags/v1.0.0", false},
		{"update the metadata ref", "PATCH", "git/refs/meta/_decap_cms", false},
		{"a branch merely starting with the prefix", "PATCH", "git/refs/heads/cmsfoo", false},
		{"cms prefix with nothing after it", "DELETE", "git/refs/heads/cms%2F", true},
		{"reads are unrestricted", "GET", "git/refs/heads/release", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Allow(tc.method, tc.subPath, testBranch)
			if got.Allowed != tc.want {
				t.Fatalf("Allow(%q, %q) = %v (%s), want allowed=%v",
					tc.method, tc.subPath, got.Allowed, got.Reason, tc.want)
			}
		})
	}
}

// An encoded slash is part of the name, not a separator. The danger is a name
// that reads as an allowed branch once the encoding is peeled off.
func TestAllowRejectsTraversalInsideAnEncodedSegment(t *testing.T) {
	tests := []string{
		"git/refs/heads/release%2F..%2F..%2Fmain",
		// Prefixed with cms/, so the ref pin alone would wave this through.
		"git/refs/heads/cms%2F..%2F..%2Fmain",
		"git/trees/main:site%2F..%2F..%2Fetc",
	}
	for _, subPath := range tests {
		t.Run(subPath, func(t *testing.T) {
			if d := Allow("PATCH", subPath, testBranch); d.Allowed {
				t.Fatalf("Allow(PATCH, %q) was allowed, want denied", subPath)
			}
		})
	}
}

func TestCheckSegmentsRejectsMalformedPaths(t *testing.T) {
	tests := []struct {
		name    string
		subPath string
	}{
		{"literal traversal", "git/../../evil"},
		{"encoded traversal", "git/%2e%2e/evil"},
		{"uppercase encoded traversal", "git/%2E%2E/evil"},
		{"single dot", "git/./blobs"},
		{"empty segment", "git//blobs"},
		{"backslash", "git\\blobs"},
		{"bad percent encoding", "git/refs/heads/%zz"},
		{"truncated percent encoding", "git/refs/heads/%2"},
		{"control character", "git/refs/heads/a\nb"},
		{"null byte encoded", "git/refs/heads/a%00b"},
		{"embedded query", "git/blobs?x=1"},
		{"embedded fragment", "git/blobs#x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if d := Allow("POST", tc.subPath, testBranch); d.Allowed {
				t.Fatalf("Allow(POST, %q) was allowed, want denied", tc.subPath)
			}
		})
	}
}

func TestAcceptsAuthor(t *testing.T) {
	tests := []struct {
		method  string
		subPath string
		want    bool
	}{
		{"POST", "git/commits", true},
		{"post", "git/commits", true},
		{"POST", "/git/commits", true},
		{"GET", "git/commits", false},
		{"POST", "git/trees", false},
		{"POST", "git/blobs", false},
		{"POST", "pulls", false},
		// Decap's github backend never writes through the contents API, so
		// this endpoint is not an injection point even though it takes one.
		{"PUT", "contents/site/content/index.md", false},
		{"PUT", "pulls/12/merge", false},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.subPath, func(t *testing.T) {
			if got := AcceptsAuthor(tc.method, tc.subPath); got != tc.want {
				t.Fatalf("AcceptsAuthor(%q, %q) = %v, want %v", tc.method, tc.subPath, got, tc.want)
			}
		})
	}
}

func TestUnescapeJoin(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"encoded", []string{"cms%2Fpages%2Fslug"}, "cms/pages/slug"},
		{"unencoded", []string{"cms", "pages", "slug"}, "cms/pages/slug"},
		{"mixed", []string{"cms", "pages%2Fslug"}, "cms/pages/slug"},
		{"empty", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := unescapeJoin(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unescapeJoin(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
