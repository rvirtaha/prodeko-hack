package forward

import "testing"

func TestClassifyBody(t *testing.T) {
	tests := []struct {
		method  string
		subPath string
		want    bodyRule
		wantRef string
	}{
		{"POST", "git/commits", bodyAuthor, ""},
		{"POST", "git/refs", bodyRefCreate, ""},
		{"PATCH", "git/refs/heads/main", bodyRefUpdate, "main"},
		{"PATCH", "git/refs/heads/cms%2Fpages%2Fslug", bodyRefUpdate, "cms/pages/slug"},
		{"PATCH", "git/refs/heads/cms/pages/slug", bodyRefUpdate, "cms/pages/slug"},
		{"POST", "pulls", bodyPullCreate, ""},
		{"PATCH", "pulls/12", bodyPullUpdate, ""},
		// Media uploads must stream, not buffer.
		{"POST", "git/blobs", bodyPassThrough, ""},
		{"POST", "git/trees", bodyPassThrough, ""},
		{"PUT", "pulls/12/merge", bodyPassThrough, ""},
		{"PUT", "issues/12/labels", bodyPassThrough, ""},
		{"DELETE", "git/refs/heads/cms%2Fpages%2Fslug", bodyPassThrough, ""},
		{"GET", "git/trees/main:site", bodyPassThrough, ""},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.subPath, func(t *testing.T) {
			rule, ref := classifyBody(tc.method, tc.subPath)
			if rule != tc.want || ref != tc.wantRef {
				t.Fatalf("classifyBody(%q, %q) = (%v, %q), want (%v, %q)",
					tc.method, tc.subPath, rule, ref, tc.want, tc.wantRef)
			}
		})
	}
}

// The ref name for a creation is in the body, not the path, so the path
// allowlist cannot see it.
func TestPinRefCreate(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"cms branch", `{"ref":"refs/heads/cms/pages/slug","sha":"abc"}`, true},
		{"the default branch", `{"ref":"refs/heads/main","sha":"abc"}`, false},
		{"another branch", `{"ref":"refs/heads/release","sha":"abc"}`, false},
		{"a tag", `{"ref":"refs/tags/v1.0.0","sha":"abc"}`, false},
		{"the metadata ref", `{"ref":"refs/meta/_decap_cms","sha":"abc"}`, false},
		{"prefix with nothing after it", `{"ref":"refs/heads/cms/","sha":"abc"}`, false},
		{"a name merely starting with cms", `{"ref":"refs/heads/cmsfoo","sha":"abc"}`, false},
		{"no ref at all", `{"sha":"abc"}`, false},
		{"malformed json", `{`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pinRefCreate([]byte(tc.body)); got.Allowed != tc.want {
				t.Fatalf("pinRefCreate(%s) = %v (%s), want %v", tc.body, got.Allowed, got.Reason, tc.want)
			}
		})
	}
}

// Decap checks the cms/ prefix in the browser before force-pushing and nowhere
// else, so a hand-built force push over the default branch stops here or not
// at all.
func TestPinRefUpdate(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		refName string
		want    bool
	}{
		{"fast-forward the default branch", `{"sha":"abc","force":false}`, "main", true},
		{"default branch, no force key", `{"sha":"abc"}`, "main", true},
		{"force over the default branch", `{"sha":"abc","force":true}`, "main", false},
		{"force a cms branch", `{"sha":"abc","force":true}`, "cms/pages/slug", true},
		{"fast-forward a cms branch", `{"sha":"abc","force":false}`, "cms/pages/slug", true},
		{"an unrelated branch", `{"sha":"abc","force":false}`, "release", false},
		{"a name merely starting with cms", `{"sha":"abc","force":true}`, "cmsfoo", false},
		{"malformed json", `{`, "main", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pinRefUpdate([]byte(tc.body), tc.refName, testBranch)
			if got.Allowed != tc.want {
				t.Fatalf("pinRefUpdate(%s, %q) = %v (%s), want %v",
					tc.body, tc.refName, got.Allowed, got.Reason, tc.want)
			}
		})
	}
}

func TestPinPullCreate(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			"what Decap sends",
			`{"title":"t","body":"b","head":"prodeko:cms/pages/slug","base":"main"}`,
			true,
		},
		{
			"owner capitalised the way GitHub returns it",
			`{"head":"ProDeko:cms/pages/slug","base":"main"}`,
			true,
		},
		{"bare branch head", `{"head":"cms/pages/slug","base":"main"}`, true},
		{"head from a fork", `{"head":"attacker:evil","base":"main"}`, false},
		{"head from a fork, cms-named branch", `{"head":"attacker:cms/pages/x","base":"main"}`, false},
		{"head is not a cms branch", `{"head":"prodeko:release","base":"main"}`, false},
		{"targets another branch", `{"head":"prodeko:cms/pages/slug","base":"release"}`, false},
		{"no base", `{"head":"prodeko:cms/pages/slug"}`, false},
		{"malformed json", `{`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pinPullCreate([]byte(tc.body), "prodeko", testBranch)
			if got.Allowed != tc.want {
				t.Fatalf("pinPullCreate(%s) = %v (%s), want %v", tc.body, got.Allowed, got.Reason, tc.want)
			}
		})
	}
}

func TestPinPullUpdate(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"closing a pull request", `{"state":"closed"}`, true},
		{"reopening", `{"state":"open"}`, true},
		{"retitling", `{"title":"New title"}`, true},
		{"base restated unchanged", `{"base":"main"}`, true},
		{"retargeting", `{"base":"release"}`, false},
		{"base of the wrong type", `{"base":123}`, false},
		{"malformed json", `{`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pinPullUpdate([]byte(tc.body), testBranch)
			if got.Allowed != tc.want {
				t.Fatalf("pinPullUpdate(%s) = %v (%s), want %v", tc.body, got.Allowed, got.Reason, tc.want)
			}
		})
	}
}
