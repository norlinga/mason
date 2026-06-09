package domain

import "testing"

func TestDecideShare(t *testing.T) {
	const objectPath = "demos/test.mp3"

	// shareAt builds a manifest with one share occupying objectPath, pointing at blob.
	shareAt := func(path, blob string, knownBlobs ...string) *Manifest {
		m := NewManifest()
		for _, b := range knownBlobs {
			m.Blobs[b] = Blob{}
		}
		m.Shares = append(m.Shares, Share{
			ID:   "share-1",
			Blob: blob,
			Path: &Endpoint{Value: path, State: EndpointOK},
			Slug: &SlugEndpoint{Value: "aaa111", Ext: ".mp3", State: EndpointOK},
		})
		return m
	}

	tests := []struct {
		name       string
		manifest   *Manifest
		blob       string
		wantAction ShareAction
		wantUpload bool
		wantOld    string
	}{
		{
			name:       "new object path; blob exists",
			manifest:   &Manifest{Version: ManifestVersion, Blobs: map[string]Blob{manifestBlobA: {}}, Shares: []Share{}, ReservedSlugs: []string{}},
			blob:       manifestBlobA,
			wantAction: ShareCreate,
			wantUpload: false,
		},
		{
			name:       "new object path; blob missing",
			manifest:   NewManifest(),
			blob:       manifestBlobA,
			wantAction: ShareCreate,
			wantUpload: true,
		},
		{
			name:       "object path exists; same hash",
			manifest:   shareAt(objectPath, manifestBlobA, manifestBlobA),
			blob:       manifestBlobA,
			wantAction: ShareNoop,
			wantUpload: false,
		},
		{
			name:       "object path exists; different hash, new blob present",
			manifest:   shareAt(objectPath, manifestBlobA, manifestBlobA, manifestBlobB),
			blob:       manifestBlobB,
			wantAction: ShareRepoint,
			wantUpload: false,
			wantOld:    manifestBlobA,
		},
		{
			name:       "object path exists; different hash, new blob missing",
			manifest:   shareAt(objectPath, manifestBlobA, manifestBlobA),
			blob:       manifestBlobB,
			wantAction: ShareRepoint,
			wantUpload: true,
			wantOld:    manifestBlobA,
		},
		{
			name:       "same bytes; new object path reuses blob",
			manifest:   shareAt("other/path.mp3", manifestBlobA, manifestBlobA),
			blob:       manifestBlobA,
			wantAction: ShareCreate,
			wantUpload: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.manifest.DecideShare(objectPath, tt.blob)
			if got.Action != tt.wantAction {
				t.Fatalf("Action = %s, want %s", got.Action, tt.wantAction)
			}
			if got.UploadBlob != tt.wantUpload {
				t.Fatalf("UploadBlob = %v, want %v", got.UploadBlob, tt.wantUpload)
			}
			if got.OldBlob != tt.wantOld {
				t.Fatalf("OldBlob = %q, want %q", got.OldBlob, tt.wantOld)
			}
		})
	}
}

func TestShareActionString(t *testing.T) {
	tests := []struct {
		action ShareAction
		want   string
	}{
		{ShareNoop, "noop"},
		{ShareCreate, "create"},
		{ShareRepoint, "repoint"},
		{ShareAction(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.action.String(); got != tt.want {
			t.Fatalf("ShareAction(%d).String() = %q, want %q", tt.action, got, tt.want)
		}
	}
}
