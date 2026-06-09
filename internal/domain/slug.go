package domain

import (
	"crypto/rand"
	"fmt"
	"io"
	"strings"
)

// slugAlphabet is base62: digits, then lowercase, then uppercase.
const slugAlphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// SlugLength is the number of base62 characters in a minted slug.
const SlugLength = 6

// maxSlugMintAttempts bounds collision retries before giving up.
const maxSlugMintAttempts = 100

// ValidateSlug checks that slug is a clean SlugLength base62 identity.
func ValidateSlug(slug string) error {
	if len(slug) != SlugLength {
		return fmt.Errorf("slug %q must be %d characters", slug, SlugLength)
	}
	for _, c := range slug {
		if !strings.ContainsRune(slugAlphabet, c) {
			return fmt.Errorf("slug %q contains non-base62 character %q", slug, c)
		}
	}
	return nil
}

// MintSlug returns a fresh SlugLength base62 slug for which reserved is false,
// drawing randomness from crypto/rand. reserved reports whether a candidate is
// already taken; pass Manifest.SlugReserved to honor the reservation table.
func MintSlug(reserved func(string) bool) (string, error) {
	return MintSlugWith(rand.Reader, reserved)
}

// MintSlugWith is MintSlug with an injectable randomness source for tests.
func MintSlugWith(r io.Reader, reserved func(string) bool) (string, error) {
	if reserved == nil {
		reserved = func(string) bool { return false }
	}
	for attempt := 0; attempt < maxSlugMintAttempts; attempt++ {
		slug, err := randomSlug(r)
		if err != nil {
			return "", err
		}
		if !reserved(slug) {
			return slug, nil
		}
	}
	return "", fmt.Errorf("mint slug: exhausted %d attempts", maxSlugMintAttempts)
}

// randomSlug reads bytes from r and maps them onto the base62 alphabet using
// rejection sampling so every character is uniformly distributed.
func randomSlug(r io.Reader) (string, error) {
	limit := 256 - (256 % len(slugAlphabet))
	out := make([]byte, SlugLength)
	buf := make([]byte, 1)
	for i := 0; i < SlugLength; {
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", fmt.Errorf("read random: %w", err)
		}
		if int(buf[0]) >= limit {
			continue
		}
		out[i] = slugAlphabet[int(buf[0])%len(slugAlphabet)]
		i++
	}
	return string(out), nil
}

// SlugReserved reports whether slug is recorded in the reservation table.
func (m *Manifest) SlugReserved(slug string) bool {
	if m == nil {
		return false
	}
	for _, s := range m.ReservedSlugs {
		if s == slug {
			return true
		}
	}
	return false
}

// ReserveSlug records slug in the reservation table if not already present.
// Slugs stay reserved for the life of the manifest and are never auto-reused.
func (m *Manifest) ReserveSlug(slug string) {
	if m == nil || m.SlugReserved(slug) {
		return
	}
	m.ReservedSlugs = append(m.ReservedSlugs, slug)
}

// MintAndReserveSlug mints a slug not present in this manifest's reservation
// table and records the reservation before returning.
func (m *Manifest) MintAndReserveSlug() (string, error) {
	slug, err := MintSlug(m.SlugReserved)
	if err != nil {
		return "", err
	}
	m.ReserveSlug(slug)
	return slug, nil
}
