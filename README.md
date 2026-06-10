# Mason

A local Go CLI for publishing to a personal VPS behind Caddy: **share** files
(content-addressed blobs + short links) and **deploy** static sites (rsync). No
daemon runs on the server — the remote is just a filesystem contract plus Caddy.

## Requirements

- **Local:** Go (to build), `ssh`, `rsync`.
- **Remote:** `ssh` access, `rsync`, `caddy`. Bootstrapped via `mason remote-init`.

## Build

```sh
go build -o mason ./cmd/mason   # or: go install ./cmd/mason
```

## Bootstrap

### 1. Local onboarding

Generates an SSH key if needed, writes `~/.mason`, and tests the connection.

```sh
mason setup \
  --host share.example.com \
  --user mason \
  --root /srv/share \
  --base-url https://share.example.com
```

Run without flags for an interactive prompt. Use a dedicated unprivileged
`mason` user on the server (the default) — not root or your personal login.

### 2. Remote bootstrap

`remote-init` prints an idempotent script: it creates the `mason` user, installs
your deploy key, lays out the share + deploy trees, grants Caddy read access, and
adds a Mason-owned Caddy snippet (with `/objects/*` and `/.mason/*` returning 404).

```sh
# Review the script, then pipe it to an admin login:
mason remote-init | ssh root@share.example.com sh

# Or let mason run it over SSH as an admin user:
mason remote-init --apply --as-admin root
```

Re-running is safe; it converges to the same state.

### 3. Verify

```sh
mason doctor   # SSH, perms, manifest, private-prefix 404s, Caddy reconciliation
```

## Sharing files

```sh
mason share ./talk.mp3                          # → inbox/talk.mp3 (default path)
mason share ./talk.mp3 --as podcasts/ep-01.mp3  # exact object path
mason ls [prefix]                               # list shares
mason info podcasts/ep-01.mp3                    # full detail for one share
mason url s/f76 [--long]                         # print a URL (short by default)
```

A share is a **link**, not a blob. Re-sharing identical bytes to a new path links
only (no re-upload). Each share has two independent endpoints — its object path
and a short slug (`s/<slug>`) — addressable separately.

### Managing shares

```sh
mason mv podcasts/ep-01.mp3 archive/ep-01.mp3   # re-home the object path
mason cp podcasts/ep-01.mp3 archive/ep-01.mp3   # duplicate (new path + new slug)
mason disable s/f76                              # 404 the endpoint (reversible)
mason enable s/f76
mason rm s/f76                                   # remove one endpoint
mason prune                                      # GC orphaned blobs (dry-run; -y to delete)
mason sync                                       # refresh local cache from remote
```

Refer to a share by object path or by slug as `s/<slug>`.

## Deploying a site

```sh
mason init                  # scaffold a project .mason
mason status               # show the deploy plan (no transfer)
mason deploy [target]      # rsync the build tree; plan → progress → summary
mason deploy --delete      # also remove remote files absent locally (confirmed)
```

## Targets

`~/.mason` (or a project `.mason` found by walking up from the cwd) holds named
targets; `default` selects one. Override per command with `-t <name>`.

```sh
mason target ls
mason target add work --host w.example.com --root /srv/share --base-url https://w.example.com
mason target use work
mason target rm work
```

## Global flags

`-t/--target`, `--json`, `-n/--dry-run`, `-y/--yes`, `-q/--quiet`, `-v/--verbose`,
`--config <path>`. Plain output is columnar/greppable; `--json` is the scripting
counterpart.
