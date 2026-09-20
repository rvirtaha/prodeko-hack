package upload

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testTokens(t *testing.T, now *time.Time) *Tokens {
	t.Helper()
	tk, err := NewTokens([]byte("0123456789abcdef0123456789abcdef"), func() time.Time { return *now })
	if err != nil {
		t.Fatalf("NewTokens: %v", err)
	}
	return tk
}

func TestTokensAreSingleUseAndExpire(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	tk := testTokens(t, &now)

	token, err := tk.Mint("maija")
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	user, err := tk.Redeem(token)
	if err != nil || user != "maija" {
		t.Fatalf("Redeem = %q, %v", user, err)
	}
	if _, err := tk.Redeem(token); err != ErrUsedToken {
		t.Fatalf("second Redeem = %v, want %v", err, ErrUsedToken)
	}

	late, _ := tk.Mint("maija")
	now = now.Add(TokenTTL + time.Minute)
	if _, err := tk.Redeem(late); err != ErrExpired {
		t.Fatalf("Redeem after the TTL = %v, want %v", err, ErrExpired)
	}
}

func TestTokensRefuseTampering(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	tk := testTokens(t, &now)
	token, _ := tk.Mint("maija")

	flipped := []byte(token)
	flipped[3] ^= 1
	if _, err := tk.Redeem(string(flipped)); err != ErrBadToken {
		t.Fatalf("Redeem of a tampered token = %v, want %v", err, ErrBadToken)
	}
	if _, err := tk.Redeem("not-a-token"); err != ErrBadToken {
		t.Fatalf("Redeem of garbage = %v, want %v", err, ErrBadToken)
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding a png: %v", err)
	}
	return buf.Bytes()
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.RGBA{200, 10, 10, 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encoding a jpeg: %v", err)
	}
	return buf.Bytes()
}

func TestSanitizeAcceptsImagesAndRefusesTheRest(t *testing.T) {
	if _, ext, err := Sanitize(testPNG(t)); err != nil || ext != ".png" {
		t.Errorf("png: %v %q", err, ext)
	}
	if _, ext, err := Sanitize(testJPEG(t)); err != nil || ext != ".jpg" {
		t.Errorf("jpeg: %v %q", err, ext)
	}
	for name, data := range map[string][]byte{
		"text":             []byte("hei vaan"),
		"pdf":              []byte("%PDF-1.7 ..."),
		"jpeg magic, junk": {0xff, 0xd8, 0xff, 0x00, 0x01},
		"svg":              []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"),
	} {
		if _, _, err := Sanitize(data); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func postImage(t *testing.T, h *Handler, token string, name string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("token", token)
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, Path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerSavesACleanImage(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	tk := testTokens(t, &now)

	var savedUser, savedName string
	var savedBytes []byte
	h := &Handler{
		Tokens: tk,
		Log:    slog.New(slog.DiscardHandler),
		Save: func(user, name string, data []byte) (string, error) {
			savedUser, savedName, savedBytes = user, name, data
			return "site/assets/images/" + name, nil
		},
	}
	token, _ := tk.Mint("maija")
	rec := postImage(t, h, token, "Vujut 2026 Kuva.PNG", testPNG(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if savedUser != "maija" || !strings.HasPrefix(savedName, "vujut-2026-kuva-") || !strings.HasSuffix(savedName, ".png") {
		t.Errorf("saved as %q for %q", savedName, savedUser)
	}
	if len(savedBytes) == 0 || !bytes.HasPrefix(savedBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Errorf("saved bytes are not a png")
	}
	if !strings.Contains(rec.Body.String(), "site/assets/images/") {
		t.Errorf("the answer does not name the asset: %s", rec.Body)
	}
}

func TestHandlerRefusesABadTokenAndANonImage(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	tk := testTokens(t, &now)
	h := &Handler{Tokens: tk, Log: slog.New(slog.DiscardHandler),
		Save: func(user, name string, data []byte) (string, error) {
			t.Error("Save ran for a refused upload")
			return "", nil
		}}

	if rec := postImage(t, h, "nope", "a.png", testPNG(t)); rec.Code != http.StatusForbidden {
		t.Errorf("bad token status = %d", rec.Code)
	}
	token, _ := tk.Mint("maija")
	if rec := postImage(t, h, token, "a.txt", []byte("just text")); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("non-image status = %d", rec.Code)
	}
}

func TestCORSReflectsOnlyTheWidgetOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, Path, nil)
	req.Header.Set("Origin", "https://abc123.claudemcpcontent.com")
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	(&Handler{Tokens: nil, Log: slog.New(slog.DiscardHandler)}).Register(mux)
	mux.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://abc123.claudemcpcontent.com" {
		t.Errorf("widget origin not allowed: %q", got)
	}

	req = httptest.NewRequest(http.MethodOptions, Path, nil)
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("a foreign origin was allowed: %q", got)
	}
}

var _ = io.Discard
