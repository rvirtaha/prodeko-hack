package preview

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Server serves one change's build output for the length of one screenshot call.
//
// It listens on an ephemeral port on the loopback interface: a change that no
// maintainer has reviewed is not something to publish on a network interface,
// and the only client is the Chromium this process starts.
type Server struct {
	// URL is the site root, "http://127.0.0.1:41234".
	URL string

	srv *http.Server
}

// serveTimeouts bound one page load. Everything served is a local file, so a read
// that takes a second is a bug somewhere and not a slow network.
const (
	serveReadTimeout  = 10 * time.Second
	serveWriteTimeout = 30 * time.Second
)

// Serve starts the server over output. The caller closes it; nothing here
// outlives one call.
func Serve(output string) (*Server, error) {
	if !dirExists(output) {
		return nil, ErrNotBuilt
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("preview: opening a port to serve the build output: %w", err)
	}
	s := &Server{
		URL: "http://" + lis.Addr().String(),
		srv: &http.Server{
			Handler:           handler(output),
			ReadHeaderTimeout: serveReadTimeout,
			ReadTimeout:       serveReadTimeout,
			WriteTimeout:      serveWriteTimeout,
		},
	}
	go func() {
		// The error is the listener closing, which is how this ends.
		_ = s.srv.Serve(lis)
	}()
	return s, nil
}

// Close stops the server and the port with it.
func (s *Server) Close() error {
	if err := s.srv.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// unstretch is served with every page.
//
// A capture is a viewport, and this one is made tall enough to hold a long page.
// Anything the site sizes in viewport units would grow with it — prodeko.org's
// body is min-height: 100vh, and in a five-thousand-pixel window that is five
// thousand pixels of page with the footer pushed to the bottom of it. Neutralised
// here rather than worked around in the picture afterwards: the page keeps its
// own height, and what is under it is background this trims off.
const unstretch = `<style>html,body{min-height:0!important;height:auto!important}</style>`

// handler serves the output tree, with the stylesheet above added to every page.
// Everything else — the CSS hugo built, the fonts, the images — is served exactly
// as it is on disk, because those are what the picture is of.
func handler(output string) http.Handler {
	files := http.FileServer(http.Dir(output))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, ok := htmlFile(output, r.URL.Path)
		if !ok {
			files.ServeHTTP(w, r)
			return
		}
		data, err := os.ReadFile(page)
		if err != nil {
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(inject(data, unstretch))
	})
}

// htmlFile is the page a request path names, if it names one. A directory is its
// index.html, which is how hugo's pretty URLs are laid out on disk.
func htmlFile(output, urlPath string) (string, bool) {
	abs := filepath.Join(output, filepath.FromSlash(canonicalAddress(urlPath)))
	if !within(output, abs) {
		return "", false
	}
	if dirExists(abs) {
		abs = filepath.Join(abs, "index.html")
	}
	if !strings.HasSuffix(abs, ".html") || !fileExists(abs) {
		return "", false
	}
	return abs, true
}

// inject puts a fragment at the end of the document body, where it overrides what
// the page's own stylesheet said. A document with no </body> to find is served
// with the fragment appended, which browsers parse the same way.
func inject(page []byte, fragment string) []byte {
	const closing = "</body>"
	if i := strings.LastIndex(strings.ToLower(string(page)), closing); i >= 0 {
		out := make([]byte, 0, len(page)+len(fragment))
		out = append(out, page[:i]...)
		out = append(out, fragment...)
		return append(out, page[i:]...)
	}
	return append(page, fragment...)
}
