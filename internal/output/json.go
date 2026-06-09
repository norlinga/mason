package output

import (
	"encoding/json"
	"io"
)

// Envelope is the standard JSON response wrapper for CLI output.
type Envelope struct {
	OK    bool           `json:"ok"`
	Data  any            `json:"data,omitempty"`
	Error *Error         `json:"error,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

// WriteJSON writes a stable, newline-terminated JSON envelope.
func WriteJSON(w io.Writer, envelope Envelope) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
}

// Success wraps data in a successful JSON envelope.
func Success(data any) Envelope {
	return Envelope{
		OK:   true,
		Data: data,
	}
}

// Failure wraps an error in a failed JSON envelope.
func Failure(err *Error) Envelope {
	return Envelope{
		OK:    false,
		Error: err,
	}
}
