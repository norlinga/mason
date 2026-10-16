package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteUserConfigFreshAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mason")
	res, err := WriteUserConfig(path, UserConfigValues{Server: "example.com", Key: "~/.ssh/mason"})
	if err != nil || !res.Changed {
		t.Fatalf("WriteUserConfig = %+v, %v", res, err)
	}
	if got := readFile(t, path); got != "server: example.com\nkey: ~/.ssh/mason\n" {
		t.Fatalf("file = %q", got)
	}
	res, err = WriteUserConfig(path, UserConfigValues{Server: "example.com"})
	if err != nil || res.Changed {
		t.Fatalf("unchanged write = %+v, %v", res, err)
	}
}

func TestWriteUserConfigPreservesCommentsAndInbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mason")
	writeExec(t, path, "# my laptop\nserver: old.example.com # was here\nshare:\n  url: https://old.example.com\n  inbox: inbox/\n")
	if _, err := WriteUserConfig(path, UserConfigValues{Server: "example.com", ShareURL: "https://files.example.com"}); err != nil {
		t.Fatalf("WriteUserConfig: %v", err)
	}
	want := "# my laptop\nserver: example.com # was here\nshare:\n  url: https://files.example.com\n  inbox: inbox/\n"
	if got := readFile(t, path); got != want {
		t.Fatalf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestWriteUserConfigBacksUpV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mason")
	v1 := "default: static\ntargets:\n  static:\n    host: example.com\n"
	writeExec(t, path, v1)
	res, err := WriteUserConfig(path, UserConfigValues{Server: "example.com"})
	if err != nil || res.Backup != path+".v1.bak" {
		t.Fatalf("WriteUserConfig = %+v, %v", res, err)
	}
	if readFile(t, res.Backup) != v1 || readFile(t, path) != "server: example.com\n" {
		t.Fatal("v1 not backed up or v2 not written")
	}
}

func TestWriteUserConfigRefusesInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".mason")
	writeExec(t, path, "server: example.com\n")
	if _, err := WriteUserConfig(path, UserConfigValues{ShareURL: "https://x.example.com/not-root"}); err == nil {
		t.Fatal("invalid share URL accepted")
	}
	if readFile(t, path) != "server: example.com\n" {
		t.Fatal("original modified")
	}
	if _, err := os.Stat(path + ".mason-new"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}
