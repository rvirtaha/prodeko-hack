package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// Path is where the page and the endpoint live.
const Path = "/upload"

// maxFormBytes is the request bound: the image limit plus room for the
// multipart framing and the token.
const maxFormBytes = MaxImageBytes + 64<<10

// Saver puts a clean image into the user's current change and returns the
// repository-relative path it landed on. The toolset provides it; this
// package never touches a worktree itself.
type Saver func(user, name string, data []byte) (string, error)

// Handler serves the upload page and takes the bytes. It holds no session:
// the token in the form is the whole authorization.
type Handler struct {
	Tokens *Tokens
	Save   Saver
	Log    *slog.Logger
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+Path, h.page)
	mux.HandleFunc("POST "+Path, h.accept)
	mux.HandleFunc("OPTIONS "+Path, h.preflight)
}

// cors reflects the Origin for the in-chat widget, whose iframe origin is a
// hash under claudemcpcontent.com. Matched on Origin, never Referer: iOS
// WebKit omits Referer cross-origin and that difference broke exactly this
// kind of gate before.
func cors(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if strings.HasPrefix(origin, "https://") && strings.HasSuffix(origin, ".claudemcpcontent.com") {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
	}
}

func (h *Handler) preflight(w http.ResponseWriter, r *http.Request) {
	cors(w, r)
	w.Header().Set("Access-Control-Allow-Methods", "POST")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// The token stays in the fragment/query on the client; the page itself is
	// static and the same for everyone.
	_, _ = w.Write([]byte(PageHTML))
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	cors(w, r)
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseMultipartForm(maxFormBytes); err != nil {
		h.deny(w, http.StatusRequestEntityTooLarge, "the upload is over the 5 MB limit or is not a form")
		return
	}
	user, err := h.Tokens.Redeem(r.FormValue("token"))
	if err != nil {
		// The sentence names the way out; which of the three failures it was
		// is in the log, not in an anonymous caller's answer.
		h.Log.Warn("upload: token refused", "err", err)
		h.deny(w, http.StatusForbidden, "the upload link is not valid any more; ask for a new one in the chat")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		h.deny(w, http.StatusBadRequest, "the form carries no file")
		return
	}
	defer file.Close()

	data := make([]byte, 0, 1<<20)
	buf := make([]byte, 32<<10)
	for {
		n, readErr := file.Read(buf)
		data = append(data, buf[:n]...)
		if readErr != nil {
			break
		}
		if len(data) > MaxImageBytes {
			h.deny(w, http.StatusRequestEntityTooLarge, "the file is over the 5 MB limit; export a smaller one")
			return
		}
	}

	clean, ext, err := Sanitize(data)
	if err != nil {
		h.Log.Warn("upload: refused a file", "user", user, "name", header.Filename, "err", err)
		h.deny(w, http.StatusUnsupportedMediaType, "only JPEG and PNG images are accepted")
		return
	}
	name := assetName(header.Filename, clean, ext)
	rel, err := h.Save(user, name, clean)
	if err != nil {
		h.Log.Error("upload: saving failed", "user", user, "name", name, "err", err)
		h.deny(w, http.StatusInternalServerError, "the image could not be saved to the change; try again, and if it repeats tell the maintainers")
		return
	}
	h.Log.Info("upload: image saved", "user", user, "path", rel, "bytes", len(clean))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"path": rel,
		"next": "Tell Claude the upload is done and it will use " + rel + ".",
	})
}

func (h *Handler) deny(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// assetName is what the file is saved as: the uploaded name's slug, a short
// content hash so two exports of the same name cannot overwrite each other,
// and the extension the bytes earned. The declared extension is ignored with
// the rest of the declared metadata.
func assetName(original string, data []byte, ext string) string {
	base := original
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if slug == "" {
		slug = "kuva"
	}
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	sum := sha256.Sum256(data)
	return slug + "-" + hex.EncodeToString(sum[:4]) + ext
}
