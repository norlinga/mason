package domain

import (
	"strings"
	"testing"
	"time"
)

const (
	manifestBlobA = "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	manifestBlobB = "sha256:486ea46224d1bb4fb680f34f7c9ad96a8f24ec88be73ea8e5a6c65260e9cb8a7"
	manifestBlobC = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

func TestParseAndMarshalManifestRoundTrip(t *testing.T) {
	uploadedAt := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	createdAt := uploadedAt
	updatedAt := uploadedAt.Add(time.Hour)

	original := &Manifest{
		Version: ManifestVersion,
		Blobs: map[string]Blob{
			manifestBlobA: {Size: 4412345, UploadedAt: uploadedAt},
		},
		Shares: []Share{
			{
				ID:        "01J9Z",
				Blob:      manifestBlobA,
				Path:      &Endpoint{Value: "super/rad/podcasts/episode-01.mp3", State: EndpointOK},
				Slug:      &SlugEndpoint{Value: "f76", Ext: ".mp3", State: EndpointOK},
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
		},
		ReservedSlugs: []string{"f76"},
	}

	data, err := MarshalManifest(original)
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}

	got, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if got.Version != original.Version {
		t.Fatalf("Version = %d, want %d", got.Version, original.Version)
	}
	if len(got.Blobs) != 1 || got.Blobs[manifestBlobA].Size != 4412345 {
		t.Fatalf("Blobs = %#v, want one test blob", got.Blobs)
	}
	if len(got.Shares) != 1 {
		t.Fatalf("Shares length = %d, want 1", len(got.Shares))
	}
	share := got.Shares[0]
	if share.Path == nil || share.Path.Value != "super/rad/podcasts/episode-01.mp3" {
		t.Fatalf("Path = %#v, want episode path", share.Path)
	}
	if share.Slug == nil || share.Slug.Value != "f76" || share.Slug.Ext != ".mp3" {
		t.Fatalf("Slug = %#v, want f76 slug", share.Slug)
	}
	if len(got.ReservedSlugs) != 1 || got.ReservedSlugs[0] != "f76" {
		t.Fatalf("ReservedSlugs = %#v, want [f76]", got.ReservedSlugs)
	}
}

func TestNewManifestIsValid(t *testing.T) {
	manifest := NewManifest()
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	data, err := MarshalManifest(manifest)
	if err != nil {
		t.Fatalf("MarshalManifest: %v", err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Fatalf("marshaled manifest = %q, want version 1", string(data))
	}
}

func TestEndpointRefCount(t *testing.T) {
	tests := []struct {
		name  string
		share Share
		want  int
	}{
		{
			name: "both endpoints",
			share: Share{
				Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointOK},
				Slug: &SlugEndpoint{Value: "f76", Ext: ".mp3", State: EndpointOK},
			},
			want: 2,
		},
		{
			name: "disabled endpoints still count",
			share: Share{
				Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointDisabled},
				Slug: &SlugEndpoint{Value: "f76", Ext: ".mp3", State: EndpointDisabled},
			},
			want: 2,
		},
		{
			name: "path only",
			share: Share{
				Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointOK},
			},
			want: 1,
		},
		{
			name: "slug only",
			share: Share{
				Slug: &SlugEndpoint{Value: "f76", Ext: ".mp3", State: EndpointOK},
			},
			want: 1,
		},
		{
			name:  "no endpoints",
			share: Share{},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.share.EndpointRefCount(); got != tt.want {
				t.Fatalf("EndpointRefCount() = %d, want %d", got, tt.want)
			}
			if got := tt.share.Active(); got != (tt.want > 0) {
				t.Fatalf("Active() = %v, want %v", got, tt.want > 0)
			}
		})
	}
}

