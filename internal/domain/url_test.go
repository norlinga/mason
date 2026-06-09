package domain

import "testing"

func TestObjectURL(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		objectPath string
		want       string
	}{
		{
			name:       "simple",
			baseURL:    "https://share.example.com",
			objectPath: "super/rad/podcasts/episode-01.mp3",
			want:       "https://share.example.com/super/rad/podcasts/episode-01.mp3",
		},
		{
			name:       "trims base trailing slash",
			baseURL:    "https://share.example.com/",
			objectPath: "inbox/foo.mp3",
			want:       "https://share.example.com/inbox/foo.mp3",
		},
		{
			name:       "trims leading slash on path",
			baseURL:    "https://share.example.com",
			objectPath: "/inbox/foo.mp3",
			want:       "https://share.example.com/inbox/foo.mp3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ObjectURL(tt.baseURL, tt.objectPath); got != tt.want {
				t.Fatalf("ObjectURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSlugURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		slug    string
		ext     string
		want    string
	}{
		{
			name:    "with extension",
			baseURL: "https://share.example.com",
			slug:    "f76",
			ext:     ".mp3",
			want:    "https://share.example.com/s/f76.mp3",
		},
		{
			name:    "without extension",
			baseURL: "https://share.example.com",
			slug:    "f76",
			ext:     "",
			want:    "https://share.example.com/s/f76",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SlugURL(tt.baseURL, tt.slug, tt.ext); got != tt.want {
				t.Fatalf("SlugURL = %q, want %q", got, tt.want)
			}
		})
	}
}
