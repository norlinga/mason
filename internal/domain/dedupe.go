package domain

// ShareAction is the high-level operation `mason share` must perform for a file.
type ShareAction int

const (
	// ShareNoop means the object path already points at this exact blob.
	ShareNoop ShareAction = iota
	// ShareCreate means a new share (object-path link + short slug) is needed.
	ShareCreate
	// ShareRepoint means the object path exists but must point at a different blob.
	ShareRepoint
)

// String renders the action for diagnostics and test output.
func (a ShareAction) String() string {
	switch a {
	case ShareNoop:
		return "noop"
	case ShareCreate:
		return "create"
	case ShareRepoint:
		return "repoint"
	default:
		return "unknown"
	}
}

// ShareDecision is the outcome of the dedupe/upload decision.
type ShareDecision struct {
	Action ShareAction
	// UploadBlob reports whether the blob bytes must be transferred before linking.
	UploadBlob bool
	// OldBlob is the previous blob at the object path on a repoint; it becomes
	// GC-eligible once no manifest endpoint references it.
	OldBlob string
}

// DecideShare determines what `mason share` must do to publish blob at objectPath
// against the current manifest. It performs no I/O; blob presence is read from the
// manifest's blob table, which is authoritative for what already exists remotely.
func (m *Manifest) DecideShare(objectPath, blob string) ShareDecision {
	blobPresent := false
	if m != nil {
		_, blobPresent = m.Blobs[blob]
	}

	existing, found := m.FindShareByPath(objectPath)
	if !found {
		// New object path: link only when the blob already exists, else upload first.
		return ShareDecision{Action: ShareCreate, UploadBlob: !blobPresent}
	}
	if existing.Blob == blob {
		// Object path already points at these exact bytes.
		return ShareDecision{Action: ShareNoop}
	}
	// Object path exists but content changed: re-point, uploading if the new blob is absent.
	return ShareDecision{Action: ShareRepoint, UploadBlob: !blobPresent, OldBlob: existing.Blob}
}
