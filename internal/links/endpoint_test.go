package links

import (
	"context"
	"errors"
	"testing"

	"github.com/norlinga/mason/internal/domain"
)

func TestDisableSlugRemovesBothLinks(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	fs.symlinks["/srv/share/s/zzz999.mp3"] = "../" + blobAPath
	fs.symlinks["/srv/share/s/zzz999"] = "../" + blobAPath
	svc := newService(t, fs, store)

	res, err := svc.Disable(context.Background(), EndpointRequest{Ref: "s/zzz999"})
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if res.Endpoint != "slug" || res.State != domain.EndpointDisabled || !res.Changed {
		t.Fatalf("result = %#v", res)
	}
	for _, link := range []string{"/srv/share/s/zzz999.mp3", "/srv/share/s/zzz999"} {
		if _, ok := fs.symlinks[link]; ok {
			t.Fatalf("slug symlink %q not removed on disable", link)
		}
	}
	if store.manifest.Shares[0].Slug.State != domain.EndpointDisabled {
		t.Fatal("slug state not disabled in manifest")
	}
	// Path endpoint untouched.
	if store.manifest.Shares[0].Path.State != domain.EndpointOK {
		t.Fatal("path endpoint state changed by slug disable")
	}
	// Slug stays reserved.
	if !store.manifest.SlugReserved("zzz999") {
		t.Fatal("slug reservation dropped on disable")
	}
}

func TestDisablePathRemovesObjectLink(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	if _, err := svc.Disable(context.Background(), EndpointRequest{Ref: "inbox/foo.mp3"}); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, ok := fs.symlinks["/srv/share/inbox/foo.mp3"]; ok {
		t.Fatal("object symlink not removed on disable")
	}
	if store.manifest.Shares[0].Path.State != domain.EndpointDisabled {
		t.Fatal("path state not disabled")
	}
}

func TestDisableAlreadyDisabledIsNoop(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointDisabled, domain.EndpointOK))
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.Disable(context.Background(), EndpointRequest{Ref: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if res.Changed {
		t.Fatal("Changed = true, want false for already-disabled")
	}
	if store.puts != 0 || len(fs.removes) != 0 {
		t.Fatal("no-op disable mutated remote state")
	}
}

func TestEnableRecreatesLinks(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointDisabled))
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.Enable(context.Background(), EndpointRequest{Ref: "s/zzz999"})
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if res.Endpoint != "slug" || res.State != domain.EndpointOK || !res.Changed {
		t.Fatalf("result = %#v", res)
	}
	if got := fs.symlinks["/srv/share/s/zzz999.mp3"]; got != "../"+blobAPath {
		t.Fatalf("canonical slug link = %q, want recreated", got)
	}
	if got := fs.symlinks["/srv/share/s/zzz999"]; got != "../"+blobAPath {
		t.Fatalf("bare slug link = %q, want recreated", got)
	}
	if store.manifest.Shares[0].Slug.State != domain.EndpointOK {
		t.Fatal("slug state not enabled in manifest")
	}
}

func TestEnableAlreadyEnabledIsNoop(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	svc := newService(t, fs, store)

	res, err := svc.Enable(context.Background(), EndpointRequest{Ref: "inbox/foo.mp3"})
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if res.Changed {
		t.Fatal("Changed = true, want false for already-enabled")
	}
	if store.puts != 0 || len(fs.symlinks) != 0 {
		t.Fatal("no-op enable mutated remote state")
	}
}

func TestDisableEnableRoundTrip(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	if _, err := svc.Disable(context.Background(), EndpointRequest{Ref: "inbox/foo.mp3"}); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, ok := fs.symlinks["/srv/share/inbox/foo.mp3"]; ok {
		t.Fatal("symlink present after disable")
	}
	if _, err := svc.Enable(context.Background(), EndpointRequest{Ref: "inbox/foo.mp3"}); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if got := fs.symlinks["/srv/share/inbox/foo.mp3"]; got != "../"+blobAPath {
		t.Fatalf("symlink = %q after enable, want restored", got)
	}
	if store.manifest.Shares[0].Path.State != domain.EndpointOK {
		t.Fatal("state not ok after round trip")
	}
}

func TestDisablePutErrorRestoresState(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointOK, domain.EndpointOK))
	store.putErr = errors.New("write failed")
	fs := newFakeFS()
	fs.symlinks["/srv/share/inbox/foo.mp3"] = "../" + blobAPath
	svc := newService(t, fs, store)

	if _, err := svc.Disable(context.Background(), EndpointRequest{Ref: "inbox/foo.mp3"}); err == nil {
		t.Fatal("Disable err = nil on put failure, want error")
	}
	if store.manifest.Shares[0].Path.State != domain.EndpointOK {
		t.Fatal("state not restored after failed disable put")
	}
	if _, ok := fs.symlinks["/srv/share/inbox/foo.mp3"]; !ok {
		t.Fatal("symlink removed despite failed disable put")
	}
}

func TestEnablePutErrorRollsBackLinks(t *testing.T) {
	store := storeWith(shareFixture("existing", blobA, "inbox/foo.mp3", "zzz999", domain.EndpointDisabled, domain.EndpointOK))
	store.putErr = errors.New("write failed")
	fs := newFakeFS()
	svc := newService(t, fs, store)

	if _, err := svc.Enable(context.Background(), EndpointRequest{Ref: "inbox/foo.mp3"}); err == nil {
		t.Fatal("Enable err = nil on put failure, want error")
	}
	if store.manifest.Shares[0].Path.State != domain.EndpointDisabled {
		t.Fatal("state not restored to disabled after failed enable put")
	}
	if _, ok := fs.symlinks["/srv/share/inbox/foo.mp3"]; ok {
		t.Fatal("created symlink not rolled back after failed enable put")
	}
}

func TestDisableUnknownRef(t *testing.T) {
	store := storeWith()
	svc := newService(t, newFakeFS(), store)
	if _, err := svc.Disable(context.Background(), EndpointRequest{Ref: "nope.mp3"}); err == nil {
		t.Fatal("Disable err = nil, want not-found error")
	}
}
