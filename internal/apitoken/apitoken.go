// Package apitoken mints, parses and hashes API tokens: "vq_" followed by
// 32 random bytes in base64url (43 characters). Only Hash(raw) and
// Prefix(raw) are stored; a token is shown once, at creation. Tokens carry
// 256 bits of entropy, so a plain SHA-256 is enough: no server secret.
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"
)

const (
	// Scheme starts every token.
	Scheme = "vq_"
	// TouchEvery is the most often a token's last-used time is written.
	TouchEvery = 5 * time.Minute

	bodyLen    = 43
	prefixBody = 6
)

// Mint returns a new random token.
func Mint() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return Scheme + base64.RawURLEncoding.EncodeToString(b), nil
}

// Valid reports whether raw has a token's shape.
func Valid(raw string) bool {
	body, ok := strings.CutPrefix(raw, Scheme)
	if !ok || len(body) != bodyLen {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(body)
	return err == nil && len(b) == 32
}

// Parse reads the token from an Authorization header ("Bearer vq_…", the
// scheme in any case).
func Parse(header string) (string, bool) {
	scheme, rest, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	raw := strings.TrimSpace(rest)
	return raw, Valid(raw)
}

// Prefix is the part of a token shown in lists: "vq_" and 6 more.
func Prefix(raw string) string { return raw[:len(Scheme)+prefixBody] }

// Hash is what is stored and looked up.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
