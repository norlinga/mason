package domain

import (
	"strings"
	"testing"
)

func TestNormalizeObjectPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{
			name:  "simple path",
			input: "super/rad/podcasts/foo.mp3",
			want:  "super/rad/podcasts/foo.mp3",
		},
		{
			name:  "strips leading slash",
			input: "/inbox/foo.mp3",
			want:  "inbox/foo.mp3",
		},
		{
			name:  "collapses repeated slashes",
			input: "inbox//foo.mp3",
			want:  "inbox/foo.mp3",
		},
		{
			name:  "converts backslashes",
			input: `inbox\foo.mp3`,
			want:  "inbox/foo.mp3",
		},
		{
			name:  "trims whitespace",
			input: "  inbox/foo.mp3  ",
			want:  "inbox/foo.mp3",
		},
		{
			name:  "drops dot segments",
			input: "inbox/./foo.mp3",
			want:  "inbox/foo.mp3",
		},
		{
			name:  "single filename",
			input: "foo.mp3",
			want:  "foo.mp3",
		},
		{
			name:    "empty path",
			input:   "",
			wantErr: "object path is required",
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: "object path is required",
		},
		{
			name:    "slash only",
			input:   "/",
			wantErr: "object path is required",
		},
		{
			name:    "rejects parent traversal",
			input:   "inbox/../secret/foo.mp3",
			wantErr: `cannot contain ".."`,
		},
		{
			name:    "rejects reserved slug namespace",
			input:   "s/foo.mp3",
			wantErr: `reserved prefix "s/"`,
		},
		{
			name:    "rejects reserved slug namespace after normalization",
			input:   "/s/foo.mp3",
			wantErr: `reserved prefix "s/"`,
		},
		{
			name:    "rejects reserved slug namespace exact segment",
			input:   "s",
			wantErr: `reserved prefix "s/"`,
		},
		{
			name:    "rejects reserved objects namespace",
			input:   "objects/ab/cd/blob",
			wantErr: `reserved prefix "objects/"`,
		},
		{
			name:    "rejects reserved mason namespace",
			input:   ".mason/manifest.json",
			wantErr: `reserved prefix ".mason/"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeObjectPath(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("NormalizeObjectPath(%q) err = nil, want error containing %q", tt.input, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NormalizeObjectPath(%q) err = %q, want containing %q", tt.input, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeObjectPath(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeObjectPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveSharePath(t *testing.T) {
	tests := []struct {
		name        string
		as          string
		defaultPath string
		filename    string
		want        string
		wantErr     string
	}{
		{
			name:        "no as uses default path bucket",
			defaultPath: "inbox/",
			filename:    "foo.mp3",
			want:        "inbox/foo.mp3",
		},
		{
			name:     "no as and no default uses filename",
			filename: "foo.mp3",
			want:     "foo.mp3",
		},
		{
			name:     "as directory keeps filename",
			as:       "super/rad/podcasts/",
			filename: "foo.mp3",
			want:     "super/rad/podcasts/foo.mp3",
		},
		{
			name:     "as exact path ignores filename",
			as:       "super/rad/podcasts/episode-01.mp3",
			filename: "foo.mp3",
			want:     "super/rad/podcasts/episode-01.mp3",
		},
		{
			name:     "as exact path can rename without extension",
			as:       "archive/episode-01",
			filename: "foo.mp3",
			want:     "archive/episode-01",
		},
		{
			name:        "as takes precedence over default path",
			as:          "demos/test.mp3",
			defaultPath: "inbox/",
			filename:    "foo.mp3",
			want:        "demos/test.mp3",
		},
		{
			name:     "as root directory",
			as:       "/",
			filename: "foo.mp3",
			want:     "foo.mp3",
		},
		{
			name:        "default path without trailing slash",
			defaultPath: "inbox",
			filename:    "foo.mp3",
			want:        "inbox/foo.mp3",
		},
		{
			name:     "normalizes as exact path",
			as:       " /demos/test.mp3 ",
			filename: "foo.mp3",
			want:     "demos/test.mp3",
		},
		{
			name:     "normalizes as directory path",
			as:       `demos\`,
			filename: "foo.mp3",
			want:     "demos/foo.mp3",
		},
		{
			name:    "filename required",
			as:      "inbox/",
			wantErr: "filename is required",
		},
		{
			name:     "rejects parent traversal in as exact path",
			as:       "inbox/../secret/foo.mp3",
			filename: "foo.mp3",
			wantErr:  `cannot contain ".."`,
		},
		{
			name:     "rejects parent traversal in as directory",
			as:       "inbox/../secret/",
			filename: "foo.mp3",
			wantErr:  `cannot contain ".."`,
		},
		{
			name:     "rejects slug namespace in as exact path",
			as:       "s/foo.mp3",
			filename: "foo.mp3",
			wantErr:  `reserved prefix "s/"`,
		},
		{
			name:     "rejects slug namespace in as directory",
			as:       "s/foo/",
			filename: "bar.mp3",
			wantErr:  `reserved prefix "s/"`,
		},
		{
			name:     "rejects objects namespace in as exact path",
			as:       "objects/ab/file",
			filename: "foo.mp3",
			wantErr:  `reserved prefix "objects/"`,
		},
		{
			name:        "rejects mason namespace in default path",
			defaultPath: ".mason/",
			filename:    "manifest.json",
			wantErr:     `reserved prefix ".mason/"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveSharePath(tt.as, tt.defaultPath, tt.filename)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ResolveSharePath(%q, %q, %q) err = nil, want error containing %q", tt.as, tt.defaultPath, tt.filename, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ResolveSharePath(%q, %q, %q) err = %q, want containing %q", tt.as, tt.defaultPath, tt.filename, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveSharePath(%q, %q, %q): %v", tt.as, tt.defaultPath, tt.filename, err)
			}
			if got != tt.want {
				t.Fatalf("ResolveSharePath(%q, %q, %q) = %q, want %q", tt.as, tt.defaultPath, tt.filename, got, tt.want)
			}
		})
	}
}
