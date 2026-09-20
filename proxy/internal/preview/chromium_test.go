package preview

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shooter is the browser this machine has, or a skip. A capture needs a real
// Chromium and the server image is where one is guaranteed; a developer's machine
// is allowed not to have one, and a test that fails there would be a test nobody
// believes.
func shooter(t *testing.T) Shooter {
	t.Helper()
	s := Shooter{Log: slog.New(slog.DiscardHandler)}
	if _, err := s.Find(); err != nil {
		t.Skipf("%v: install one of %s to run the capture tests", err, strings.Join(DefaultBins, ", "))
	}
	return s
}

// The whole path, end to end: the output is served on a loopback port and the
// browser takes a picture of what it serves.
func TestCaptureTakesAPictureOfTheServedPage(t *testing.T) {
	s := shooter(t)
	site := testSite(t)
	srv, err := Serve(site.Output)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer srv.Close()

	shot, err := s.Capture(t.Context(), srv.URL+"/fi/tapahtumat/", DesktopWidth)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if shot.Width != DesktopWidth {
		t.Errorf("width = %d, want %d", shot.Width, DesktopWidth)
	}
	if shot.Height <= 0 || shot.Height > viewportHeight(DesktopWidth) {
		t.Errorf("height = %d, want it inside the viewport", shot.Height)
	}
	// A short page has its blank strip cut off rather than shown as page.
	if shot.Cut {
		t.Errorf("a two-line page reports as taller than the viewport")
	}
	if _, err := png.Decode(bytes.NewReader(shot.PNG)); err != nil {
		t.Fatalf("the capture is not a readable PNG: %v", err)
	}
}

// Chromium failing is the model's to read, and what Chromium said is what it
// reads: this package understands the site, not the browser. The browser here is
// a script, because a real one failing on demand is not something to arrange.
func TestCaptureQuotesTheBrowserVerbatim(t *testing.T) {
	const said = "[0920/113045.1:ERROR:headless_shell.cc(123)] Unable to open X display"
	s := Shooter{Bin: fakeBrowser(t, "exit 3", said), Log: slog.New(slog.DiscardHandler)}

	_, err := s.Capture(t.Context(), "http://127.0.0.1:1/fi/", DesktopWidth)
	if !errors.Is(err, ErrChromium) {
		t.Fatalf("Capture of a browser that failed = %v, want ErrChromium", err)
	}
	if !strings.Contains(err.Error(), said) {
		t.Fatalf("the refusal does not quote the browser: %v", err)
	}
}

// Chromium writes the picture and then falls over on its way out often enough to
// plan for. The picture is the answer; the exit status is a line in the log.
func TestCaptureKeepsAPictureWrittenBeforeABadExit(t *testing.T) {
	png := filepath.Join(t.TempDir(), "fixture.png")
	if err := os.WriteFile(png, capture(t, 200, 800, 300), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	s := Shooter{
		Bin: fakeBrowser(t, `for a in "$@"; do case "$a" in --screenshot=*) cp `+
			png+` "${a#--screenshot=}";; esac; done; exit 4`, ""),
		Log: slog.New(slog.DiscardHandler),
	}

	shot, err := s.Capture(t.Context(), "http://127.0.0.1:1/fi/", DesktopWidth)
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if shot.Height < 300 || shot.Height > 300+trimPad {
		t.Fatalf("height = %d, want the picture that was written", shot.Height)
	}
}

// fakeBrowser is a script that stands in for Chromium: it says what it is given
// to say on stderr, where a browser says such things, and then runs body.
func fakeBrowser(t *testing.T, body, says string) string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not on PATH")
	}
	path := filepath.Join(t.TempDir(), "fake-chromium")
	script := "#!/bin/sh\n"
	if says != "" {
		script += "echo '" + says + "' >&2\n"
	}
	script += body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing the fake browser: %v", err)
	}
	return path
}

// A machine with no browser is not a machine that took a bad picture, and the
// refusal names what was looked for.
func TestFindNamesWhatItLookedFor(t *testing.T) {
	_, err := Shooter{Bin: "definitely-not-a-browser"}.Find()
	if !errors.Is(err, ErrNoChromium) {
		t.Fatalf("Find = %v, want ErrNoChromium", err)
	}
	if !strings.Contains(err.Error(), "definitely-not-a-browser") {
		t.Fatalf("the refusal does not name the binary: %v", err)
	}
}

func TestViewportHeightIsTallerForTheNarrowerWidth(t *testing.T) {
	// The same page is longer on a phone, so the phone's viewport is the taller
	// of the two.
	if viewportHeight(MobileWidth) <= viewportHeight(DesktopWidth) {
		t.Fatalf("viewport heights are %d at %d and %d at %d",
			viewportHeight(MobileWidth), MobileWidth, viewportHeight(DesktopWidth), DesktopWidth)
	}
}

// The trim is what turns a viewport into a page: the window is made tall enough
// for a long page, and a short one comes back with the background under it.
func TestTrimBackgroundCutsTheBlankStrip(t *testing.T) {
	const (
		width   = 200
		content = 300
		total   = 2000
	)
	shot, err := trimBackground(capture(t, width, total, content))
	if err != nil {
		t.Fatalf("trimBackground: %v", err)
	}
	if shot.Width != width {
		t.Errorf("width = %d, want %d", shot.Width, width)
	}
	if shot.Height < content || shot.Height > content+trimPad {
		t.Errorf("height = %d, want the page's %d plus at most %d of air", shot.Height, content, trimPad)
	}
	if shot.Cut {
		t.Error("a trimmed capture reports as cut off")
	}
	if len(shot.PNG) == 0 {
		t.Error("the trimmed capture is empty")
	}
}

// A page that fills the viewport has nothing to trim, and what the model is
// looking at then ends where the window did, not where the page does.
func TestTrimBackgroundReportsAPageThatFilledTheViewport(t *testing.T) {
	shot, err := trimBackground(capture(t, 200, 800, 800))
	if err != nil {
		t.Fatalf("trimBackground: %v", err)
	}
	if !shot.Cut {
		t.Fatalf("a capture with no background under it = %+v, want Cut", shot)
	}
	if shot.Height != 800 {
		t.Errorf("height = %d, want the whole viewport", shot.Height)
	}
}

// A strip too short to be worth an encode is left where it is, PNG and all.
func TestTrimBackgroundLeavesAShortStripAlone(t *testing.T) {
	data := capture(t, 200, 800, 800-minTrim/2)
	shot, err := trimBackground(data)
	if err != nil {
		t.Fatalf("trimBackground: %v", err)
	}
	if !bytes.Equal(shot.PNG, data) {
		t.Error("a capture with almost nothing to trim was re-encoded anyway")
	}
}

func TestTrimBackgroundRefusesSomethingThatIsNotAPNG(t *testing.T) {
	if _, err := trimBackground([]byte("this is not a picture")); err == nil {
		t.Fatal("trimBackground accepted something that is not a PNG")
	}
}

// capture is a PNG shaped like one Chromium would return: content down to a
// height, and the page's background under it to the bottom of the window.
func capture(t *testing.T, width, height, content int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	background := color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	ink := color.RGBA{R: 0x11, G: 0x33, B: 0x88, A: 0xff}
	for y := range height {
		for x := range width {
			c := background
			if y < content && (x+y)%7 == 0 {
				c = ink
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the fixture: %v", err)
	}
	return buf.Bytes()
}
