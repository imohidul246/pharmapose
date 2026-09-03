// Package auth provides password hashing, session validation, role-based
// authorization and HTTP middleware for the pharmacy ERP. Sessions are
// server-side: the browser holds an opaque bearer token in an HttpOnly cookie
// and the database keeps only its SHA-256 hash.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. These are tuned for a cheap interactive login on the
// single-binary pharmacy server; memory 64 MiB, one pass, four lanes.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

var (
	// ErrWeakPassword is returned when a password fails the minimum strength rule.
	ErrWeakPassword = errors.New("password must be between 8 and 72 characters")
	// ErrInvalidHash is returned when a stored hash cannot be parsed.
	ErrInvalidHash = errors.New("stored password hash is malformed")
)

// HashPassword derives an Argon2id hash in the modular PHC string format,
// e.g. $argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>.
func HashPassword(password string) (string, error) {
	if len(password) < 8 || len(password) > 72 {
		return "", ErrWeakPassword
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a plaintext password against a stored PHC string.
// It is deliberately constant-time on the derived key comparison.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// parts: ["", "argon2id", "v=19", "m=65536,t=1,p=4", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var version uint32
	var memory, iterations, parallelism uint32
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}
	if version != argon2.Version {
		return false, ErrInvalidHash
	}
	derived := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism), uint32(len(expected)))
	if len(derived) != len(expected) {
		return false, ErrInvalidHash
	}
	return subtle.ConstantTimeCompare(derived, expected) == 1, nil
}

// NewSessionToken returns a fresh 32-byte URL-safe random session token.
func NewSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashSessionToken hashes a raw session token into the 32-byte hex digest the
// database stores. The raw token must never be persisted anywhere.
func HashSessionToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

// PrincipalFromContext returns the authenticated principal bound to the request
// context, or nil when the request is not authenticated.
func PrincipalFromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

type principalKey struct{}

// WithPrincipal returns a context carrying the authenticated principal.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}