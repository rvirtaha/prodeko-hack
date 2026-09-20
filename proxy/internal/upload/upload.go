// Package upload is how image bytes reach a change. No chat client can pass
// an attachment to an MCP tool, so the bytes go out of band: begin_image_upload
// mints a short-lived single-use token, the person opens the upload page or
// the in-chat widget, and their browser posts the file straight here. Only the
// token and the resulting asset path ever cross the model's context.
//
// The token is the whole authorization. It is HMAC-signed, bound to one
// person and about fifteen minutes, and redeemed exactly once; the upload
// page needs no session of its own because holding a fresh token is the
// proof of having asked.
package upload

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"sync"
	"time"
)

// TokenTTL is how long a minted token lasts. Long enough to find the file on
// a phone, short enough that a leaked link goes stale over coffee.
const TokenTTL = 15 * time.Minute

// MaxImageBytes matches the fence's file limit: what cannot be committed is
// not worth accepting.
const MaxImageBytes = 5 << 20

// jpegQuality is what re-encoding writes. Re-encoding is the EXIF strip: a
// decoded pixel grid carries no camera serial number, no GPS position and no
// embedded thumbnail, so encoding it fresh is the removal.
const jpegQuality = 90

var (
	ErrBadToken  = errors.New("upload: the token is not valid")
	ErrUsedToken = errors.New("upload: the token was already used")
	ErrExpired   = errors.New("upload: the token has expired")
	ErrNotImage  = errors.New("upload: not a JPEG or PNG image")
	ErrTooLarge  = fmt.Errorf("upload: the file is over the %d byte limit", MaxImageBytes)
)

// Tokens mints and redeems upload tokens. Redemption state is in memory: the
// server is one process, and a token that dies with a restart is re-minted by
// asking again.
type Tokens struct {
	secret []byte
	now    func() time.Time

	mu   sync.Mutex
	used map[string]time.Time // nonce -> expiry, for single use
}

func NewTokens(secret []byte, now func() time.Time) (*Tokens, error) {
	if len(secret) < 16 {
		return nil, errors.New("upload: the token secret is too short to sign with")
	}
	if now == nil {
		now = time.Now
	}
	return &Tokens{secret: secret, now: now, used: make(map[string]time.Time)}, nil
}

// Mint issues a token bound to user. The payload is user|expiry|nonce and the
// signature covers all of it, so neither the person nor the deadline can be
// swapped.
func (t *Tokens) Mint(user string) (string, error) {
	if strings.TrimSpace(user) == "" || strings.ContainsAny(user, "|") {
		return "", fmt.Errorf("upload: %q cannot be a token subject", user)
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("upload: minting a nonce: %w", err)
	}
	exp := make([]byte, 8)
	binary.BigEndian.PutUint64(exp, uint64(t.now().Add(TokenTTL).Unix()))
	payload := []byte(user + "|")
	payload = append(payload, exp...)
	payload = append(payload, nonce...)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(t.sign(payload)), nil
}

// Redeem checks and consumes a token, returning who it was minted for. A
// second redemption of the same token fails whatever else is true of it.
func (t *Tokens) Redeem(token string) (string, error) {
	payloadPart, macPart, ok := strings.Cut(token, ".")
	if !ok {
		return "", ErrBadToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return "", ErrBadToken
	}
	mac, err := base64.RawURLEncoding.DecodeString(macPart)
	if err != nil {
		return "", ErrBadToken
	}
	if subtle.ConstantTimeCompare(t.sign(payload), mac) != 1 {
		return "", ErrBadToken
	}
	sep := bytes.IndexByte(payload, '|')
	if sep < 1 || len(payload) != sep+1+8+16 {
		return "", ErrBadToken
	}
	user := string(payload[:sep])
	expiry := time.Unix(int64(binary.BigEndian.Uint64(payload[sep+1:sep+9])), 0)
	nonce := string(payload[sep+9:])

	now := t.now()
	if now.After(expiry) {
		return "", ErrExpired
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for n, exp := range t.used {
		if now.After(exp) {
			delete(t.used, n)
		}
	}
	if _, seen := t.used[nonce]; seen {
		return "", ErrUsedToken
	}
	t.used[nonce] = expiry
	return user, nil
}

func (t *Tokens) sign(payload []byte) []byte {
	h := hmac.New(sha256.New, t.secret)
	h.Write([]byte("prodeko-upload\x00"))
	h.Write(payload)
	return h.Sum(nil)
}

// Sanitize checks the bytes are a JPEG or PNG and re-encodes them, which is
// both the validation and the metadata strip. The magic bytes are checked
// before the decoder runs, so the answer to a PDF is "not an image" rather
// than a decoder's stack of errors. Returns the clean bytes and the extension
// they should be saved under.
func Sanitize(data []byte) ([]byte, string, error) {
	if len(data) > MaxImageBytes {
		return nil, "", ErrTooLarge
	}
	kind := sniff(data)
	if kind == "" {
		return nil, "", ErrNotImage
	}
	img, decoded, err := image.Decode(bytes.NewReader(data))
	if err != nil || decoded != kind {
		return nil, "", ErrNotImage
	}
	var out bytes.Buffer
	switch kind {
	case "jpeg":
		err = jpeg.Encode(&out, img, &jpeg.Options{Quality: jpegQuality})
	case "png":
		err = png.Encode(&out, img)
	}
	if err != nil {
		return nil, "", fmt.Errorf("upload: re-encoding the image: %w", err)
	}
	if out.Len() > MaxImageBytes {
		return nil, "", ErrTooLarge
	}
	ext := ".jpg"
	if kind == "png" {
		ext = ".png"
	}
	return out.Bytes(), ext, nil
}

// sniff names the format by its magic bytes, "" for anything else. The
// declared filename and Content-Type are not consulted: bytes do not lie
// about themselves.
func sniff(data []byte) string {
	switch {
	case len(data) > 3 && bytes.Equal(data[:3], []byte{0xff, 0xd8, 0xff}):
		return "jpeg"
	case len(data) > 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "png"
	default:
		return ""
	}
}
