package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What the HTML check must catch, and what it must stay quiet about. The quiet
// half is the important half: this runs over every page of a site it did not
// write, and a check that is wrong about the pages nobody touched is a check the
// model learns to scroll past.
func TestCheckPage(t *testing.T) {
	cases := []struct {
		name string
		page string
		want []string // substrings, one per expected finding, in order
	}{
		{
			name: "a whole page with nothing wrong",
			page: `<!DOCTYPE html>
<html lang="fi"><head><meta charset="utf-8"><title>Prodeko</title></head>
<body>
  <main id="main">
    <div class="card"><h2>Tapahtumat</h2><p>Tapahtumia tulossa.</p></div>
    <ul><li>yksi<li>kaksi</ul>
    <img src="/kuva.png" alt="Kuva"><br>
    <table><tr><td>a<td>b</table>
  </main>
</body></html>
`,
		},
		{
			name: "a div that is never closed",
			page: "<html><body>\n<div class=\"card\">\n<p>Hei</p>\n</body></html>\n",
			want: []string{"<div> is never closed"},
		},
		{
			name: "an end tag that closes nothing",
			page: "<html><body>\n<p>Hei</p>\n</section>\n</body></html>\n",
			want: []string{"</section> closes nothing"},
		},
		{
			name: "a span still open where its parent closes",
			page: "<html><body>\n<div>\n<span>nimi\n</div>\n</body></html>\n",
			want: []string{"<span> is still open"},
		},
		{
			name: "two elements with one id",
			page: "<html><body>\n<div id=\"main\">a</div>\n<section id=\"main\">b</section>\n</body></html>\n",
			want: []string{`id="main" is on two elements`},
		},
		{
			// Script content is text, not markup. A comparison in JavaScript is
			// not an unclosed tag, and a check that said so would fire on the
			// site's own mega menu.
			name: "a comparison inside a script",
			page: "<html><body>\n<script>if (a<b && c>d) { go(); }</script>\n</body></html>\n",
		},
		{
			// A void element is closed by being written, and an editor who wrote
			// </br> has done nothing to anybody.
			name: "void elements",
			page: "<html><body>\n<hr><br><input name=\"x\"><img src=\"/a.png\" alt=\"\">\n</body></html>\n",
		},
		{
			// A page without <html> or <body> is a page the browser supplies
			// them for. Hugo writes partial output like this for aliases.
			name: "a fragment",
			page: "<p>Hei</p>\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkPage("fi/index.html", []byte(tc.page))
			if len(got) != len(tc.want) {
				t.Fatalf("checkPage found %d findings, want %d:\n%s", len(got), len(tc.want), join(got))
			}
			for i, want := range tc.want {
				if !strings.Contains(got[i].Text, want) {
					t.Errorf("finding %d = %q, want it to contain %q", i, got[i].Text, want)
				}
				if got[i].Line <= 0 {
					t.Errorf("finding %d has no line, and a line is what makes it fixable", i)
				}
				if got[i].Where != "fi/index.html" {
					t.Errorf("finding %d names %q, not the page it is on", i, got[i].Where)
				}
			}
		})
	}
}

// The line named is where the element opened, not where the check noticed: the
// opening tag is the thing to go and look at.
func TestFindingNamesTheLineTheElementOpenedOn(t *testing.T) {
	page := "<html>\n<body>\n<p>yksi</p>\n<div>\n<p>kaksi</p>\n</body>\n</html>\n"
	got := checkPage("fi/index.html", []byte(page))
	if len(got) != 1 {
		t.Fatalf("checkPage found %d findings, want 1:\n%s", len(got), join(got))
	}
	if got[0].Line != 4 {
		t.Errorf("the unclosed <div> is reported at line %d, want 4", got[0].Line)
	}
}

// One mistake in a partial is one mistake, however many pages render it.
func TestHTMLReportsOneMistakeOnce(t *testing.T) {
	root := t.TempDir()
	const broken = "<html><body>\n<div>\n</body></html>\n"
	for _, page := range []string{"fi/index.html", "fi/tapahtumat/index.html", "en/index.html"} {
		abs := filepath.Join(root, filepath.FromSlash(page))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(broken), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	got := HTML(root)
	if len(got) != 1 {
		t.Fatalf("HTML reported %d findings for one mistake on three pages:\n%s", len(got), join(got))
	}
	// The first page in lexical order is the one to look at, and the same build
	// has to report the same one every time.
	if got[0].Where != "en/index.html" {
		t.Errorf("the finding names %q, want the first page in order", got[0].Where)
	}
}

// The cap is on the report, not on the walk: a site with a hundred different
// mistakes is still a report a person can read.
func TestHTMLStopsAtTheCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < MaxFindings*2; i++ {
		page := filepath.Join(root, fmt.Sprintf("page-%03d.html", i))
		// A different id on every page makes every finding a different one.
		body := fmt.Sprintf("<html><body><div id=\"x%d\">a</div><p id=\"x%d\">b</p></body></html>\n", i, i)
		if err := os.WriteFile(page, []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if got := HTML(root); len(got) != MaxFindings {
		t.Fatalf("HTML reported %d findings, want the cap of %d", len(got), MaxFindings)
	}
}

// A build that produced nothing is reported by the build. There is nothing for
// this to say about it, and nothing it may treat as an error.
func TestHTMLOfAnAbsentOutput(t *testing.T) {
	if got := HTML(filepath.Join(t.TempDir(), "never-built")); got != nil {
		t.Fatalf("HTML of a missing output = %v, want nothing", got)
	}
}

func TestFindingString(t *testing.T) {
	f := Finding{Where: "site/assets/css/main.css", Line: 412, Text: "a } here closes nothing"}
	if want := "site/assets/css/main.css:412: a } here closes nothing"; f.String() != want {
		t.Errorf("Finding.String() = %q, want %q", f, want)
	}
	f.Line = 0
	if want := "site/assets/css/main.css: a } here closes nothing"; f.String() != want {
		t.Errorf("Finding.String() without a line = %q, want %q", f, want)
	}
}

func join(findings []Finding) string {
	var b strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&b, "  %s\n", f)
	}
	return b.String()
}
