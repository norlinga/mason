package output

import (
	"errors"
	"testing"
)

func TestErrorFormattingAndUnwrap(t *testing.T) {
	cause := errors.New("open config")
	err := WithHint(NewError(CodeConfig, "could not load config", cause), "run mason setup")

	if got, want := err.Error(), "could not load config (run mason setup)"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("wrapped error was not preserved")
	}
}

func TestOutputOptions(t *testing.T) {
	tests := []struct {
		name       string
		options    Options
		wantDetail bool
		wantDebug  bool
	}{
		{name: "default", wantDetail: true},
		{name: "quiet", options: Options{Quiet: true}, wantDetail: false},
		{name: "verbose", options: Options{Verbose: true}, wantDetail: true, wantDebug: true},
		{name: "quiet wins", options: Options{Quiet: true, Verbose: true}, wantDetail: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.options.DetailAllowed(); got != tt.wantDetail {
				t.Fatalf("DetailAllowed() = %v, want %v", got, tt.wantDetail)
			}
			if got := tt.options.DebugAllowed(); got != tt.wantDebug {
				t.Fatalf("DebugAllowed() = %v, want %v", got, tt.wantDebug)
			}
		})
	}
}
