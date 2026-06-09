package domain

import (
	"errors"
	"strings"
	"testing"
)

const (
	emptyKey = "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	helloKey = "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
)

func TestHashBytes(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{name: "empty", input: []byte{}, want: emptyKey},
		{name: "nil", input: nil, want: emptyKey},
		{name: "hello", input: []byte("hello"), want: helloKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HashBytes(tt.input); got != tt.want {
				t.Fatalf("HashBytes(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHashReader(t *testing.T) {
	got, err := HashReader(strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("HashReader: %v", err)
	}
	if got != helloKey {
		t.Fatalf("HashReader = %q, want %q", got, helloKey)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestHashReaderError(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := HashReader(errReader{err: sentinel})
	if err == nil {
		t.Fatal("HashReader err = nil, want error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("HashReader err = %v, want wrapping %v", err, sentinel)
	}
}

func TestBlobHex(t *testing.T) {
	const digest = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "valid", input: helloKey, want: digest},
		{
			name:  "uppercase digest normalized",
			input: "sha256:" + strings.ToUpper(digest),
			want:  digest,
		},
		{
			name:    "missing prefix",
			input:   digest,
			wantErr: "missing",
		},
		{
			name:    "wrong length",
			input:   "sha256:abcd",
			wantErr: "invalid digest length",
		},
		{
			name:    "non-hex digest",
			input:   "sha256:zzf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
			wantErr: "non-hex digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BlobHex(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("BlobHex(%q) err = nil, want containing %q", tt.input, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("BlobHex(%q) err = %q, want containing %q", tt.input, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BlobHex(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("BlobHex(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBlobPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{
			name:  "hello blob",
			input: helloKey,
			want:  "objects/2c/f2/2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name:  "empty blob",
			input: emptyKey,
			want:  "objects/e3/b0/e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:    "invalid key",
			input:   "not-a-blob",
			wantErr: "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BlobPath(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("BlobPath(%q) err = nil, want containing %q", tt.input, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("BlobPath(%q) err = %q, want containing %q", tt.input, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BlobPath(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("BlobPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
