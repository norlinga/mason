# Mason — Implementation Plan

This document synthesizes [PROJECT_DESCRIPTION.md](./PROJECT_DESCRIPTION.md),
[DESIGN.md](./DESIGN.md), [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md), and
[COMMANDS.md](./COMMANDS.md) into a single actionable plan. It is the **primary
guide for implementation** and the **source text for contextq queue context**.

**Precedence:** If older docs conflict with this plan, this plan wins for
implementation sequencing and unresolved technical choices locked below.

---

## Table of contents

1. [Philosophy](#philosophy)
2. [Architecture summary](#architecture-summary)
3. [Technical decisions locked by this plan](#technical-decisions-locked-by-this-plan)
4. [Command reference](#command-reference)
5. [Remote layout & data model](#remote-layout--data-model)
6. [Codebase structure & engineering standards](#codebase-structure--engineering-standards)
7. [Work plan](#work-plan)
8. [Success gates](#success-gates)
9. [contextq integration](#contextq-integration)

---

## Philosophy

### What Mason is

Mason is a **local Go CLI** that makes publishing to a personal VPS feel like a
native copy operation. It assembles blocks—config, transport, remote layout,
links—into something useful. It does not compete with S3/CDN tooling (MinIO `mc`,
etc.); it optimizes for **one person's static host behind Caddy**.

### Two workflows, one tool

| Workflow | User goal | Mason's job |
|----------|-----------|-------------|
| **Share** | Publish a file and manage links over time | Content-addressed blobs + symlink links + manifest CRUD |
| **Deploy** | Sync a build tree to the server | Observable rsync with plan, progress, and summary |

They share **config, targets, and transport** (SSH/rsync). They do **not** share
remote storage mechanics. Never route deploy through the share object store.

### The CLI organizing principle

**Local-touching vs remote-link commands.**

| Kind | Commands | Touches local disk? | Transfers bytes? |
|------|----------|:-------------------:|:----------------:|
| Local-touching | `share`, `deploy` | yes | yes |
| Remote-link | `ls`, `info`, `url`, `mv`, `cp`, `rm`, `disable`, `enable`, `prune`, `sync` | no | no |

Heavy commands run occasionally. Remote-link commands are cheap and frequent.
Design every feature by asking which bucket it belongs in.

### UX principles (non-negotiable)

1. **Unix composability** — Plain output is columnar and greppable. `--json` is a
   first-class parallel format for scripting (`mason ls --json | jq`).
2. **Two-phase share upload** — Ensure blob exists, then ensure link exists.
   Re-sharing identical bytes to a new path is normal and must not re-upload.
3. **Curated namespace** — Object paths are **user-assigned** (`--as` or default
   bucket). They are not derived from the local filesystem layout.
4. **A share is a link, not a blob** — Identity is `(object path, content hash)`.
   Same bytes may appear at many paths as independent shares. Each share exposes
   **two independently controllable endpoints** — its object path and its short
   slug — that `disable`/`enable`/`rm` can act on separately. Blob reference
   counts come from manifest endpoints, not raw symlink counts.
5. **No URL stability guarantee** — Users can `disable`, `rm`, or rotate links
   after hotlink abuse. Slugs are **reserved** and never auto-reused for different
   content.
6. **Deploy observability** — Pre-flight plan, live progress, post-run summary.
   Mirror-delete is opt-in (`--delete`) and confirmed.
7. **Explicit garbage collection** — `prune` is dry-run by default; no silent GC.
8. **Convenience-first secrets** — SSH keys are the happy path; setup tooling
   should make first-run painless.

### Shape A

No Mason daemon on the server. The remote is a **filesystem contract** (layout +
Caddy + SSH). All orchestration runs locally. A future `masond` may sit behind
the same interfaces but is out of scope until proven necessary.

---

## Architecture summary

```
┌──────────────────────────────────────────────────────────────┐
│  mason CLI (local)                                           │
│  ┌─────────┐  ┌─────────┐  ┌──────────┐  ┌───────────────┐  │
│  │  cli/   │→ │ share/  │  │ deploy/  │  │ setup/        │  │
│  │ cobra   │  │orch.    │  │ rsync    │  │ onboarding    │  │
│  └────┬────┘  └────┬────┘  └────┬─────┘  └───────┬───────┘  │
│       │            │            │                 │          │
│       └────────────┴────────────┴─────────────────┘          │
│                         │                                    │
│              ┌──────────┴──────────┐                         │
│              │ config/  domain/    │                         │
│              │ output/  remote/    │                         │
│              └──────────┬──────────┘                         │
└─────────────────────────┼────────────────────────────────────┘
                          │ SSH + rsync
                          ▼
┌──────────────────────────────────────────────────────────────┐
│  VPS — no Mason binary                                       │
│  /srv/share/  (Caddy site root)                              │
│    objects/   content-addressed blobs   (hidden from web)    │
│    s/         short slug symlinks (name carries extension)   │
│    <paths>/   curated object-path symlinks                   │
│    .mason/    manifest.json (authoritative; hidden from web) │
└──────────────────────────────────────────────────────────────┘
                          │
                          ▼
                    Caddy file_server
```

**Local sidecar:** `~/.cache/mason/` (or XDG cache) mirrors the remote manifest
for fast `ls`/`info` and offline inspection. Remote manifest wins on conflict.

---

## Technical decisions locked by this plan

These resolve open questions from earlier docs:

| Topic | Decision | Rationale |
|-------|----------|-----------|
| Config format | **YAML** (`.mason`, `~/.mason`) | Readable; `gopkg.in/yaml.v3` is standard |
| Manifest format | **Single JSON document** at `{root}/.mason/manifest.json` | Simple RMW for single-user; one file to fetch/patch |
| Local cache | JSON mirror at `{cache_dir}/{target-id}/manifest.json` | Same schema as remote |
| Content hash | **SHA-256**, hex encoded | Standard, collision-resistant |
| Blob path | `objects/{hash[0:2]}/{hash[2:4]}/{hash}` | Fan-out; extension stored in manifest, not path |
| Slug generation | **6-char base62**, random; retry on collision | Short URLs; reservation table in manifest |
| Slug reservation | Permanent per link lifecycle; new slug on `cp` | Collision hygiene |
| Share endpoints | Object path & short slug are **independent endpoints**, each with own state | `disable`/`enable`/`rm` act per-endpoint |
| Short-URL Content-Type | Served slug URL carries the object path's extension (`/s/f76.mp3`); slug identity stays clean (`f76`) | Caddy sets MIME without per-resource config; Mason also creates `/s/f76` for bare fallback |
| Caddy access control | `hide` `/objects/*` and `/.mason/*` request paths | Manifest not public; blobs reachable only via symlinks |
| Manifest durability | Remote writes atomic (temp + rename) | Survives interrupted RMW |
| `sync` conflict | **Remote wins**; overwrite local cache; warn on stderr | Single-user VPS; remote is authoritative |
| Blob reference count | Count enabled or disabled manifest endpoints, not filesystem symlinks | Disabled endpoints remove symlinks but still keep blobs re-enableable |
| Disabled endpoints | Remove that endpoint's symlink(s) from web tree; state in manifest | Caddy 404s; path/slug toggle independently; slug stays reserved |
| `setup` | Local-only: keys, write config, test SSH | Runs on laptop |
| `remote-init` | Server-side: dirs, permissions, Caddy snippet | Runs over SSH once |
| `doctor` | Read-only diagnostics for local + remote checks | No mutation |
| CLI framework | **Cobra** | De facto Go CLI standard |
| Testing | Table-driven unit tests in `domain/`; interfaces + fakes in `remote/`; golden CLI output | Fast, modular, CI-friendly |

---

## Command reference

Full behavioral spec. Implementation must match this surface.

### Global flags

| Flag | Meaning |
|------|---------|
| `-t, --target <name>` | Override default target |
| `--json` | Machine-readable output |
| `-n, --dry-run` | Show plan; change nothing |
| `-y, --yes` | Skip confirmation prompts |
| `-q, --quiet` | Minimal output |
| `-v, --verbose` | Extra detail |
| `--config <path>` | Explicit config file |

### Referring to a share: `<ref>`

- **Object path** — `super/rad/podcasts/foo.mp3`
- **Short slug** — `s/f76` (`s/`-prefix disambiguates)

Some commands resolve a ref to the whole share; others act on the specific
endpoint named by the ref:

- `info`, `url`, `mv`, and `cp` resolve either endpoint to the underlying share.
  `url` prints the short URL by default; use `--long` for the object-path URL.
- `rm`, `disable`, and `enable` act on the referenced endpoint.
- `mv <src> <dst>` moves or recreates the share's object-path endpoint at
  `<dst>` and leaves its slug endpoint unchanged. `<src>` may be either the old
  object path or the slug.
- `cp <src> <dst>` creates a new share at `<dst>` with a new slug, pointing at
  the same blob as `<src>`.

### Share — create / read

| Command | Local FS? | Behavior |
|---------|:---------:|----------|
| `mason share <file> [--as <path>]` | yes | Hash → upload blob if needed → create object-path link + short slug → print URLs |
| `mason ls [prefix]` | no | List shares; columnar + `--json` |
| `mason info <ref>` | no | Full detail: paths, slug, hash, size, timestamps, URLs, blob ref count |
| `mason url <ref> [--long]` | no | Print one URL (short default); for piping to clipboard |

**`share` path rules:**

- No `--as` → `<share.default_path>/<filename>` (e.g. `inbox/foo.mp3`)
- `--as dir/` → into directory, keep filename
- `--as path/to/name.ext` → exact path / rename

**Example output:**

```
Sharing 'foo.mp3' → super/rad/podcasts/episode-01.mp3
Content already on server — linking only.
  Long:  https://share.example.com/super/rad/podcasts/episode-01.mp3
  Short: https://share.example.com/s/f76.mp3
```

The served short URL carries the file extension so it opens with the right type
in a browser; the slug *identity* stays clean (`f76`). Mason also creates an
extensionless slug path (`/s/f76`) pointing at the same blob so copied or
hand-typed bare URLs still resolve, with degraded Content-Type for some file
types. Disabling or removing the slug endpoint affects both remote paths.

### Share — update / delete

| Command | Behavior |
|---------|----------|
| `mason mv <src> <dst>` | Re-home object-path symlink; no upload |
| `mason cp <src> <dst>` | New object path + **new slug**; same blob; no upload |
| `mason rm <ref>` | Remove an endpoint (object path or short slug independently); share remains while the other endpoint exists; blob GC-eligible once no manifest endpoint references it |
| `mason disable <ref>` | Endpoint (path or slug) returns 404; slug reserved |
| `mason enable <ref>` | Inverse of disable (per-endpoint) |

**Hotlink recovery:**

```bash
mason disable s/f76
mason cp super/rad/podcasts/foo.mp3 archive/foo.mp3
# distribute new short URL
```

### Share — maintenance

| Command | Behavior |
|---------|----------|
| `mason prune` | GC orphaned blobs with zero manifest endpoint references; **dry-run default**; `--yes` to delete |
| `mason sync` | Reconcile local cache ← remote manifest; implicit before reads |

### Deploy

| Command | Behavior |
|---------|----------|
| `mason init` | Scaffold project `.mason` |
| `mason deploy [target]` | rsync build tree; plan → progress → summary |
| `mason status [target]` | Plan only (`deploy -n`) |

Mirror-delete: **`--delete`**, confirmed unless `-y`.

### Setup / admin

| Command | Behavior |
|---------|----------|
| `mason setup` | Local onboarding wizard |
| `mason doctor` | Diagnostics (SSH, perms, symlinks, Caddy root) |
| `mason remote-init` | One-time server bootstrap |
| `mason target ls\|add\|use\|rm` | Manage named targets |

### Config schema (per target)

```yaml
default: home

targets:
  home:
    host: share.example.com
    user: mason
    root: /srv/share              # share site root (Caddy)
    base_url: https://share.example.com
    key_path: ~/.ssh/id_ed25519
    port: 22                      # optional
    share:
      default_path: inbox/
    deploy:
      build_dir: ./public
      remote_path: /srv/site      # may differ from share root
```

**Discovery:** walk up from cwd for `.mason`; fallback `~/.mason`.

### Dedupe / upload behavior

| Situation | Behavior |
|-----------|----------|
| New object path; blob exists | Link only; no transfer |
| New object path; blob missing | Upload blob, then link |
| Object path exists; same hash | No-op; return URLs |
| Object path exists; different hash | Re-point link; old blob GC-eligible |
| Same bytes; new object path | New share (new link + slug); shared blob |

---

## Remote layout & data model

### Directory tree (under target `root`)

```
/srv/share/
  .mason/                    # hidden from web (Caddy 404s /.mason/*)
    manifest.json            # authoritative index
  objects/                   # hidden from web (Caddy 404s /objects/*)
    ab/cd/{full-sha256-hex}  # blob files (no extension in path)
  s/
    f76.mp3                  # canonical slug symlink; name carries ext → ../objects/ab/cd/...
    f76                      # bare fallback slug symlink → ../objects/ab/cd/...
  inbox/
    foo.mp3                  # symlink → ../objects/...
  super/rad/podcasts/
    episode-01.mp3           # symlink → ../objects/...
```

Caddy site root = target `root`. Symlinks must stay under this tree. Caddy
**follows symlinks** but **hides** the `/objects/*` and `/.mason/*` request paths,
so the manifest is never public and blobs are reachable only through their `s/`
and object-path symlinks (not by guessing a hash). The canonical slug symlink's
name carries the object path's extension (`f76.mp3`) so Caddy infers Content-Type;
Mason also creates a bare fallback symlink (`f76`) for extensionless URLs. The
slug endpoint still has one identity (`f76`); disable/rm removes both slug
symlinks.

### Manifest schema (v1)

A **share** is a blob plus up to two **independent endpoints** — an object-path
endpoint and a short-slug endpoint — each carrying its own state so they can be
disabled/enabled/removed separately.

```json
{
  "version": 1,
  "blobs": {
    "sha256:abc...": {
      "size": 4412345,
      "uploaded_at": "2026-06-07T12:00:00Z"
    }
  },
  "shares": [
    {
      "id": "01J9Z...",
      "blob": "sha256:abc...",
      "path": { "value": "super/rad/podcasts/episode-01.mp3", "state": "ok" },
      "slug": { "value": "f76", "ext": ".mp3", "state": "ok" },
      "created_at": "2026-06-07T12:00:00Z",
      "updated_at": "2026-06-07T12:00:00Z"
    }
  ],
  "reserved_slugs": ["f76"]
}
```

- `state` (per endpoint): `ok` | `disabled`. A removed endpoint is absent/null.
- A share is **gone** once both endpoints are removed.
- `slug.value` is the clean identity (matches `s/f76`); `slug.ext` is appended to
  the **served** short URL (`/s/f76.mp3`) so Caddy sets the right Content-Type.
- `reserved_slugs` enforces the reservation invariant even after a slug endpoint
  is removed — a slug is never auto-reused for different content.
- Indexes (path → share, slug → share) are derived in memory from `shares`.
- A blob is **orphaned** (prune-eligible) when no manifest endpoint references
  it. Disabled endpoints still count because they can be re-enabled; raw symlink
  count is not authoritative.

### Share operation (internal)

1. Resolve target + config
2. Parse `--as` → object path
3. Hash local file (SHA-256)
4. Fetch remote manifest (or use cache + sync)
5. If object path exists with same hash → no-op, print URLs
6. If blob missing → rsync to `objects/...`
7. SSH: create/update symlinks — object path, `s/{slug}{ext}` for the canonical
   slug URL, and `s/{slug}` for bare fallback
8. Patch manifest; write remote **atomically** (temp + rename); update local cache
9. Print human or JSON output

If an operation is interrupted between remote filesystem mutation and manifest
write, the remote manifest remains authoritative. `doctor` should report
unmanifested symlinks and unreferenced blobs; `sync` only mirrors the remote
manifest into the local cache and does not invent remote state.

---

## Codebase structure & engineering standards

### Layout

```
mason/
├── cmd/mason/main.go           # entrypoint only
├── internal/
│   ├── cli/                    # cobra commands (thin handlers)
│   ├── config/                 # load, validate, discover, targets
│   ├── domain/                 # manifest, link, ref, slug, paths — pure logic
│   ├── output/                 # columnar human + JSON writers, error codes
│   ├── remote/                 # SSH session, rsync, remote FS, manifest I/O
│   ├── share/                  # share upload orchestration (two-phase create)
│   ├── links/                  # remote-link mutations: mv, cp, rm, disable, enable, prune
│   ├── deploy/                 # deploy planner + rsync runner
│   └── setup/                  # setup, doctor, remote-init, target admin
├── test/
│   ├── integration/            # build tag `integration` — optional real SSH
│   └── testdata/               # golden files, fixture manifests
├── go.mod
├── Makefile                    # test, lint, build
└── contextq.config
```

### Layering rules

1. **`domain/`** — no I/O, no cobra, no SSH. All invariants and state transitions
   live here. **Highest test coverage target (≥90%).**
2. **`remote/`** — all network and subprocess I/O. Expose narrow interfaces
   (`Uploader`, `ManifestStore`, `RemoteFS`) so `share/` and `deploy/` stay testable.
3. **`cli/`** — parse flags, call services, render `output/`. No business logic.
4. **`share/`**, **`links/`**, **`deploy/`**, **`setup/`** — orchestrate domain +
   remote. `share/` owns byte-moving upload; `links/` owns pure remote-link
   mutations (no upload) shared by `mv`/`cp`/`rm`/`disable`/`enable`/`prune`.

### Testing standards

| Layer | Approach |
|-------|----------|
| `domain/` | Table-driven unit tests; every dedupe row and state transition |
| `remote/` | Interface fakes; optional `integration` tests with local SSH fixture |
| `cli/` | Golden-file tests for plain + `--json` output |
| End-to-end | Manual gates against a real VPS (G5/G7); automate later if desired |

Run `go test ./...` in CI on every change. No skipped tests without a tracked reason.

### Style

- Standard `gofmt`, `go vet`, staticcheck if available
- Errors wrap with context (`fmt.Errorf("...: %w", err)`)
- Stable JSON field names (snake_case in `--json` output)
- User-facing errors: one clear sentence + hint (`run mason doctor`)
- Avoid premature abstraction; one file per command in `cli/` is fine

### Dependencies (initial)

- `github.com/spf13/cobra` — CLI
- `gopkg.in/yaml.v3` — config
- `golang.org/x/crypto/ssh` — SSH (or stdlib as appropriate)
- rsync invoked via `os/exec` (document requirement in `doctor`)

---

## Work plan

Tasks are ordered so **dependencies flow downward**. Each task is sized **XS**
(~1–3 hours) or **S** (~ half day). Task IDs are stable keys for contextq.

**Legend:** ⬜ not started · 🟡 in progress · ✅ done (update as work proceeds)

### Phase 0 — Foundation

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-001 | XS | — | Initialize Go module, `cmd/mason`, Makefile (`build`, `test`, `lint`) | `go build ./...` succeeds |
| M-002 | XS | M-001 | Cobra root command, version, global flags wired (stubs OK) | `mason --help` lists flags |
| M-003 | XS | M-001 | `internal/output` — error type with codes, JSON envelope, quiet/verbose | Unit tests for output helpers |

**Phase gate 0:** `go test ./internal/output/...` passes; `mason --help` works.

---

### Phase 1 — Configuration

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-010 | S | M-001 | Config types + YAML parse + validation | `domain`-level tests for invalid configs |
| M-011 | XS | M-010 | Config discovery (walk up cwd, fallback `~/.mason`) | Tests with temp dirs |
| M-012 | XS | M-010 | Target resolution (`default`, `-t` override) | Tests for missing/ambiguous target |

**Phase gate 1:** Load sample config from testdata; resolve default target.

---

### Phase 2 — Remote transport

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-020 | S | M-010 | `remote.SSH` — connect with key from config, run remote commands | Fake-friendly interface + unit tests |
| M-021 | S | M-020 | `remote.Rsync` — upload file/dir with progress callback | Tests with local temp dirs |
| M-022 | XS | M-020 | `remote.RemoteFS` — mkdir, symlink, rm, read/write small files | Used by share later |

**Phase gate 2:** Integration test (optional tag) or scripted fake: SSH echo + rsync round-trip locally.

---

### Phase 3 — Share domain core

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-030 | S | M-001 | `domain` manifest types, JSON marshal, ref-count logic | ≥90% coverage on domain |
| M-031 | XS | M-030 | `<ref>` parsing (`s/` slug vs object path) | Table tests |
| M-032 | XS | M-030 | Object path normalization + `--as` rules | Table tests |
| M-033 | XS | M-030 | SHA-256 hashing + blob/object key paths | Table tests |
| M-034 | XS | M-030 | Slug mint + reservation + collision retry | Table tests |
| M-035 | S | M-030 | Dedupe / upload decision function (pure) | One test per row in dedupe table |

**Phase gate 3:** All domain tests pass; dedupe table fully covered.

---

### Phase 4 — Manifest I/O & cache

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-040 | S | M-020, M-030 | Remote manifest fetch + **atomic** write (RMW over SSH, temp + rename) | Tests with fake remote |
| M-041 | S | M-040 | Local cache read/write under XDG cache path | Tests |
| M-042 | XS | M-040, M-041 | `sync` logic — remote wins, warn on overwrite | Tests |
| M-043 | S | M-022, M-040 | `links.Service` core — create/update endpoints for `share`; common manifest + symlink helpers | Service tests with fake remote |

**Phase gate 4:** Round-trip manifest remote → cache → remote.

---

### Phase 5 — Share MVP orchestration & read commands

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-050 | S | M-021, M-022, M-035, M-040, M-043 | `share.Service` — two-phase upload orchestration (uses `links.Service` to create endpoints) | Service tests with fake remote |
| M-051 | S | M-050, M-002, M-003 | `mason share` command + output | Golden output tests |
| M-052 | XS | M-042, M-002 | `mason sync` command | Tests |
| M-053 | S | M-041, M-003, M-002 | `mason ls` (+ prefix filter, `--json`) | Golden tests |
| M-054 | XS | M-053 | `mason info`, `mason url` | Golden tests |

**Phase gate 5 (Share MVP):** Against a test VPS or documented manual script:

```bash
mason share ./test.mp3 --as demos/test.mp3
mason ls
mason url s/XXXX | head -1   # returns URL
curl -I "<short url>"        # 200
mason share ./test.mp3 --as demos/copy.mp3       # link-only, no re-upload
```

---

### Phase 6 — Deploy

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-060 | S | M-021, M-010 | Deploy planner — diff local build_dir vs remote (dry-run) | Unit tests |
| M-061 | S | M-060, M-002, M-003 | `mason deploy`, `mason status` with plan/progress/summary | Golden tests |
| M-062 | XS | M-010, M-002 | `mason init` — scaffold `.mason` | Writes valid YAML |

**Phase gate 6:**

```bash
mason init && mason status && mason deploy -n
mason deploy   # shows plan, progress, summary
```

---

### Phase 7 — Share lifecycle & maintenance

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-055 | XS | M-043 | `mason mv`, `mason cp` services + commands | Domain + integration tests |
| M-056 | XS | M-043 | `mason rm` service + command (per-endpoint) | Tests |
| M-057 | S | M-043 | `mason disable`, `mason enable` services + commands (per-endpoint: path or slug) | Tests |
| M-058 | S | M-043 | `mason prune` service + command (dry-run default, `--yes`) | Tests |

**Phase gate 7 (Share lifecycle):**

```bash
mason disable s/XXXX && curl -I "<short url>"  # 404
mason enable s/XXXX && curl -I "<short url>"   # 200
mason cp demos/test.mp3 demos/copy.mp3         # new slug, no re-upload
mason rm demos/copy.mp3
mason prune -n                                 # shows orphaned blobs, if any
```

---

### Phase 8 — Setup & admin

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-070 | S | M-010, M-020 | `mason setup` — key check, config write, SSH test | Interactive + testable core |
| M-071 | S | M-020, M-022 | `mason remote-init` — layout + Caddy snippet (incl. `hide` for `/objects/*` and `/.mason/*`) | Documented server tree |
| M-072 | S | M-020 | `mason doctor` — SSH, perms, manifest, symlink probe; verify `/objects/` and `/.mason/` are **not** publicly reachable | Actionable diagnostics |
| M-073 | S | M-010, M-002 | `mason target ls|add|use|rm` | Config r/w tests |

**Phase gate 8:** Fresh machine path: `setup` → `remote-init` → `doctor` all green.

---

### Phase 9 — Hardening

| ID | Size | Depends | Task | Deliverable |
|----|------|---------|------|-------------|
| M-080 | S | M-058 | README quickstart + env requirements (rsync, ssh) | User-facing docs |
| M-081 | XS | all | CI workflow: `go test ./...` | GitHub Actions or equivalent |
| M-082 | S | M-051, M-061, M-057 | End-to-end smoke script (optional `integration` tag) | Script in `test/` |

**Phase gate 9 (Release candidate):** All gates pass; `go test ./...` clean; manual smoke on real VPS documented.

---

## Success gates

Use these to judge whether a phase (or the whole project) is done. Do not advance
past a gate with failing criteria.

| Gate | Phase | Criteria |
|------|-------|----------|
| **G0** | 0 | `go test ./...` passes; `mason --help` shows global flags |
| **G1** | 1 | Config loads from `.mason` and `~/.mason`; invalid config fails with clear error |
| **G2** | 2 | Can run remote command and rsync file via library API (fake or real) |
| **G3** | 3 | Domain tests cover full dedupe table, ref parsing, slug reservation |
| **G4** | 4 | Manifest RMW + local cache sync; remote wins on conflict |
| **G5** | 5 | Share MVP script (above) passes on VPS; `--json` output parses |
| **G6** | 6 | Deploy shows plan/progress/summary; `--delete` requires confirmation |
| **G7** | 7 | Share lifecycle script passes; endpoint-specific disable/enable/rm behavior works |
| **G8** | 8 | Setup → remote-init → doctor path succeeds for new target |
| **G9** | 9 | README quickstart complete; CI green; smoke test documented |

**Project complete** when G0–G9 are satisfied and every command in [Command reference](#command-reference) is implemented to spec.

---

## contextq integration

### Loading the queue

From the repo root (requires [contextq](https://github.com/norlinga/contextq)):

```bash
contextq queue create "$(sed -n '/^```contextq$/,/^```$/p' IMPLEMENTATION_PLAN.md | sed '1d;$d')" --name mason
```

Or paste the block below as the context argument to `contextq queue create`.

### Pushing work

Push tasks in dependency order (Phase 0 first). Keys are task IDs:

```bash
contextq item push mason M-001
contextq item push mason M-002
# … continue through M-082
```

Agents: read `.cursor/skills/contextq/SKILL.md`. **Take work only via `item pop`.**
Update items to `DONE` | `FAILED` | `CANCELED` when finished.

### Per-task completion checklist

When marking a task `DONE`:

1. Deliverable for that task ID exists
2. `go test ./...` passes
3. Any new `--json` fields match this plan
4. Gate criteria for the task's phase still hold (re-run if needed)

```contextq
You are implementing Mason — a local Go CLI for publishing to a personal VPS (Shape A: no remote daemon). Read IMPLEMENTATION_PLAN.md for full detail; this queue tracks bite-sized tasks M-001 through M-082.

PHILOSOPHY (follow on every task):
- Share and Deploy are separate workflows; never mix share object store into deploy.
- Local-touching commands (share, deploy) transfer bytes; remote-link commands (ls, info, url, mv, cp, rm, disable, enable, prune, sync) only touch symlinks + manifest.
- Plain output must be columnar/greppable; --json is first-class.
- Share identity = (curated object path, content hash). Object paths come from --as or share.default_path — NOT from local filesystem layout.
- Two-phase share: ensure blob exists, then ensure link exists. Same bytes + new path = link only, no re-upload.
- Slugs are reserved forever for their share; new slug on cp. disable/enable is reversible; rm is not. prune is dry-run by default.
- Each share has TWO independent endpoints (object path + short slug), each with its own state — disable/enable/rm act per-endpoint.
- Blob reference counts come from manifest endpoints, not raw symlink counts. Disabled endpoints still count.
- Slug identity is clean (f76); the SERVED short URL carries the object path's extension (/s/f76.mp3) so Caddy sets Content-Type. Mason also creates /s/f76 as the extensionless fallback.
- Remote manifest is authoritative; local cache syncs from remote (remote wins).

ARCHITECTURE:
- cmd/mason + internal/{cli,config,domain,output,remote,share,links,deploy,setup}
- domain/ is pure logic, highest test coverage, no I/O
- remote/ owns SSH, rsync, manifest RMW
- share/ owns byte-moving upload; links/ owns pure remote-link mutations (no upload)
- cli/ is thin: flags → service → output

TECHNICAL DEFAULTS (locked):
- Config: YAML (.mason, ~/.mason)
- Manifest: JSON at {root}/.mason/manifest.json; writes atomic (temp + rename); schema = blobs + shares[] (each with independent path/slug endpoints) + reserved_slugs
- Hash: SHA-256; blobs at objects/ab/cd/{hash} (no extension on blob)
- Slugs: 6-char base62; served as /s/{slug}{ext}; extensionless /s/{slug} fallback; reservation persists in manifest
- Caddy hides /objects/* and /.mason/* (manifest private; blobs only via symlinks)
- Cobra CLI; table-driven tests; golden CLI output tests

CURRENT TASK: Work the item key returned by `contextq item pop mason`. Find the task row in IMPLEMENTATION_PLAN.md Work plan section. Implement only that task's deliverable; do not scope-creep adjacent tasks.

BEFORE DONE:
- go test ./... passes
- deliverable matches task row
- if CLI output changed, add/update golden tests
- run `contextq item update mason <key> DONE`

If blocked, update FAILED with reason in your message. Do not pick work from item list — pop only.
```

---

## Documentation chain (updated)

| Step | Document | Role |
|------|----------|------|
| 1 | [PROJECT_DESCRIPTION.md](./PROJECT_DESCRIPTION.md) | Motivation |
| 2 | [DESIGN.md](./DESIGN.md) | Historical first pass |
| 3 | [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md) | Architecture refinement |
| 4 | [COMMANDS.md](./COMMANDS.md) | CLI behavioral spec |
| 5 | **IMPLEMENTATION_PLAN.md** *(this file)* | **Implementation guide + work queue** |
