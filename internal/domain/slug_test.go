package domain

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// infiniteReader yields the same byte forever, for exhaustion tests.
type infiniteReader struct{ b byte }

func (r infiniteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}
	return len(p), nil
}

func TestMintSlugWith(t *testing.T) {
	t.Run("maps bytes onto base62 alphabet", func(t *testing.T) {
		got, err := MintSlugWith(bytes.NewReader([]byte{0, 1, 2, 3, 4, 5}), nil)
		if err != nil {
			t.Fatalf("MintSlugWith: %v", err)
		}
		if got != "012345" {
			t.Fatalf("MintSlugWith = %q, want %q", got, "012345")
		}
	})

	t.Run("rejection sampling skips out-of-range bytes", func(t *testing.T) {
		// 250 >= 248 (largest multiple of 62 under 256) is rejected, then 0..5 map cleanly.
		got, err := MintSlugWith(bytes.NewReader([]byte{250, 0, 1, 2, 3, 4, 5}), nil)
		if err != nil {
			t.Fatalf("MintSlugWith: %v", err)
		}
		if got != "012345" {
			t.Fatalf("MintSlugWith = %q, want %q", got, "012345")
		}
	})

	t.Run("retries on collision", func(t *testing.T) {
		// First candidate "000000" is reserved; second "000001" is free.
		src := bytes.NewReader([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
		got, err := MintSlugWith(src, func(s string) bool { return s == "000000" })
		if err != nil {
			t.Fatalf("MintSlugWith: %v", err)
		}
		if got != "000001" {
			t.Fatalf("MintSlugWith = %q, want %q", got, "000001")
		}
	})

	t.Run("exhausts attempts when everything is reserved", func(t *testing.T) {
		_, err := MintSlugWith(infiniteReader{b: 0}, func(string) bool { return true })
		if err == nil {
			t.Fatal("MintSlugWith err = nil, want exhaustion error")
		}
		if !strings.Contains(err.Error(), "exhausted") {
			t.Fatalf("MintSlugWith err = %q, want containing %q", err.Error(), "exhausted")
		}
	})

	t.Run("propagates reader errors", func(t *testing.T) {
		sentinel := errors.New("boom")
		_, err := MintSlugWith(errReader{err: sentinel}, nil)
		if err == nil {
			t.Fatal("MintSlugWith err = nil, want error")
		}
		if !errors.Is(err, sentinel) {
			t.Fatalf("MintSlugWith err = %v, want wrapping %v", err, sentinel)
		}
	})
}

func TestMintSlugCryptoRand(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		slug, err := MintSlug(nil)
		if err != nil {
			t.Fatalf("MintSlug: %v", err)
		}
		if len(slug) != SlugLength {
			t.Fatalf("MintSlug = %q, want length %d", slug, SlugLength)
		}
		for _, c := range slug {
			if !strings.ContainsRune(slugAlphabet, c) {
				t.Fatalf("MintSlug = %q contains non-base62 rune %q", slug, c)
			}
		}
		seen[slug] = struct{}{}
	}
	// Random 6-char base62 should not collide across 100 draws in practice.
	if len(seen) < 100 {
		t.Fatalf("MintSlug produced %d unique of 100 draws", len(seen))
	}
}

func TestSlugReservation(t *testing.T) {
	m := NewManifest()

	if m.SlugReserved("f76") {
		t.Fatal("SlugReserved(f76) = true on empty manifest, want false")
	}

	m.ReserveSlug("f76")
	if !m.SlugReserved("f76") {
		t.Fatal("SlugReserved(f76) = false after ReserveSlug, want true")
	}

	m.ReserveSlug("f76") // idempotent
	if got := len(m.ReservedSlugs); got != 1 {
		t.Fatalf("ReservedSlugs length = %d after duplicate reserve, want 1", got)
	}
}

func TestMintAndReserveSlug(t *testing.T) {
	m := NewManifest()

	slug, err := m.MintAndReserveSlug()
	if err != nil {
		t.Fatalf("MintAndReserveSlug: %v", err)
	}
	if !m.SlugReserved(slug) {
		t.Fatalf("minted slug %q not reserved", slug)
	}

	second, err := m.MintAndReserveSlug()
	if err != nil {
		t.Fatalf("MintAndReserveSlug: %v", err)
	}
	if second == slug {
		t.Fatalf("MintAndReserveSlug reused reserved slug %q", slug)
	}
	if len(m.ReservedSlugs) != 2 {
		t.Fatalf("ReservedSlugs length = %d, want 2", len(m.ReservedSlugs))
	}
}
