package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/norlinga/mason/internal/domain"
	"github.com/norlinga/mason/internal/links"
)

func TestDisableCommandPlainChanged(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "disable" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newDisableCommand(&stdout, &stderr, func(context.Context, rootOptions, string) (links.EndpointResult, error) {
		return links.EndpointResult{Endpoint: "slug", State: domain.EndpointDisabled, Changed: true}, nil
	}))
	root.SetArgs([]string{"disable", "s/abc123"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout.String() != "Disabled s/abc123\n" {
		t.Fatalf("plain output = %q", stdout.String())
	}
}

func TestDisableCommandPlainNoop(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "disable" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newDisableCommand(&stdout, &stderr, func(context.Context, rootOptions, string) (links.EndpointResult, error) {
		return links.EndpointResult{Endpoint: "slug", State: domain.EndpointDisabled, Changed: false}, nil
	}))
	root.SetArgs([]string{"disable", "s/abc123"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout.String() != "s/abc123 is already disabled.\n" {
		t.Fatalf("plain noop output = %q", stdout.String())
	}
}

func TestEnableCommandPlainChanged(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "enable" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newEnableCommand(&stdout, &stderr, func(context.Context, rootOptions, string) (links.EndpointResult, error) {
		return links.EndpointResult{Endpoint: "slug", State: domain.EndpointOK, Changed: true}, nil
	}))
	root.SetArgs([]string{"enable", "s/abc123"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stdout.String() != "Enabled s/abc123\n" {
		t.Fatalf("plain output = %q", stdout.String())
	}
}

func TestDisableCommandJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "disable" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newDisableCommand(&stdout, &stderr, func(context.Context, rootOptions, string) (links.EndpointResult, error) {
		return links.EndpointResult{Endpoint: "path", State: domain.EndpointDisabled, Changed: true}, nil
	}))
	root.SetArgs([]string{"--json", "disable", "inbox/foo.mp3"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "ref": "inbox/foo.mp3",
    "endpoint": "path",
    "state": "disabled",
    "changed": true
  }
}
`
	if stdout.String() != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout.String(), want)
	}
}

func TestEnableCommandJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "enable" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newEnableCommand(&stdout, &stderr, func(context.Context, rootOptions, string) (links.EndpointResult, error) {
		return links.EndpointResult{Endpoint: "slug", State: domain.EndpointOK, Changed: true}, nil
	}))
	root.SetArgs([]string{"--json", "enable", "s/abc123"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := `{
  "ok": true,
  "data": {
    "ref": "s/abc123",
    "endpoint": "slug",
    "state": "ok",
    "changed": true
  }
}
`
	if stdout.String() != want {
		t.Fatalf("json output:\n got: %q\nwant: %q", stdout.String(), want)
	}
}

func TestEnableCommandError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	for _, c := range root.Commands() {
		if c.Name() == "enable" {
			root.RemoveCommand(c)
		}
	}
	root.AddCommand(newEnableCommand(&stdout, &stderr, func(context.Context, rootOptions, string) (links.EndpointResult, error) {
		return links.EndpointResult{}, errors.New("no share found for \"nope.mp3\"")
	}))
	root.SetArgs([]string{"enable", "nope.mp3"})
	if err := root.Execute(); err == nil {
		t.Fatal("execute err = nil, want error")
	}
	if !strings.Contains(stderr.String(), "no share found") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDisableCommandRequiresRef(t *testing.T) {
	var stdout, stderr bytes.Buffer
	root := NewRootCommand(&stdout, &stderr)
	root.SetArgs([]string{"disable"})
	if err := root.Execute(); err == nil {
		t.Fatal("execute err = nil, want usage error")
	}
}
