# Mason Implementer Brief

This document is for an implementation agent resuming Mason work after context has
been cleared. The implementer is the primary builder. The job is to take work from
the `contextq` queue, implement exactly one task deliverable at a time, verify it,
and mark the item done.

Both *implementer* and *implementor* are acceptable English; this repo uses
**implementer** to match [REVIEWER_BRIEF.md](./REVIEWER_BRIEF.md).

## Role

Operate in implementation mode:

1. Orient using this brief and the source-of-truth docs below.
2. Claim work only via `contextq item pop mason`.
3. Find the task row in [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) and
   implement **only** that deliverable.
4. Add or update tests that prove the behavior.
5. Run verification before marking the item `DONE`.
6. Do not commit or push unless the user explicitly asks.

When the user says `proceed`, `next`, or `continue`, treat it as:

> Run the contextq pop → implement → verify → update cycle for the next queue item.

If the user asks you to pause (for review, commits, or coordination), stop after
finishing the current item or before popping a new one — follow their instruction.

A separate reviewer agent may inspect your work using [REVIEWER_BRIEF.md](./REVIEWER_BRIEF.md).
Expect review findings and be ready to make small targeted fixes if asked.

## Source Of Truth

Read these documents as needed:

| Priority | Document | Use |
|----------|----------|-----|
| 1 | [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) | Primary guide: architecture, work plan, task IDs, phase gates, manifest schema, contextq context block |
| 2 | [COMMANDS.md](./COMMANDS.md) | Authoritative CLI behavior and output expectations |
| 3 | [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md) | Architectural rationale and corrected share model |
| 4 | [DESIGN.md](./DESIGN.md) | Historical first pass; only for context on what was superseded |
| 5 | [PROJECT_DESCRIPTION.md](./PROJECT_DESCRIPTION.md) | Original human intent |

If docs conflict, follow the precedence above. For implementation sequencing and
unresolved technical choices, **IMPLEMENTATION_PLAN.md wins**.

Also read before acting on queue work:

- [.cursor/skills/contextq/SKILL.md](./.cursor/skills/contextq/SKILL.md) — queue
  coordination protocol
- [REVIEWER_BRIEF.md](./REVIEWER_BRIEF.md) — what reviewers will check; useful
  for self-review before marking `DONE`

## What Mason Is

Mason is a **local Go CLI** for publishing to a personal VPS. **Shape A**: no
Mason daemon on the server. The remote is a filesystem contract (layout + Caddy +
SSH); all orchestration runs locally.

Two workflows share config, targets, and transport — but **not** storage mechanics:

| Workflow | Commands | Moves bytes? |
|----------|----------|:------------:|
| **Share** | `share` | yes |
| **Deploy** | `deploy` | yes |
| **Remote-link** | `ls`, `info`, `url`, `mv`, `cp`, `rm`, `disable`, `enable`, `prune`, `sync` | no |

Remote-link commands manipulate symlinks and the manifest only. Never route deploy
through the share object store.

## Contextq Workflow

