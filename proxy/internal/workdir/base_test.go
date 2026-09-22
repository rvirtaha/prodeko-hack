package workdir

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// One view serves everybody, so a refresh or a build belonging to one
// conversation runs while another is reading. A read holds the view still: the
// alternative is answering "no such file" about a page that exists, out of a
// tree git is in the middle of resetting.
func TestAReadHoldsOffARefreshOfTheBaseView(t *testing.T) {
	f := newFixture(t)
	m := f.manager(t)
	b, err := m.Base()
	if err != nil {
		t.Fatalf("Base: %v", err)
	}

	reading, release := make(chan struct{}), make(chan struct{})
	viewed := make(chan error, 1)
	go func() {
		viewed <- b.View(func() error {
			close(reading)
			<-release
			return nil
		})
	}()
	<-reading

	refreshed := make(chan error, 1)
	go func() { refreshed <- m.RefreshBase(context.Background()) }()
	select {
	case err := <-refreshed:
		t.Fatalf("RefreshBase ran underneath a read of the view: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	if err := <-viewed; err != nil {
		t.Fatalf("View: %v", err)
	}
	if err := <-refreshed; err != nil {
		t.Fatalf("RefreshBase after the read: %v", err)
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
