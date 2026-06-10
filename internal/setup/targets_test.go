package setup

import (
	"testing"

	"github.com/norlinga/mason/internal/config"
)

func twoTargetConfig() *config.Config {
	return &config.Config{
		Default: "home",
		Targets: map[string]config.Target{
			"home": {Host: "h.example.com", User: "mason", Root: "/srv/share", BaseURL: "https://h.example.com"},
			"work": {Host: "w.example.com", User: "mason", Root: "/srv/share", BaseURL: "https://w.example.com"},
		},
	}
}

func TestListTargetsSortedWithDefault(t *testing.T) {
	got := ListTargets(twoTargetConfig())
	if len(got) != 2 || got[0].Name != "home" || got[1].Name != "work" {
		t.Fatalf("list = %+v, want sorted home, work", got)
	}
	if !got[0].IsDefault || got[1].IsDefault {
		t.Fatalf("default marking wrong: %+v", got)
	}
}

func TestAddTarget(t *testing.T) {
	cfg := twoTargetConfig()
	tgt := config.Target{Host: "p", User: "mason", Root: "/srv/share", BaseURL: "https://p"}
	if err := AddTarget(cfg, "prod", tgt, false); err != nil {
		t.Fatalf("AddTarget: %v", err)
	}
	if _, ok := cfg.Targets["prod"]; !ok {
		t.Fatal("prod not added")
	}
	if cfg.Default != "home" {
		t.Fatalf("default changed unexpectedly: %q", cfg.Default)
	}
}

func TestAddTargetMakeDefault(t *testing.T) {
	cfg := twoTargetConfig()
	tgt := config.Target{Host: "p", User: "mason", Root: "/srv/share", BaseURL: "https://p"}
	if err := AddTarget(cfg, "prod", tgt, true); err != nil {
		t.Fatalf("AddTarget: %v", err)
	}
	if cfg.Default != "prod" {
		t.Fatalf("default = %q, want prod", cfg.Default)
	}
}

func TestAddTargetDuplicate(t *testing.T) {
	cfg := twoTargetConfig()
	if err := AddTarget(cfg, "home", config.Target{}, false); err == nil {
		t.Fatal("AddTarget duplicate err = nil, want error")
	}
}

func TestAddTargetInvalidValidates(t *testing.T) {
	cfg := twoTargetConfig()
	// Missing host/root/base_url should fail validation.
	if err := AddTarget(cfg, "bad", config.Target{User: "mason"}, false); err == nil {
		t.Fatal("AddTarget invalid err = nil, want validation error")
	}
}

func TestUseTarget(t *testing.T) {
	cfg := twoTargetConfig()
	if err := UseTarget(cfg, "work"); err != nil {
		t.Fatalf("UseTarget: %v", err)
	}
	if cfg.Default != "work" {
		t.Fatalf("default = %q, want work", cfg.Default)
	}
	if err := UseTarget(cfg, "nope"); err == nil {
		t.Fatal("UseTarget unknown err = nil, want error")
	}
}

func TestRemoveTargetReassignsDefault(t *testing.T) {
	cfg := twoTargetConfig() // default home
	newDefault, err := RemoveTarget(cfg, "home")
	if err != nil {
		t.Fatalf("RemoveTarget: %v", err)
	}
	if _, ok := cfg.Targets["home"]; ok {
		t.Fatal("home not removed")
	}
	if newDefault != "work" || cfg.Default != "work" {
		t.Fatalf("default = %q (returned %q), want work", cfg.Default, newDefault)
	}
}

func TestRemoveTargetNonDefaultKeepsDefault(t *testing.T) {
	cfg := twoTargetConfig()
	newDefault, err := RemoveTarget(cfg, "work")
	if err != nil {
		t.Fatalf("RemoveTarget: %v", err)
	}
	if newDefault != "home" {
		t.Fatalf("default = %q, want home unchanged", newDefault)
	}
}

func TestRemoveTargetUnknown(t *testing.T) {
	cfg := twoTargetConfig()
	if _, err := RemoveTarget(cfg, "nope"); err == nil {
		t.Fatal("RemoveTarget unknown err = nil, want error")
	}
}

func TestRemoveOnlyTargetRefused(t *testing.T) {
	cfg := &config.Config{
		Default: "home",
		Targets: map[string]config.Target{
			"home": {Host: "h", User: "mason", Root: "/srv/share", BaseURL: "https://h"},
		},
	}
	if _, err := RemoveTarget(cfg, "home"); err == nil {
		t.Fatal("removing only target err = nil, want refusal")
	}
}
