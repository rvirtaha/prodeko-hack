package workdir

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBaseViewReadsTheDefaultBranch(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)

	b, err := m.Base()
	if err != nil {
		t.Fatalf("Base: %v", err)
	}
	if !b.IsBase() {
		t.Fatal("the base view does not report IsBase")
	}
	if b.User != ".base" || b.Branch != "" {
		t.Fatalf("base view is %q on branch %q", b.User, b.Branch)
	}
	if _, err := os.Stat(filepath.Join(b.Dir, "site", "hugo.toml")); err != nil {
		t.Fatalf("the base view has no checkout: %v", err)
	}
	if dir := filepath.Join(f.state, "wt", ".base"); b.Dir != dir {
		t.Fatalf("base view is at %s, want %s", b.Dir, dir)
	}

	// A second call reattaches rather than remaking.
	b2, err := m.Base()
	if err != nil || b2 != b {
		t.Fatalf("Base is not idempotent: %v", err)
	}
}

// The view holds no edits and has no branch to put them on, so everything that
// would publish something has to refuse it rather than improvise.
func TestBaseViewRefusesSubmitAndAbandon(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	b, err := m.Base()
	if err != nil {
		t.Fatalf("Base: %v", err)
	}

	author := Author{Name: "Maija", Email: "maija@prodeko.org"}
	if _, err := m.Submit(t.Context(), b, author, "Otsikko", ""); !errors.Is(err, ErrBaseView) {
		t.Fatalf("Submit on the base view = %v, want ErrBaseView", err)
	}
	if _, err := m.Abandon(t.Context(), b); !errors.Is(err, ErrBaseView) {
		t.Fatalf("Abandon on the base view = %v, want ErrBaseView", err)
	}
	if _, err := m.Feedback(t.Context(), b); !errors.Is(err, ErrBaseView) {
		t.Fatalf("Feedback on the base view = %v, want ErrBaseView", err)
	}
	if _, err := os.Stat(filepath.Join(b.Dir, "site", "hugo.toml")); err != nil {
		t.Fatalf("a refusal removed the base view: %v", err)
	}
}

func TestRefreshBasePicksUpAMovedOrigin(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	b, err := m.Base()
	if err != nil {
		t.Fatalf("Base: %v", err)
	}

	f.pushNewFile(t, "site/content/fi/uusi.md", "---\ntitle: Uusi\n---\n")
	if err := m.RefreshBase(context.Background()); err != nil {
		t.Fatalf("RefreshBase: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.Dir, "site", "content", "fi", "uusi.md")); err != nil {
		t.Fatalf("the refreshed base view is stale: %v", err)
	}
}
