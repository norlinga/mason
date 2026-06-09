package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// BlobHashPrefix is the algorithm tag carried on every blob key.
const BlobHashPrefix = "sha256:"

// HashReader streams r through SHA-256 and returns the blob key (sha256:<hex>).
func HashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", fmt.Errorf("hash content: %w", err)
	}
	return BlobHashPrefix + hex.EncodeToString(h.Sum(nil)), nil
}

// HashBytes returns the blob key (sha256:<hex>) for b.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return BlobHashPrefix + hex.EncodeToString(sum[:])
}

// BlobHex returns the bare lowercase hex digest of a blob key, validating its shape.
func BlobHex(blob string) (string, error) {
	digest, ok := strings.CutPrefix(blob, BlobHashPrefix)
	if !ok {
		return "", fmt.Errorf("blob key %q missing %q prefix", blob, BlobHashPrefix)
	}
	if len(digest) != sha256.Size*2 {
		return "", fmt.Errorf("blob key %q has invalid digest length", blob)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("blob key %q has non-hex digest", blob)
	}
	return strings.ToLower(digest), nil
}

// BlobPath returns the content-addressed storage path for a blob key, relative to
// the site root: objects/{hash[0:2]}/{hash[2:4]}/{hash}. The blob file carries no
// extension; the extension lives in the manifest, not the path.
func BlobPath(blob string) (string, error) {
	digest, err := BlobHex(blob)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("objects/%s/%s/%s", digest[0:2], digest[2:4], digest), nil
}
