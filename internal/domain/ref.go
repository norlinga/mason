package domain

import (
	"fmt"
	"strings"
)

// RefKind distinguishes object-path refs from short-slug refs.
type RefKind int

const (
	RefKindPath RefKind = iota
	RefKindSlug
)

// Ref is a parsed share reference from a CLI argument.
type Ref struct {
	Kind  RefKind
	Value string
}

const slugRefPrefix = "s/"

// ParseRef interprets a CLI <ref> as either an object path or a short slug.
// Mason treats an s/-prefixed token as a slug; anything else is an object path.
func ParseRef(input string) (Ref, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Ref{}, fmt.Errorf("ref is required")
	}
	if strings.HasPrefix(trimmed, slugRefPrefix) {
		slug := strings.TrimSpace(trimmed[len(slugRefPrefix):])
		if slug == "" {
			return Ref{}, fmt.Errorf("slug ref requires value after %q", slugRefPrefix)
		}
		return Ref{Kind: RefKindSlug, Value: slug}, nil
	}
	return Ref{Kind: RefKindPath, Value: trimmed}, nil
}

// IsSlug reports whether the ref names a short-slug endpoint.
func (r Ref) IsSlug() bool {
	return r.Kind == RefKindSlug
}

// IsPath reports whether the ref names an object-path endpoint.
func (r Ref) IsPath() bool {
	return r.Kind == RefKindPath
}
