package output

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestWriteJSONSuccessEnvelope(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSON(&buf, Success(map[string]string{"path": "demo.mp3"}))
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var got struct {
		OK   bool              `json:"ok"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	if !got.OK {
		t.Fatalf("OK = false, want true")
	}
	if got.Data["path"] != "demo.mp3" {
		t.Fatalf("data[path] = %q, want demo.mp3", got.Data["path"])
	}
}

func TestWriteJSONFailureEnvelope(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSON(&buf, Failure(NewError(CodeUsage, "bad args", nil)))
	if err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var got struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	if got.OK {
		t.Fatalf("OK = true, want false")
	}
	if got.Error.Code != string(CodeUsage) {
		t.Fatalf("error.code = %q, want %q", got.Error.Code, CodeUsage)
	}
	if got.Error.Message != "bad args" {
		t.Fatalf("error.message = %q, want bad args", got.Error.Message)
	}
}
