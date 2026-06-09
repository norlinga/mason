package output

import "testing"

func TestHumanSize(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{5, "5 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{4412345, "4.2 MB"},
		{8493465, "8.1 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		if got := HumanSize(tt.n); got != tt.want {
			t.Fatalf("HumanSize(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
