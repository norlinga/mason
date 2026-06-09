package domain

import "strings"

// ObjectURL returns the long, readable URL for a share's object path:
// {baseURL}/{objectPath}.
func ObjectURL(baseURL, objectPath string) string {
	return joinURL(baseURL, objectPath)
}

// SlugURL returns the served short URL for a slug endpoint: {baseURL}/s/{slug}{ext}.
// The extension is carried so the web server infers the Content-Type; the slug
// identity itself stays clean.
func SlugURL(baseURL, slug, ext string) string {
	return joinURL(baseURL, "s/"+slug+ext)
}

func joinURL(baseURL, rel string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	rel = strings.TrimLeft(strings.TrimSpace(rel), "/")
	if base == "" {
		return rel
	}
	if rel == "" {
		return base
	}
	return base + "/" + rel
}
