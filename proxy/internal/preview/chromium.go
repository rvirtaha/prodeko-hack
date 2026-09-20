package preview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Widths a picture is taken at. 1280 is the desktop the site is laid out for; 390
// is the phone the media team reads it on.
const (
	DesktopWidth = 1280
	MobileWidth  = 390
)

// Viewport heights, one per width.
//
// Chromium's own screenshot is a viewport and not a document, so a whole page is
// a viewport tall enough to hold one, with the background under a shorter page
// cut off the picture afterwards. prodeko.org's longest page measures about
// 3300 px wide at 1280 and about 5200 px at 390; these leave it room to grow.
const (
	desktopHeight = 5200
	mobileHeight  = 8000
)

// CaptureTimeout bounds one Chromium. Everything it loads is a local file over
// the loopback interface, so a capture that runs this long is a browser that is
// not coming back.
const CaptureTimeout = 30 * time.Second

// virtualTimeBudget is how long the page is given to lay itself out, in
// milliseconds of the page's own clock. Chromium fast-forwards timers to it and
// captures when it runs out, so this is not four seconds of waiting.
const virtualTimeBudget = 4000

// The bounds of the trim. A capture whose blank strip is shorter than minTrim is
// a page that filled its viewport, and trimPad leaves the page a little air
// under its footer rather than cutting flush against the last pixel of it.
const (
	minTrim = 64
	trimPad = 24
)

// DefaultBins are the names headless Chromium is installed under, in the order
// they are tried. The server image ships the first; the rest are what a
// developer's machine is likely to have.
var DefaultBins = []string{
	"chrome-headless-shell",
	"headless_shell",
	"chromium",
	"chromium-browser",
	"google-chrome",
	"google-chrome-stable",
}

// ErrNoChromium is an image or a machine with no browser in it. It is reported
// as it is: a server that cannot take pictures is not the editor's mistake, and
// the tests skip on it rather than failing.
var ErrNoChromium = errors.New("preview: no headless Chromium is installed")

// ErrChromium is Chromium failing at a page. Whatever it said travels with it
// and is passed on verbatim: this package understands the site, not the browser.
var ErrChromium = errors.New("preview: Chromium did not produce a screenshot")

// Shooter runs headless Chromium.
type Shooter struct {
	// Bin is the browser to run. Empty means the first of [DefaultBins] found on
	// PATH, which is how the server image supplies it.
	Bin string

	Log *slog.Logger // nil means slog.Default
}

// Shot is one capture. The size travels with the picture because "1280 by 3262"
// is part of what the model is being told it is looking at.
type Shot struct {
	PNG    []byte
	Width  int
	Height int

	// Cut is a picture that ends where the viewport did rather than where the
	// page did: the page is taller than any capture this takes, and what is
	// below the fold of the picture is not in it.
	Cut bool
}