func TestBlobRefCountAggregatesAcrossShares(t *testing.T) {
	manifest := &Manifest{
		Version: ManifestVersion,
		Blobs: map[string]Blob{
			manifestBlobA: {Size: 10},
			manifestBlobB: {Size: 20},
		},
		Shares: []Share{
			{
				ID:   "share-1",
				Blob: manifestBlobA,
				Path: &Endpoint{Value: "a/foo.mp3", State: EndpointOK},
				Slug: &SlugEndpoint{Value: "f76", Ext: ".mp3", State: EndpointDisabled},
			},
			{
				ID:   "share-2",
				Blob: manifestBlobA,
				Path: &Endpoint{Value: "b/foo.mp3", State: EndpointOK},
			},
			{
				ID:   "share-3",
				Blob: manifestBlobB,
				Slug: &SlugEndpoint{Value: "g42", Ext: ".mp3", State: EndpointOK},
			},
		},
		ReservedSlugs: []string{"f76", "g42"},
	}

	if got, want := manifest.BlobRefCount(manifestBlobA), 3; got != want {
		t.Fatalf("BlobRefCount(abc) = %d, want %d", got, want)
	}
	if got, want := manifest.BlobRefCount(manifestBlobB), 1; got != want {
		t.Fatalf("BlobRefCount(def) = %d, want %d", got, want)
	}
	if got, want := manifest.BlobRefCount(manifestBlobC), 0; got != want {
		t.Fatalf("BlobRefCount(missing) = %d, want %d", got, want)
	}

	counts := manifest.BlobRefCounts()
	if counts[manifestBlobA] != 3 || counts[manifestBlobB] != 1 {
		t.Fatalf("BlobRefCounts() = %#v, want abc=3 def=1", counts)
	}
}