The repo uses [contextq](https://github.com/norlinga/contextq) with project-local
storage (`.contextq-data/`, configured in `contextq.config`).

### Core rules

- **Take work only with `item pop`.** Never claim a key from `item list` output.
- **`CLAIMED` is set by `pop`, not `update`.** `item update` accepts only `DONE`,
  `FAILED`, or `CANCELED`.
- Read queue context immediately before each `pop`.
- Update the popped key as soon as the outcome is known.
- Do not push duplicate available keys.

### Implementer loop

```bash
# 1. Orient (optional but recommended)
contextq queue read mason
contextq item list mason --state DONE    # observation only

# 2. Claim work — only path to CLAIMED
contextq item pop mason
# Returns a key like M-031

# 3. Implement: find the task row in IMPLEMENTATION_PLAN.md Work plan section

# 4. Verify (see Verification section)

# 5. Mark complete
contextq item update mason <TASK_ID> DONE
# Or FAILED / CANCELED with reason if blocked
```

Useful observation commands (do **not** use these to claim work):

```bash
contextq item list mason
contextq item list mason --state AVAILABLE
contextq item read mason <TASK_ID>
contextq item history mason <TASK_ID>
contextq --json queue read mason
```

### Task ID numbering

Task IDs are **topical blocks**, not a continuous sequence. Gaps are intentional:

| Block | IDs | Phase |
|-------|-----|-------|
| Foundation | M-001 – M-003 | 0 |
| Configuration | M-010 – M-012 | 1 |
| Remote transport | M-020 – M-022 | 2 |
| Share domain | M-030 – M-035 | 3 |
| Manifest I/O | M-040 – M-043 | 4 |
| Share MVP | M-050 – M-054 | 5 |
| Deploy | M-060 – M-062 | 6 |
| Share lifecycle | M-055 – M-058 | 7 |
| Setup / admin | M-070 – M-073 | 8 |
| Hardening | M-080 – M-082 | 9 |

There are no M-023–M-029 tasks. M-004–M-009, M-013–M-019, etc. are also unused
gaps between phases.

### Queue coverage note

The `mason` queue does **not** contain every task defined in the plan. Early
phases (M-001–M-019) were implemented before queue tracking began. The queue
starts roughly at M-020. If code for a dependency already exists in the repo,
you may proceed even when the dependency is not a `DONE` queue item.

If this baseline is stale, prefer `contextq item list mason`, git history, and
the latest user instruction.

Known `DONE` baseline when this brief was written:

- `M-020` — `remote.SSH`
- `M-021` — `remote.Rsync`
- `M-022` — `remote.RemoteFS` (`SSHFS` + `RemoteFS` interface)
- `M-030` — `domain` manifest types, JSON, ref-count logic

Next FIFO item at time of writing: **M-031** (`<ref>` parsing).

## Scope Discipline

Every task should answer:

- Does this implement **exactly** the deliverable in the task row?
- Did I avoid pulling in adjacent future tasks?
- Did I preserve package boundaries?
- Did I leave unrelated docs and code untouched?

**Do not scope-creep.** Examples:

- M-030: manifest types and ref counts — not ref parsing (M-031), not slug minting
  (M-034), not dedupe logic (M-035)
- M-022: remote FS primitives — not atomic manifest RMW (M-040)
- M-040: manifest I/O — not CLI commands (M-051+)

If a dependency is missing and blocks the task, mark `FAILED` with a clear reason
rather than implementing the dependency yourself (unless it is genuinely part of
the same task row).

## Package Boundaries

```
cmd/mason/              entrypoint only
internal/
  cli/                  Cobra commands: flags → service → output
  config/               YAML load, validate, discover, target resolution
  domain/               pure logic: manifest, ref, slug, paths — NO I/O
  output/               columnar human output, JSON envelope, error codes
  remote/               SSH, rsync, RemoteFS, manifest I/O (future)
  share/                share upload orchestration (future)
  links/                remote-link mutations (future)
  deploy/               deploy planner + rsync runner (future)
  setup/                setup, doctor, remote-init, targets (future)
test/
  integration/          build tag `integration` (future)
  testdata/             golden files, fixtures (future)
```

Layering rules:

- **`domain/`** — no I/O, no Cobra, no SSH, no filesystem. Highest test coverage
  (≥90%). All invariants and state transitions live here.
- **`remote/`** — all network and subprocess I/O. Expose narrow interfaces
  (`CommandRunner`, `ProcessRunner`, `RemoteFS`, later `ManifestStore`) with
  injectable fakes.
- **`cli/`** — parse flags, call services, render `output/`. No business logic.
- **Orchestration packages** (`share/`, `links/`, etc.) — compose `domain/` +
  `remote/`.

## Naming And Code Style

Match existing repo conventions:

| Area | Pattern |
|------|---------|
| Package names | Short, lowercase: `remote`, `domain`, `config` |
| Files | One concern per file: `ssh.go`, `remotefs.go`, `manifest.go` |
| Constructors | `NewSSH`, `NewSSHFS`, `NewRsync`, `NewManifest` |
| Injectable variants | `NewSSHWithDialer`, `NewRsyncWithRunner` |
| Interfaces | Narrow seams: `CommandRunner`, `RemoteFS`, `ProcessRunner` |
| Concrete types | `SSH`, `SSHFS`, `Rsync` — struct name when no naming conflict |
| Errors | `fmt.Errorf("context: %w", err)`; one clear user-facing sentence where appropriate |
| JSON fields | Stable snake_case in manifest and `--json` output |
| Comments | Sparse; explain non-obvious invariants only |
| Formatting | `gofmt` — always run before marking `DONE` |

Avoid premature abstraction. A focused diff that solves the task row is better
than scaffolding future phases.

### Patterns established in completed work

**`remote/SSH`** — `CommandRunner` interface; `SSHDialer` / `SSHClient` /
`SSHSession` subsets for test injection; `SSHOptionsFromTarget(config.Target)`.

**`remote/Rsync`** — wraps `rsync` via `ProcessRunner`; progress callback from
parsed stdout; local temp-dir round-trip tests gated on `rsync` being installed.

**`remote/RemoteFS`** — `RemoteFS` interface; `SSHFS` implementation over
`CommandRunner`; `RemotePath(root, rel)` helper; `shellQuote` for safe remote
shell commands; `WriteFile` uses base64 pipe for binary-safe transfer.

**`domain/manifest`** — v1 schema types; `ParseManifest` / `MarshalManifest`;
`Validate`; `BlobRefCount` / `OrphanedBlobs`; `BuildIndexes`; disabled endpoints
still count toward blob refs.

Read adjacent files in the target package before adding code. New code should read
as if written by the same author.

## Testing Standards

| Layer | Approach |
|-------|----------|
| `domain/` | Table-driven unit tests; ≥90% coverage; every state transition and dedupe row (when applicable) |
| `remote/` | Fake runners/dialers; optional local rsync tests with `t.Skip` if binary missing |
| `cli/` | Golden-file tests for plain + `--json` output (when CLI changes) |
| Integration | `integration` build tag; real SSH/VPS — manual or later automation |

Test quality expectations:

- Test **behavior**, not implementation noise.
- Cover edge cases, not only happy paths.
- Use `t.Helper()` in test utilities.
- Use `t.TempDir()` for filesystem fixtures.
- Name subtests with `t.Run(tt.name, ...)`.

Before marking `DONE`:

```bash
gofmt -w <changed files>
go test ./...
go vet ./...
make lint          # runs go vet ./...
```

For `domain/` tasks, also check coverage:

```bash
go test ./internal/domain/... -cover
```

In restricted sandboxes where `$HOME` caches may be read-only:

```bash
env GOCACHE=/tmp/mason-go-cache GOMODCACHE=/tmp/mason-go-mod-cache go test ./...
```

If CLI output changed, add or update golden tests per the task row and
IMPLEMENTATION_PLAN.md.

## Project Invariants

These rules apply on **every** task. Violating them is a defect:

- **Shape A** — no remote Mason daemon.
- **Share identity** — curated object path + content hash; paths come from `--as`
  or `share.default_path`, not local filesystem layout.
- **Two-phase share** — ensure blob exists, then ensure link exists. Same bytes +
  new path = link only, no re-upload.
- **Two independent endpoints** per share — object path and short slug; each has
  its own `ok` / `disabled` state; `rm` removes one endpoint at a time.
- **Blob ref counts** — from manifest endpoints, not raw symlink count. Disabled
  endpoints still count.
- **Slug reservation** — slugs are never auto-reused for different content; `cp`
  mints a new slug.
- **`prune`** — dry-run by default; explicit GC only.
- **Remote manifest is authoritative** — local cache mirrors it; remote wins on
  conflict.
- **Atomic manifest writes** — temp + rename (when M-040+ is implemented).
- **Caddy** — hides `/objects/*` and `/.mason/*`; blobs reachable only via
  symlinks.
- **Short URLs** — served as `/s/{slug}{ext}` (e.g. `/s/f76.mp3`); bare fallback
  `/s/{slug}` also created.

## Per-Task Checklist

Before `contextq item update mason <KEY> DONE`:

1. Deliverable for that task ID exists in the codebase.
2. Scope matches the task row — no adjacent-task scope creep.
3. `gofmt` applied to changed Go files.
4. `go test ./...` passes.
5. `go vet ./...` (or `make lint`) passes.
6. Coverage meets phase expectations (especially `domain/` ≥90%).
7. New `--json` fields match IMPLEMENTATION_PLAN.md (if applicable).
8. Phase gate criteria for the task's phase still hold (re-run if needed).

If blocked:

```bash
contextq item update mason <KEY> FAILED
```

Leave a clear reason in your final message (missing dependency, ambiguous spec,
failing test infrastructure, etc.).

## What Not To Do

- Do not `git commit` or `git push` unless the user explicitly asks.
- Do not pick work from `item list` — only `item pop`.
- Do not mark another agent's item `DONE` or `FAILED`.
- Do not implement multiple queue items in one pop unless the user explicitly
  asks for batch/phase mode.
- Do not add README or doc files unless the task row calls for it (e.g. M-080).
- Do not add dependencies beyond those listed in IMPLEMENTATION_PLAN.md without
  user approval.
- Do not skip tests for non-trivial behavior.

## Session Restart Checklist

When starting cold:

1. Read this brief.
2. Skim [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) Work plan and
   Architecture summary.
3. Run `contextq item list mason` to see current progress.
4. Inspect `internal/` to see what packages exist.
5. Run `go test ./...` to confirm the tree is healthy.
6. Wait for user signal to `pop`, or run the implementer loop if told to proceed.

## Quick Reference — Manifest v1

Authoritative schema lives in IMPLEMENTATION_PLAN.md. Core shape:

```json
{
  "version": 1,
  "blobs": { "sha256:abc...": { "size": 0, "uploaded_at": "..." } },
  "shares": [{
    "id": "...",
    "blob": "sha256:abc...",
    "path": { "value": "curated/path.mp3", "state": "ok" },
    "slug": { "value": "f76", "ext": ".mp3", "state": "ok" },
    "created_at": "...",
    "updated_at": "..."
  }],
  "reserved_slugs": ["f76"]
}
```

- Removed endpoint → JSON `null` / Go `nil` pointer.
- Share is gone when **both** endpoints are removed.
- Blob path on disk: `objects/{hash[0:2]}/{hash[2:4]}/{hash}` (no extension).
- Manifest path: `{root}/.mason/manifest.json`.

## Quick Reference — Config

- Discovery: walk up from cwd for `.mason`; fallback `~/.mason`.
- Format: YAML; `gopkg.in/yaml.v3`; `KnownFields(true)` on decode.
- Per-target fields: `host`, `user`, `root`, `base_url`, optional `key_path`,
  `port`, `share.default_path`, `deploy.build_dir`, `deploy.remote_path`.

---

*Keep this brief aligned with REVIEWER_BRIEF.md and IMPLEMENTATION_PLAN.md. Update
the "Known DONE baseline" section when major milestones land.*