// Find resolves the browser this Shooter runs, or names what it looked for.
func (s Shooter) Find() (string, error) {
	if s.Bin != "" {
		path, err := exec.LookPath(s.Bin)
		if err != nil {
			return "", fmt.Errorf("%w: %s is not executable: %w", ErrNoChromium, s.Bin, err)
		}
		return path, nil
	}
	for _, name := range DefaultBins {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: none of %s is on PATH", ErrNoChromium, strings.Join(DefaultBins, ", "))
}

// Capture is one page as a PNG, at the given viewport width.
//
// The page is loaded over HTTP from the server this process started, never as a
// file: hugo's output links its stylesheet and its fonts from the site root, and
// a browser reading file:// resolves those against the filesystem root.
func (s Shooter) Capture(ctx context.Context, url string, width int) (Shot, error) {
	bin, err := s.Find()
	if err != nil {
		return Shot{}, err
	}
	height := viewportHeight(width)

	// One throwaway profile per capture. Chromium locks a profile directory, so
	// two captures sharing one would serialise on it at best and refuse to start
	// at worst; and a profile that outlived the capture would be a cache of a
	// site that is about to change.
	dir, err := os.MkdirTemp("", "prodeko-shot-")
	if err != nil {
		return Shot{}, fmt.Errorf("preview: making a directory for the capture: %w", err)
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "page.png")

	ctx, cancel := context.WithTimeout(ctx, CaptureTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, captureArgs(bin, dir, file, url, width, height)...)
	// Built rather than inherited, like every other process this server starts:
	// the browser gets a home of its own for the length of the capture and
	// nothing out of this process's environment.
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"TMPDIR=" + dir,
		"LC_ALL=C",
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	// Chromium leaves helper processes behind; killing the parent does not kill
	// them, and one holding the output pipe open would keep the timeout from
	// being a timeout.
	cmd.WaitDelay = 5 * time.Second

	runErr := cmd.Run()
	data, readErr := os.ReadFile(file)
	if readErr != nil || len(data) == 0 {
		out := strings.TrimSpace(buf.String())
		if out == "" {
			out = "(it said nothing)"
		}
		if runErr == nil {
			runErr = errors.New("it exited cleanly and wrote no file")
		}
		return Shot{}, fmt.Errorf("%w at %s: %v. Chromium said:\n\n%s", ErrChromium, url, runErr, out)
	}
	if runErr != nil {
		// A picture and a bad exit status together is Chromium falling over on
		// its way out. The picture is the answer; the status is worth a line in
		// the log and nothing more.
		s.logger().Warn("preview: chromium wrote a screenshot and exited badly",
			"url", url, "err", runErr, "output", strings.TrimSpace(buf.String()))
	}

	shot, err := trimBackground(data)
	if err != nil {
		return Shot{}, err
	}
	return shot, nil
}

func (s Shooter) logger() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// captureArgs is the whole command line, which is fixed except for the size, the
// output file and the address.
func captureArgs(bin, profile, file, url string, width, height int) []string {
	args := headlessFlag(bin)
	return append(args,
		// The container runs as an unprivileged uid with no user namespaces to
		// build a sandbox out of, and what is rendered is a page this process
		// built itself and serves on its own loopback port.
		"--no-sandbox",
		"--disable-gpu",
		// /dev/shm is 64 MB in a default container, which a renderer outgrows.
		"--disable-dev-shm-usage",
		"--no-first-run",
		"--no-default-browser-check",
		// A full Google Chrome starts sync, GCM registration, component update
		// and a keyring lookup over D-Bus, none of which a screenshot needs.
		// On a machine without those services it retries them instead of
		// rendering and the capture times out with nothing written. A
		// de-Googled Chromium or the headless shell ignores what does not
		// apply to it, so the flags cost the server image nothing.
		"--disable-background-networking",
		"--disable-sync",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-breakpad",
		"--no-pings",
		"--mute-audio",
		"--password-store=basic",
		"--use-mock-keychain",
		// A scrollbar down the side of the picture is furniture, not layout.
		"--hide-scrollbars",
		"--force-device-scale-factor=1",
		"--user-data-dir="+filepath.Join(profile, "profile"),
		"--window-size="+strconv.Itoa(width)+","+strconv.Itoa(height),
		"--virtual-time-budget="+strconv.Itoa(virtualTimeBudget),
		"--screenshot="+file,
		url,
	)
}

// headlessFlag tells a full browser to run without a window. A headless shell is
// headless by definition and does not take the flag.
func headlessFlag(bin string) []string {
	if strings.Contains(strings.ToLower(filepath.Base(bin)), "headless") {
		return nil
	}
	return []string{"--headless=new"}
}

func viewportHeight(width int) int {
	if width <= MobileWidth {
		return mobileHeight
	}
	return desktopHeight
}

// trimBackground cuts the uniform strip at the bottom of a capture, which is the
// window standing taller than the page in it.
//
// The bottom row's colour is what counts as background: whatever the page ends
// on is what is under it. A strip too short to be worth an encode is left alone,
// and the picture is then a page that filled its viewport, which is what Cut
// says.
func trimBackground(data []byte) (Shot, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return Shot{}, fmt.Errorf("preview: the capture is not a readable PNG: %w", err)
	}
	b := img.Bounds()
	bottom := contentBottom(img) + trimPad
	if bottom > b.Max.Y {
		bottom = b.Max.Y
	}
	if b.Max.Y-bottom < minTrim {
		return Shot{PNG: data, Width: b.Dx(), Height: b.Dy(), Cut: true}, nil
	}

	cropped, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return Shot{PNG: data, Width: b.Dx(), Height: b.Dy()}, nil
	}
	rect := image.Rect(b.Min.X, b.Min.Y, b.Max.X, bottom)
	var out bytes.Buffer
	if err := png.Encode(&out, cropped.SubImage(rect)); err != nil {
		return Shot{}, fmt.Errorf("preview: re-encoding the trimmed capture: %w", err)
	}
	return Shot{PNG: out.Bytes(), Width: rect.Dx(), Height: rect.Dy()}, nil
}

// contentBottom is the row after the last one that is not all background. It
// reads the image a pixel at a time through the image.Image interface, which
// costs a fraction of what Chromium already spent and works whatever colour
// model the PNG decoded into.
func contentBottom(img image.Image) int {
	b := img.Bounds()
	if b.Empty() {
		return b.Max.Y
	}
	wantR, wantG, wantB, wantA := img.At(b.Min.X, b.Max.Y-1).RGBA()
	for y := b.Max.Y - 1; y >= b.Min.Y; y-- {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if r != wantR || g != wantG || bl != wantB || a != wantA {
				return y + 1
			}
		}
	}
	return b.Max.Y
}
