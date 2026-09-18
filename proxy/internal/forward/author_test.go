package forward

import (
	"encoding/json"
	"testing"
)

var (
	testEditor    = Author{Name: "Aino Editor", Email: "aino@prodeko.org"}
	testCommitter = Author{Name: "Prodeko Bot", Email: "bot@prodeko.org"}
)

func TestInjectAuthor(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			// What Decap actually sends on a save: three keys, no author.
			name: "no author present",
			body: `{"message":"Update index.md","tree":"t1","parents":["p1"]}`,
		},
		{
			// rebaseSingleCommit replays existing commits and carries their
			// original identities. Rule 2 says overwrite, so overwrite.
			name: "rebase carries an author and a committer",
			body: `{"message":"m","tree":"t1","parents":["p1"],` +
				`"author":{"name":"Someone Else","email":"else@example.com","date":"2020-01-01T00:00:00Z"},` +
				`"committer":{"name":"Someone Else","email":"else@example.com","date":"2020-01-01T00:00:00Z"}}`,
		},
		{
			name: "a forged author from a modified browser",
			body: `{"message":"m","tree":"t1","parents":[],"author":{"name":"Chair","email":"chair@prodeko.org"}}`,
		},
		{
			name: "empty body",
			body: ``,
		},
		{
			name: "empty object",
			body: `{}`,
		},
		{
			name: "json null",
			body: `null`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InjectAuthor([]byte(tc.body), testEditor, testCommitter)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var parsed struct {
				Author    map[string]any `json:"author"`
				Committer map[string]any `json:"committer"`
			}
			if err := json.Unmarshal(got, &parsed); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}
			if parsed.Author["name"] != testEditor.Name || parsed.Author["email"] != testEditor.Email {
				t.Fatalf("author = %v, want %+v", parsed.Author, testEditor)
			}
			if parsed.Committer["name"] != testCommitter.Name || parsed.Committer["email"] != testCommitter.Email {
				t.Fatalf("committer = %v, want %+v", parsed.Committer, testCommitter)
			}
			// A date copied off a replayed commit must not survive, or the
			// rewritten identity would keep the old timestamp.
			if _, ok := parsed.Author["date"]; ok {
				t.Fatal("author kept a date from the request body")
			}
			if _, ok := parsed.Committer["date"]; ok {
				t.Fatal("committer kept a date from the request body")
			}
		})
	}
}

func TestInjectAuthorPreservesEveryOtherField(t *testing.T) {
	body := `{"message":"Update index.md","tree":"abc","parents":["def"],"signature":"sig"}`
	got, err := InjectAuthor([]byte(body), testEditor, testCommitter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"message":   "Update index.md",
		"tree":      "abc",
		"signature": "sig",
	} {
		if parsed[key] != want {
			t.Errorf("%s = %v, want %v", key, parsed[key], want)
		}
	}
	parents, ok := parsed["parents"].([]any)
	if !ok || len(parents) != 1 || parents[0] != "def" {
		t.Errorf("parents = %v, want [def]", parsed["parents"])
	}
}

func TestInjectAuthorRejectsBadInput(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		editor    Author
		committer Author
	}{
		{"array body", `[1,2,3]`, testEditor, testCommitter},
		{"string body", `"hello"`, testEditor, testCommitter},
		{"number body", `42`, testEditor, testCommitter},
		{"malformed json", `{"message":`, testEditor, testCommitter},
		{"editor with no email", `{}`, Author{Name: "Aino"}, testCommitter},
		{"editor with no name", `{}`, Author{Email: "aino@prodeko.org"}, testCommitter},
		{"no committer", `{}`, testEditor, Author{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := InjectAuthor([]byte(tc.body), tc.editor, tc.committer); err == nil {
				t.Fatal("expected an error, got none")
			}
		})
	}
}
