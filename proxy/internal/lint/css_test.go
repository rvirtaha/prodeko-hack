package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The three ways a stylesheet stops being one, and the ordinary writing that
// must not be mistaken for them. The site's own main.css opens with a comment
// full of apostrophes, and a check that read one of those as a string would
// report the whole file as broken.
func TestCheckStylesheet(t *testing.T) {
	cases := []struct {
		name  string
		css   string
		want  []string
		lines []int
	}{
		{
			name: "a stylesheet with nothing wrong",
			css: `/* Prodeko's tokens, and Hugo's asset pipeline doesn't mind them. */
:root { --pd-blue: #002b5c; }
.card { background: #fff; content: "don't"; }
@media (min-width: 900px) {
  .card { padding: var(--space-4); }
}
@font-face { src: url("/fonts/Raleway.woff2") format("woff2"); }
`,
		},
		{
			name:  "a comment that never closes",
			css:   ".a { color: red }\n/* miksi\n.b { color: blue }\n",
			want:  []string{"a comment opens here and is never closed"},
			lines: []int{2},
		},
		{
			name:  "a quote that never closes",
			css:   ".a::after { content: \"hei }\n.b { color: blue }\n",
			want:  []string{"quote opens here and is not closed on this line"},
			lines: []int{1},
		},
		{
			name:  "a brace that closes nothing",
			css:   ".a { color: red }\n}\n.b { color: blue }\n",
			want:  []string{"closes nothing"},
			lines: []int{2},
		},
		{
			name:  "a brace that never closes",
			css:   "@media print {\n  .a { color: red }\n",
			want:  []string{"is never closed"},
			lines: []int{1},
		},
		{
			// A brace inside a comment or a string is not a brace, or every
			// commented-out rule in the file would read as an error.
			name: "braces that are not braces",
			css:  "/* .a { color: red } */\n.b::after { content: \"}\"; }\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkStylesheet("site/assets/css/main.css", []byte(tc.css))
			if len(got) != len(tc.want) {
				t.Fatalf("checkStylesheet found %d findings, want %d:\n%s", len(got), len(tc.want), join(got))
			}
			for i, want := range tc.want {
				if !strings.Contains(got[i].Text, want) {
					t.Errorf("finding %d = %q, want it to contain %q", i, got[i].Text, want)
				}
				if got[i].Line != tc.lines[i] {
					t.Errorf("finding %d is at line %d, want %d", i, got[i].Line, tc.lines[i])
				}
			}
		})
	}
}

// A finding has to name the file the way every tool names it, because the path
// in it is the path the model passes back to read_file.
func TestCSSNamesFilesTheWayTheToolsDo(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "site", "assets", "css", "main.css"), ".a { color: red }\n}\n")
	write(t, filepath.Join(root, "site", "assets", "css", "tokens", "colors.css"), ":root { --pd-blue: #002b5c }\n")
	// Outside the stylesheet root, and so outside this check.
	write(t, filepath.Join(root, "site", "static", "other.css"), "}\n")

	got := CSS(root)
	if len(got) != 1 {
		t.Fatalf("CSS found %d findings, want 1:\n%s", len(got), join(got))
	}
	if got[0].Where != "site/assets/css/main.css" {
		t.Errorf("the finding names %q, want the repository-relative path", got[0].Where)
	}
}

func TestCSSOfAnAbsentTree(t *testing.T) {
	if got := CSS(filepath.Join(t.TempDir(), "no-worktree")); got != nil {
		t.Fatalf("CSS of a missing tree = %v, want nothing", got)
	}
}

func write(t *testing.T, abs, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
}