func TestOrphanedBlobs(t *testing.T) {
	manifest := &Manifest{
		Version: ManifestVersion,
		Blobs: map[string]Blob{
			manifestBlobA: {Size: 10},
			manifestBlobB: {Size: 20},
			manifestBlobC: {Size: 30},
		},
		Shares: []Share{
			{
				ID:   "share-1",
				Blob: manifestBlobA,
				Path: &Endpoint{Value: "a/foo.mp3", State: EndpointOK},
			},
		},
		ReservedSlugs: []string{},
	}

	got := manifest.OrphanedBlobs()
	want := []string{manifestBlobB, manifestBlobC}
	if len(got) != len(want) {
		t.Fatalf("OrphanedBlobs() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("OrphanedBlobs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildIndexesAndLookup(t *testing.T) {
	manifest := &Manifest{
		Version: ManifestVersion,
		Blobs:   map[string]Blob{},
		Shares: []Share{
			{
				ID:   "share-1",
				Blob: manifestBlobA,
				Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointOK},
				Slug: &SlugEndpoint{Value: "f76", Ext: ".mp3", State: EndpointOK},
			},
		},
		ReservedSlugs: []string{"f76"},
	}

	indexes := BuildIndexes(manifest)
	if indexes.ByPath["inbox/foo.mp3"].ID != "share-1" {
		t.Fatalf("ByPath lookup = %#v, want share-1", indexes.ByPath["inbox/foo.mp3"])
	}
	if indexes.BySlug["f76"].ID != "share-1" {
		t.Fatalf("BySlug lookup = %#v, want share-1", indexes.BySlug["f76"])
	}

	byPath, ok := manifest.FindShareByPath("inbox/foo.mp3")
	if !ok || byPath.ID != "share-1" {
		t.Fatalf("FindShareByPath = %#v, %v", byPath, ok)
	}
	bySlug, ok := manifest.FindShareBySlug("f76")
	if !ok || bySlug.ID != "share-1" {
		t.Fatalf("FindShareBySlug = %#v, %v", bySlug, ok)
	}
}

func TestValidateRejectsInvalidManifests(t *testing.T) {
	validShare := Share{
		ID:   "share-1",
		Blob: manifestBlobA,
		Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointOK},
	}

	tests := []struct {
		name string
		m    *Manifest
		want string
	}{
		{
			name: "nil manifest",
			m:    nil,
			want: "manifest is required",
		},
		{
			name: "unsupported version",
			m: &Manifest{
				Version:       2,
				Blobs:         map[string]Blob{},
				Shares:        []Share{},
				ReservedSlugs: []string{},
			},
			want: "unsupported manifest version",
		},
		{
			name: "missing blobs map",
			m: &Manifest{
				Version:       ManifestVersion,
				Shares:        []Share{},
				ReservedSlugs: []string{},
			},
			want: "blobs map is required",
		},
		{
			name: "invalid blob key",
			m: &Manifest{
				Version:       ManifestVersion,
				Blobs:         map[string]Blob{"sha256:abc": {}},
				Shares:        []Share{},
				ReservedSlugs: []string{},
			},
			want: "invalid blob key",
		},
		{
			name: "share without endpoints",
			m: &Manifest{
				Version:       ManifestVersion,
				Blobs:         map[string]Blob{manifestBlobA: {}},
				Shares:        []Share{{ID: "share-1", Blob: manifestBlobA}},
				ReservedSlugs: []string{},
			},
			want: "must retain at least one endpoint",
		},
		{
			name: "share references unknown blob",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{},
				Shares: []Share{
					{ID: "share-1", Blob: manifestBlobA, Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointOK}},
				},
				ReservedSlugs: []string{},
			},
			want: "references unknown blob",
		},
		{
			name: "duplicate object path",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{manifestBlobA: {}, manifestBlobB: {}},
				Shares: []Share{
					{ID: "share-1", Blob: manifestBlobA, Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointOK}},
					{ID: "share-2", Blob: manifestBlobB, Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointOK}},
				},
				ReservedSlugs: []string{},
			},
			want: "duplicate object path",
		},
		{
			name: "duplicate slug",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{manifestBlobA: {}, manifestBlobB: {}},
				Shares: []Share{
					{ID: "share-1", Blob: manifestBlobA, Slug: &SlugEndpoint{Value: "f76", State: EndpointOK}},
					{ID: "share-2", Blob: manifestBlobB, Slug: &SlugEndpoint{Value: "f76", State: EndpointOK}},
				},
				ReservedSlugs: []string{},
			},
			want: "duplicate slug",
		},
		{
			name: "invalid endpoint state",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{manifestBlobA: {}},
				Shares: []Share{
					{ID: "share-1", Blob: manifestBlobA, Path: &Endpoint{Value: "inbox/foo.mp3", State: "broken"}},
				},
				ReservedSlugs: []string{},
			},
			want: "invalid path state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.m.Validate()
			if err == nil {
				t.Fatalf("Validate succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}

	valid := &Manifest{
		Version:       ManifestVersion,
		Blobs:         map[string]Blob{manifestBlobA: {}},
		Shares:        []Share{validShare},
		ReservedSlugs: []string{},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid manifest Validate: %v", err)
	}
}

func TestParseManifestRejectsInvalidJSON(t *testing.T) {
	_, err := ParseManifest([]byte(`{"version":2,"blobs":{},"shares":[],"reserved_slugs":[]}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported manifest version") {
		t.Fatalf("ParseManifest error = %v, want unsupported version", err)
	}
}

func TestMarshalManifestRejectsNil(t *testing.T) {
	_, err := MarshalManifest(nil)
	if err == nil || !strings.Contains(err.Error(), "manifest is required") {
		t.Fatalf("MarshalManifest(nil) error = %v, want required error", err)
	}
}

func TestNilManifestHelpersReturnZeroValues(t *testing.T) {
	if got := (*Manifest)(nil).BlobRefCount(manifestBlobA); got != 0 {
		t.Fatalf("BlobRefCount on nil = %d, want 0", got)
	}
	if got := (*Manifest)(nil).OrphanedBlobs(); got != nil {
		t.Fatalf("OrphanedBlobs on nil = %#v, want nil", got)
	}
	if share, ok := (*Manifest)(nil).FindShareByPath("inbox/foo.mp3"); ok || share != nil {
		t.Fatalf("FindShareByPath on nil = %#v, %v, want nil false", share, ok)
	}
}

func TestEndpointStateValid(t *testing.T) {
	if !EndpointOK.Valid() || !EndpointDisabled.Valid() {
		t.Fatalf("known endpoint states should be valid")
	}
	if EndpointState("broken").Valid() {
		t.Fatalf("unknown endpoint state should be invalid")
	}
}

func TestParseManifestRejectsMalformedJSON(t *testing.T) {
	_, err := ParseManifest([]byte(`{`))
	if err == nil || !strings.Contains(err.Error(), "parse manifest") {
		t.Fatalf("ParseManifest error = %v, want parse error", err)
	}
}

func TestValidateRejectsAdditionalShareProblems(t *testing.T) {
	tests := []struct {
		name string
		m    *Manifest
		want string
	}{
		{
			name: "nil shares list",
			m: &Manifest{
				Version:       ManifestVersion,
				Blobs:         map[string]Blob{},
				ReservedSlugs: []string{},
			},
			want: "shares list is required",
		},
		{
			name: "nil reserved slugs list",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{},
				Shares:  []Share{},
			},
			want: "reserved_slugs list is required",
		},
		{
			name: "missing share id",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{},
				Shares: []Share{
					{Blob: manifestBlobA, Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointOK}},
				},
				ReservedSlugs: []string{},
			},
			want: "requires id",
		},
		{
			name: "duplicate share id",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{manifestBlobA: {}, manifestBlobB: {}},
				Shares: []Share{
					{ID: "share-1", Blob: manifestBlobA, Path: &Endpoint{Value: "a/foo.mp3", State: EndpointOK}},
					{ID: "share-1", Blob: manifestBlobB, Path: &Endpoint{Value: "b/foo.mp3", State: EndpointOK}},
				},
				ReservedSlugs: []string{},
			},
			want: "duplicate share id",
		},
		{
			name: "missing blob",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{},
				Shares: []Share{
					{ID: "share-1", Path: &Endpoint{Value: "inbox/foo.mp3", State: EndpointOK}},
				},
				ReservedSlugs: []string{},
			},
			want: "requires blob",
		},
		{
			name: "missing path value",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{manifestBlobA: {}},
				Shares: []Share{
					{ID: "share-1", Blob: manifestBlobA, Path: &Endpoint{State: EndpointOK}},
				},
				ReservedSlugs: []string{},
			},
			want: "path endpoint requires value",
		},
		{
			name: "missing slug value",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{manifestBlobA: {}},
				Shares: []Share{
					{ID: "share-1", Blob: manifestBlobA, Slug: &SlugEndpoint{State: EndpointOK}},
				},
				ReservedSlugs: []string{},
			},
			want: "slug endpoint requires value",
		},
		{
			name: "invalid slug state",
			m: &Manifest{
				Version: ManifestVersion,
				Blobs:   map[string]Blob{manifestBlobA: {}},
				Shares: []Share{
					{ID: "share-1", Blob: manifestBlobA, Slug: &SlugEndpoint{Value: "f76", State: "broken"}},
				},
				ReservedSlugs: []string{},
			},
			want: "invalid slug state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.m.Validate()
			if err == nil {
				t.Fatalf("Validate succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestMarshalManifestRejectsInvalidManifest(t *testing.T) {
	_, err := MarshalManifest(&Manifest{Version: 2, Blobs: map[string]Blob{}, Shares: []Share{}, ReservedSlugs: []string{}})
	if err == nil || !strings.Contains(err.Error(), "unsupported manifest version") {
		t.Fatalf("MarshalManifest error = %v, want version error", err)
	}
}

func TestNilManifestBlobRefCountsAndIndexes(t *testing.T) {
	if got := (*Manifest)(nil).BlobRefCounts(); len(got) != 0 {
		t.Fatalf("BlobRefCounts on nil = %#v, want empty map", got)
	}
	indexes := BuildIndexes(nil)
	if len(indexes.ByPath) != 0 || len(indexes.BySlug) != 0 {
		t.Fatalf("BuildIndexes(nil) = %#v, want empty indexes", indexes)
	}
	if share, ok := (*Manifest)(nil).FindShareBySlug("f76"); ok || share != nil {
		t.Fatalf("FindShareBySlug on nil = %#v, %v, want nil false", share, ok)
	}
}

func TestOrphanedBlobsNilAndEmpty(t *testing.T) {
	if got := (*Manifest)(nil).OrphanedBlobs(); got != nil {
		t.Fatalf("OrphanedBlobs on nil = %#v, want nil", got)
	}
	manifest := &Manifest{
		Version:       ManifestVersion,
		Blobs:         map[string]Blob{},
		Shares:        []Share{},
		ReservedSlugs: []string{},
	}
	if got := manifest.OrphanedBlobs(); got != nil {
		t.Fatalf("OrphanedBlobs on empty blobs = %#v, want nil", got)
	}
}
