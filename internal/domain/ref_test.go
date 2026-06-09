package domain

import (
	"strings"
	"testing"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Ref
		wantErr string
	}{
		{
			name:  "object path",
			input: "super/rad/podcasts/foo.mp3",
			want:  Ref{Kind: RefKindPath, Value: "super/rad/podcasts/foo.mp3"},
		},
		{
			name:  "object path single segment",
			input: "foo.mp3",
			want:  Ref{Kind: RefKindPath, Value: "foo.mp3"},
		},
		{
			name:  "object path with leading segment s but no prefix",
			input: "somedir/foo.mp3",
			want:  Ref{Kind: RefKindPath, Value: "somedir/foo.mp3"},
		},
		{
			name:  "slug ref",
			input: "s/f76",
			want:  Ref{Kind: RefKindSlug, Value: "f76"},
		},
		{
			name:  "slug ref trims surrounding whitespace",
			input: "  s/f76  ",
			want:  Ref{Kind: RefKindSlug, Value: "f76"},
		},
		{
			name:  "object path trims surrounding whitespace",
			input: "  inbox/foo.mp3  ",
			want:  Ref{Kind: RefKindPath, Value: "inbox/foo.mp3"},
		},
		{
			name:    "empty ref",
			input:   "",
			wantErr: "ref is required",
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: "ref is required",
		},
		{
			name:    "slug prefix only",
			input:   "s/",
			wantErr: "slug ref requires value",
		},
		{
			name:    "slug prefix with whitespace only",
			input:   "s/   ",
			wantErr: "slug ref requires value",
		},
		{
			name:  "uppercase slug prefix is object path",
			input: "S/f76",
			want:  Ref{Kind: RefKindPath, Value: "S/f76"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRef(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseRef(%q) err = nil, want error containing %q", tt.input, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseRef(%q) err = %q, want containing %q", tt.input, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRef(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("ParseRef(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRefKindHelpers(t *testing.T) {
	pathRef := Ref{Kind: RefKindPath, Value: "foo.mp3"}
	if !pathRef.IsPath() || pathRef.IsSlug() {
		t.Fatalf("path ref helpers: IsPath=%v IsSlug=%v, want true false", pathRef.IsPath(), pathRef.IsSlug())
	}

	slugRef := Ref{Kind: RefKindSlug, Value: "f76"}
	if slugRef.IsPath() || !slugRef.IsSlug() {
		t.Fatalf("slug ref helpers: IsPath=%v IsSlug=%v, want false true", slugRef.IsPath(), slugRef.IsSlug())
	}
}
