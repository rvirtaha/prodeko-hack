// Package session issues and verifies the proxy's own editor sessions.
//
// It is the only place the rest of the proxy learns who the editor is. A
// session token is a sealed AES-256-GCM blob carrying an [Identity] that was
// snapshotted from a verified Keycloak login. The blob is self-contained, so
// tokens survive a restart of the container; the price is that revocation
// lives in memory and does not.
//
// Sessions deliberately outlive the Keycloak access token. Decap CMS never
// renews a login: it stores the token in localStorage and replays it forever.
// The proxy therefore holds no Keycloak token after sign-in and no refresh
// token; the roles in [Identity] are the roles the editor held at sign-in.
// Removing an editor's role in Keycloak takes effect when their session
// expires, or immediately via [Store.Revoke].
package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Identity is a verified Keycloak identity, snapshotted at sign-in.
// Roles are the realm roles the editor held at that moment.
type Identity struct {
	Subject   string    `json:"sub"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Username  string    `json:"preferred_username"`
	Roles     []string  `json:"roles"`
	IssuedAt  time.Time `json:"iat"`
	ExpiresAt time.Time `json:"exp"`
}

// HasRole reports whether the editor held role at sign-in.
func (i Identity) HasRole(role string) bool {
	if role == "" {
		return false
	}
	for _, r := range i.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// MissingRoles returns those of required the editor did not hold at sign-in,
// in the order given. Empty means the editor held every one of them.
//
// Editing requires all of the configured roles, never any of them. The
// conjunction is the point: the hand-granted permission to edit stops working
// the moment the automatically maintained membership role lapses, rather than
// waiting for somebody to remember to revoke it.
func (i Identity) MissingRoles(required []string) []string {
	var missing []string
	for _, role := range required {
		if !i.HasRole(role) {
			missing = append(missing, role)
		}
	}
	return missing
}

var (
	ErrNoToken      = errors.New("session: no token")
	ErrInvalidToken = errors.New("session: invalid token")
	ErrExpired      = errors.New("session: expired")
	ErrRevoked      = errors.New("session: revoked")
)

// DefaultTTL is how long a session lives when [Options].TTL is zero.
// Decap never renews a login, so this deliberately outlives the Keycloak
// access token.
const DefaultTTL = 12 * time.Hour

// MinSecretLen is the shortest SESSION_SECRET [NewStore] will accept.
const MinSecretLen = 32

// tokenPrefix marks the token format. It is also the AEAD associated data, so
// a blob sealed by a future format cannot be opened as this one.
const tokenPrefix = "pkd1_"

// maxTokenLen bounds the work an unauthenticated request can cause.
const maxTokenLen = 8 << 10

type Options struct {
	Secret []byte           // SESSION_SECRET; at least MinSecretLen bytes
	TTL    time.Duration    // zero means DefaultTTL
	Now    func() time.Time // zero value means time.Now
}

// Store mints and checks session tokens. Tokens are self-contained sealed
// blobs, so they survive a restart; Revoke is backed by an in-memory set that
// does not.
type Store struct {
	aead cipher.AEAD
	ttl  time.Duration
	now  func() time.Time

	mu      sync.Mutex
	revoked map[string]time.Time // jti -> original expiry
}

// envelope is the plaintext sealed into a token. The jti exists only so a
// token can be named on the revocation list without storing the token itself.
type envelope struct {
	JTI string `json:"jti"`
	Identity
}

func NewStore(opts Options) (*Store, error) {
	if len(opts.Secret) < MinSecretLen {
		return nil, fmt.Errorf("session: SESSION_SECRET must be at least %d bytes, got %d", MinSecretLen, len(opts.Secret))
	}
	ttl := opts.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}
	if ttl < 0 {
		return nil, errors.New("session: TTL must not be negative")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	key := sha256.Sum256(opts.Secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}

	return &Store{
		aead:    aead,
		ttl:     ttl,
		now:     now,
		revoked: make(map[string]time.Time),
	}, nil
}

// TTL is how long a freshly issued session lasts.
func (s *Store) TTL() time.Duration { return s.ttl }

// Issue seals id into a bearer token, filling IssuedAt and ExpiresAt.
func (s *Store) Issue(id Identity) (string, time.Time, error) {
	now := s.now().UTC().Truncate(time.Second)
	id.IssuedAt = now
	id.ExpiresAt = now.Add(s.ttl)

	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", time.Time{}, fmt.Errorf("session: %w", err)
	}

	plaintext, err := json.Marshal(envelope{
		JTI:      base64.RawURLEncoding.EncodeToString(jti),
		Identity: id,
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("session: %w", err)
	}

	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", time.Time{}, fmt.Errorf("session: %w", err)
	}
	sealed := s.aead.Seal(nonce, nonce, plaintext, []byte(tokenPrefix))

	return tokenPrefix + base64.RawURLEncoding.EncodeToString(sealed), id.ExpiresAt, nil
}

// Verify opens and validates token. Errors wrap [ErrNoToken],
// [ErrInvalidToken], [ErrExpired] or [ErrRevoked].
func (s *Store) Verify(token string) (Identity, error) {
	env, err := s.open(token)
	if err != nil {
		return Identity{}, err
	}
	if !s.now().Before(env.ExpiresAt) {
		return Identity{}, ErrExpired
	}
	if s.isRevoked(env.JTI) {
		return Identity{}, ErrRevoked
	}
	return env.Identity, nil
}

// Revoke invalidates a still-valid token for the remainder of its TTL.
// Revoking an already expired or already revoked token is a no-op.
func (s *Store) Revoke(token string) error {
	env, err := s.open(token)
	if err != nil {
		return err
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneRevokedLocked(now)
	if !now.Before(env.ExpiresAt) {
		return nil
	}
	s.revoked[env.JTI] = env.ExpiresAt
	return nil
}

// open decrypts and decodes a token without checking expiry or revocation.
func (s *Store) open(token string) (envelope, error) {
	if token == "" {
		return envelope{}, ErrNoToken
	}
	if len(token) > maxTokenLen {
		return envelope{}, ErrInvalidToken
	}
	rest, ok := strings.CutPrefix(token, tokenPrefix)
	if !ok {
		return envelope{}, ErrInvalidToken
	}
	sealed, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil {
		return envelope{}, ErrInvalidToken
	}
	ns := s.aead.NonceSize()
	if len(sealed) < ns+s.aead.Overhead() {
		return envelope{}, ErrInvalidToken
	}
	plaintext, err := s.aead.Open(nil, sealed[:ns], sealed[ns:], []byte(tokenPrefix))
	if err != nil {
		return envelope{}, ErrInvalidToken
	}
	var env envelope
	if err := json.Unmarshal(plaintext, &env); err != nil {
		return envelope{}, ErrInvalidToken
	}
	if env.JTI == "" || env.Subject == "" || env.ExpiresAt.IsZero() {
		return envelope{}, ErrInvalidToken
	}
	return env, nil
}

func (s *Store) isRevoked(jti string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneRevokedLocked(s.now())
	_, ok := s.revoked[jti]
	return ok
}

// pruneRevokedLocked drops deny-list entries whose token would have expired on
// its own. The list only ever holds live revocations, so it stays small.
func (s *Store) pruneRevokedLocked(now time.Time) {
	for jti, exp := range s.revoked {
		if !now.Before(exp) {
			delete(s.revoked, jti)
		}
	}
}
