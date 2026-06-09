package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

const ManifestVersion = 1

// EndpointState is the serving state of a share endpoint.
type EndpointState string

const (
	EndpointOK       EndpointState = "ok"
	EndpointDisabled EndpointState = "disabled"
)

// Manifest is the authoritative remote index for the share workflow.
type Manifest struct {
	Version       int             `json:"version"`
	Blobs         map[string]Blob `json:"blobs"`
	Shares        []Share         `json:"shares"`
	ReservedSlugs []string        `json:"reserved_slugs"`
}

// Blob describes one content-addressed object on the remote host.
type Blob struct {
	Size       int64     `json:"size"`
	UploadedAt time.Time `json:"uploaded_at"`
}

// Share is a blob plus up to two independent endpoints.
type Share struct {
	ID        string        `json:"id"`
	Blob      string        `json:"blob"`
	Path      *Endpoint     `json:"path"`
	Slug      *SlugEndpoint `json:"slug"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// Endpoint is an object-path link endpoint.
type Endpoint struct {
	Value string        `json:"value"`
	State EndpointState `json:"state"`
}

// SlugEndpoint is a short-slug link endpoint.
type SlugEndpoint struct {
	Value string        `json:"value"`
	Ext   string        `json:"ext"`
	State EndpointState `json:"state"`
}

// Indexes map endpoint identities to shares. They are derived from Shares.
type Indexes struct {
	ByPath map[string]*Share
	BySlug map[string]*Share
}

// NewManifest returns an empty v1 manifest.
func NewManifest() *Manifest {
	return &Manifest{
		Version:       ManifestVersion,
		Blobs:         make(map[string]Blob),
		Shares:        []Share{},
		ReservedSlugs: []string{},
	}
}

// ParseManifest decodes and validates manifest JSON.
func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// MarshalManifest encodes a manifest as indented JSON.
func MarshalManifest(manifest *Manifest) ([]byte, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return append(data, '\n'), nil
}

// Validate checks manifest invariants.
func (m *Manifest) Validate() error {
	if m == nil {
		return fmt.Errorf("manifest is required")
	}
	if m.Version != ManifestVersion {
		return fmt.Errorf("unsupported manifest version %d", m.Version)
	}
	if m.Blobs == nil {
		return fmt.Errorf("manifest blobs map is required")
	}
	if m.Shares == nil {
		return fmt.Errorf("manifest shares list is required")
	}
	if m.ReservedSlugs == nil {
		return fmt.Errorf("manifest reserved_slugs list is required")
	}
	if err := validateBlobKeys(m.Blobs); err != nil {
		return err
	}
	if err := validateReservedSlugs(m.ReservedSlugs); err != nil {
		return err
	}
	reservedSlugs := reservedSlugSet(m.ReservedSlugs)

	seenIDs := make(map[string]struct{}, len(m.Shares))
	indexes := BuildIndexes(m)
	for i := range m.Shares {
		share := &m.Shares[i]
		if share.ID == "" {
			return fmt.Errorf("share at index %d requires id", i)
		}
		if _, ok := seenIDs[share.ID]; ok {
			return fmt.Errorf("duplicate share id %q", share.ID)
		}
		seenIDs[share.ID] = struct{}{}
		if share.Blob == "" {
			return fmt.Errorf("share %q requires blob", share.ID)
		}
		if _, ok := m.Blobs[share.Blob]; !ok {
			return fmt.Errorf("share %q references unknown blob %q", share.ID, share.Blob)
		}
		if share.Path == nil && share.Slug == nil {
			return fmt.Errorf("share %q must retain at least one endpoint", share.ID)
		}
		if err := validatePathEndpoint(share, indexes); err != nil {
			return err
		}
		if err := validateSlugEndpoint(share, indexes, reservedSlugs); err != nil {
			return err
		}
	}
	return nil
}

func validateBlobKeys(blobs map[string]Blob) error {
	for blob := range blobs {
		if _, err := BlobHex(blob); err != nil {
			return fmt.Errorf("invalid blob key %q: %w", blob, err)
		}
	}
	return nil
}

func reservedSlugSet(slugs []string) map[string]struct{} {
	reserved := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		reserved[slug] = struct{}{}
	}
	return reserved
}

func validateReservedSlugs(slugs []string) error {
	seen := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		if err := ValidateSlug(slug); err != nil {
			return fmt.Errorf("invalid reserved slug: %w", err)
		}
		if _, ok := seen[slug]; ok {
			return fmt.Errorf("duplicate reserved slug %q", slug)
		}
		seen[slug] = struct{}{}
	}
	return nil
}

func validatePathEndpoint(share *Share, indexes Indexes) error {
	if share.Path == nil {
		return nil
	}
	if share.Path.Value == "" {
		return fmt.Errorf("share %q path endpoint requires value", share.ID)
	}
	normalized, err := NormalizeObjectPath(share.Path.Value)
	if err != nil {
		return fmt.Errorf("share %q has invalid path endpoint: %w", share.ID, err)
	}
	if normalized != share.Path.Value {
		return fmt.Errorf("share %q path endpoint must be canonical: %q", share.ID, share.Path.Value)
	}
	if !share.Path.State.Valid() {
		return fmt.Errorf("share %q has invalid path state %q", share.ID, share.Path.State)
	}
	if other, ok := indexes.ByPath[share.Path.Value]; ok && other.ID != share.ID {
		return fmt.Errorf("duplicate object path %q", share.Path.Value)
	}
	return nil
}

func validateSlugEndpoint(share *Share, indexes Indexes, reservedSlugs map[string]struct{}) error {
	if share.Slug == nil {
		return nil
	}
	if share.Slug.Value == "" {
		return fmt.Errorf("share %q slug endpoint requires value", share.ID)
	}
	if err := ValidateSlug(share.Slug.Value); err != nil {
		return fmt.Errorf("share %q has invalid slug endpoint: %w", share.ID, err)
	}
	if _, ok := reservedSlugs[share.Slug.Value]; !ok {
		return fmt.Errorf("share %q slug %q is not reserved", share.ID, share.Slug.Value)
	}
	if !share.Slug.State.Valid() {
		return fmt.Errorf("share %q has invalid slug state %q", share.ID, share.Slug.State)
	}
	if other, ok := indexes.BySlug[share.Slug.Value]; ok && other.ID != share.ID {
		return fmt.Errorf("duplicate slug %q", share.Slug.Value)
	}
	return nil
}

// Valid reports whether state is a known endpoint state.
func (s EndpointState) Valid() bool {
	return s == EndpointOK || s == EndpointDisabled
}

// EndpointRefCount returns how many manifest endpoints on the share reference its blob.
// Disabled endpoints still count; removed endpoints do not.
func (s Share) EndpointRefCount() int {
	count := 0
	if s.Path != nil {
		count++
	}
	if s.Slug != nil {
		count++
	}
	return count
}

// Active reports whether the share still has at least one endpoint.
func (s Share) Active() bool {
	return s.EndpointRefCount() > 0
}

// BlobRefCount returns manifest endpoint references for blob across all shares.
func (m *Manifest) BlobRefCount(blob string) int {
	if m == nil {
		return 0
	}
	count := 0
	for i := range m.Shares {
		share := &m.Shares[i]
		if share.Blob != blob {
			continue
		}
		count += share.EndpointRefCount()
	}
	return count
}

// BlobRefCounts returns endpoint reference counts for every blob referenced by shares.
func (m *Manifest) BlobRefCounts() map[string]int {
	counts := make(map[string]int)
	if m == nil {
		return counts
	}
	for i := range m.Shares {
		share := &m.Shares[i]
		counts[share.Blob] += share.EndpointRefCount()
	}
	return counts
}

// OrphanedBlobs returns blob hashes listed in Blobs with zero manifest endpoint references.
func (m *Manifest) OrphanedBlobs() []string {
	if m == nil || len(m.Blobs) == 0 {
		return nil
	}
	refCounts := m.BlobRefCounts()
	orphaned := make([]string, 0)
	for blob := range m.Blobs {
		if refCounts[blob] == 0 {
			orphaned = append(orphaned, blob)
		}
	}
	sort.Strings(orphaned)
	return orphaned
}

// BuildIndexes derives path and slug lookup tables from shares.
func BuildIndexes(m *Manifest) Indexes {
	indexes := Indexes{
		ByPath: make(map[string]*Share),
		BySlug: make(map[string]*Share),
	}
	if m == nil {
		return indexes
	}
	for i := range m.Shares {
		share := &m.Shares[i]
		if share.Path != nil {
			indexes.ByPath[share.Path.Value] = share
		}
		if share.Slug != nil {
			indexes.BySlug[share.Slug.Value] = share
		}
	}
	return indexes
}

// FindShareByPath returns the share owning an object path, if any.
func (m *Manifest) FindShareByPath(path string) (*Share, bool) {
	if m == nil {
		return nil, false
	}
	share, ok := BuildIndexes(m).ByPath[path]
	return share, ok
}

// FindShareBySlug returns the share owning a slug identity, if any.
func (m *Manifest) FindShareBySlug(slug string) (*Share, bool) {
	if m == nil {
		return nil, false
	}
	share, ok := BuildIndexes(m).BySlug[slug]
	return share, ok
}
