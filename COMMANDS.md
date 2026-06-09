# Mason — Command Surface

This document is the **authoritative specification of Mason's CLI**. It defines
every command, its behavior, and the conventions that tie them together. It was
derived from a working session that prioritized UX / developer UX above all
other concerns.

Where this document and [DESIGN.md](./DESIGN.md) disagree, **this document and
[DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md) take precedence.** DESIGN.md is
retained as a historical record of the first-pass architecture.

**Document chain:** [PROJECT_DESCRIPTION.md](./PROJECT_DESCRIPTION.md) → [DESIGN.md](./DESIGN.md) → [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md) → **COMMANDS.md** → [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md)

---

## Mental model

Two ideas organize the entire CLI.

**1. Two workflows that share infrastructure but not lifecycle.**

| Workflow | What it does | Lifecycle |
|----------|--------------|-----------|
| **Share** | Publish a single file and get back a shareable URL | Rich CRUD: list, re-home, duplicate, disable, remove |
| **Deploy** | Sync a build-output directory to a remote path | Simple: transfer a tree, report clearly what happened |

They share config, targets, and transport (SSH/rsync), but they are *not* the
same operation and do not share remote storage mechanics.

**2. Within Share: local-touching commands vs. remote-link commands.**

- **Local-touching** (`share`, `deploy`) read your filesystem — they hash,
  transfer bytes, and show progress. These are the heavy, occasional commands.
- **Remote-link** (`ls`, `info`, `url`, `mv`, `cp`, `rm`, `disable`, `enable`,
  `prune`, `sync`) only manipulate symlinks and manifest entries against blobs
  **already on the server**. No local file is required and no bytes are
  transferred. These are the cheap, frequent commands.

The underlying storage model (see [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md)
for detail): bytes live once in a content-addressed blob store; every reachable
URL is a symlink to a blob; blobs are reference-counted by manifest endpoints,
not by raw filesystem state. A "share" is therefore a **link**, not a blob — the
same bytes can live at many object paths as many independent shares. Each share
has two **independently controllable endpoints** — its object path and its short
slug — that you can `disable`/`enable`/`rm` separately.

---

## Conventions

### Referring to a share: `<ref>`

A `<ref>` names an existing share. It is either:

- an **object path** — `super/rad/podcasts/foo.mp3`, or
- a **short slug in URL form** — `s/f76`.

Mason treats a `s/`-prefixed token as a slug; anything else is an object path.

Some commands act on a whole share, while others act on the specific endpoint
named by the ref:

- `info`, `url`, `mv`, and `cp` resolve either endpoint to the underlying share.
  `url` prints the short URL by default; use `--long` for the object-path URL.
- `rm`, `disable`, and `enable` act on the referenced endpoint.
- `mv <src> <dst>` moves or recreates the share's object-path endpoint at
  `<dst>` and leaves its slug endpoint unchanged. `<src>` may be either the old
  object path or the slug.
- `cp <src> <dst>` creates a new share at `<dst>` with a new slug, pointing at
  the same blob as `<src>`.

### Global flags

| Flag | Meaning |
|------|---------|
| `-t, --target <name>` | Override the default target for this command |
| `--json` | Emit machine-readable output (for `jq`, scripting) |
| `-n, --dry-run` | Show what would happen; change nothing |
| `-y, --yes` | Skip confirmation prompts |
| `-q, --quiet` | Minimal output |
| `-v, --verbose` | Extra detail |
| `--config <path>` | Use a specific config file |

Plain output is always columnar and greppable. `--json` is the structured
counterpart for piping. Both are first-class — the Unix-composability of share
state (`mason ls | grep`, `mason ls --json | jq`) is a design requirement, not
an afterthought.

---

## Share family

| Command | Touches local FS? | Behavior |
|---------|:-:|----------|
| `mason share <file> [--as <path>]` | yes | Hash the file → reuse the blob if already on the server, else upload it → create an object-path link and a short slug → print both URLs. |
| `mason ls [prefix]` | no | List shares, optionally under a path prefix. Columnar + greppable; `--json` for piping. |
| `mason info <ref>` | no | Full detail for one share: object path, slug, hash, size, timestamps, both URLs, and the blob's reference count. |
| `mason url <ref>` | no | Print a single URL to stdout (short by default; `--long` for the readable one). For `mason url foo.mp3 \| pbcopy`. |
| `mason mv <src> <dst>` | no | Re-home: move the object-path link to a new path. Pure remote symlink op. |
| `mason cp <src> <dst>` | no | Duplicate: create a new object path **and a new slug** pointing at the same blob. No upload. |
| `mason rm <ref>` | no | Remove an endpoint (object path or short slug, addressable independently). The share remains while the other endpoint exists. The blob becomes garbage-collectible once no manifest endpoint references it. |
| `mason disable <ref>` | no | Reversibly stop a link from resolving (returns 404). The slug stays reserved to this entry. |
| `mason enable <ref>` | no | Re-activate a disabled link. Exact inverse of `disable`. |
| `mason prune` | no | Garbage-collect orphaned blobs (those with zero manifest endpoint references). **Dry-run by default**; `--yes` to delete. |
| `mason sync` | no | Reconcile the local cache against the remote manifest. Runs implicitly (best-effort) before read commands; invoke explicitly when you suspect drift. |

### `mason share` — the one heavy command

```bash
mason share ~/Recordings/foo.mp3
mason share ~/Recordings/foo.mp3 --as super/rad/podcasts/
mason share ~/Recordings/foo.mp3 --as super/rad/podcasts/episode-01.mp3
```

- **No `--as`** → the file lands at `<default_path>/<filename>`, where
  `default_path` is a per-target config value (e.g. `inbox/` or `shares/`). The
  default bucket is just the implicit `--as`.
- **`--as` with a trailing slash** → "into this directory, keep the filename."
- **`--as` with a full path** → "this exact path," which also lets you rename.

Upload is two-phase: hash locally, and if that blob already exists on the
server, **skip the transfer** and just create the requested link(s). Re-sharing
identical bytes to a new path is therefore the normal, cheap way to add a path —
never an error.

Example output:

```
Sharing 'foo.mp3' → super/rad/podcasts/episode-01.mp3
Content already on server — linking only.
  Long:  https://share.example.com/super/rad/podcasts/episode-01.mp3
  Short: https://share.example.com/s/f76.mp3
```

When bytes must actually be uploaded, a progress line appears before the URLs.

The served short URL carries the file's extension (`/s/f76.mp3`) so it opens with
the right type in a browser; the slug *identity* stays clean (`f76`). Mason also
creates an extensionless slug path (`/s/f76`) pointing at the same blob so copied
or hand-typed bare URLs still resolve, with degraded Content-Type for some file
types (e.g. a PDF downloads instead of opening inline). Disabling or removing the
slug endpoint affects both remote paths.

### `mason ls` — listing and piping

```
$ mason ls
PATH                         SLUG  SIZE    HASH     SHARED      STATE
super/rad/podcasts/foo.mp3   f76   4.2 MB  ab12cd…  2026-06-05  ok
Recordings/bar.wav           q1z   8.1 MB  9f33aa…  2026-06-06  ok
old/leak.mp3                 —     4.2 MB  ab12cd…  2026-05-01  disabled

$ mason ls super/rad           # filter by prefix
$ mason ls | grep podcasts     # plain output is greppable
$ mason ls --json | jq '.[].short_url'
```

### Disable / enable and the hotlink kill-switch

`disable` ⇄ `enable` is a symmetric on/off switch on a single endpoint. Distinct
from this is a standing invariant: **a slug is never auto-assigned to different
content**, regardless of enable/disable state — that's collision hygiene, not
part of the on/off semantics.

The canonical recovery flow when a short URL gets abused/hotlinked:

```bash
mason disable s/f76                                   # kill the leaked link
mason cp super/rad/podcasts/foo.mp3 archive/foo.mp3   # fresh path + fresh slug
# ...redistribute the new short URL; the old one stays dead.
mason enable s/f76                                    # later, if you change your mind
```

`rm` remains the irreversible "forget it entirely"; `disable` is the reversible
"make it stop."

---

## Deploy family

Deploy is intentionally simpler than share. It has no per-object CRUD — its
value is **observability**: knowing exactly what a transfer is about to do, what
it's doing, and what it did. This must stay legible whether a site is a dozen
files or hundreds of posts with heavy media.

| Command | Behavior |
|---------|----------|
| `mason init` | Scaffold a project `.mason` for deploy (target, build directory, remote root). |
| `mason deploy [target]` | rsync the build tree to the remote root, with a pre-flight plan, live progress, and a post-run summary. |
| `mason status [target]` | Show the deploy plan **without transferring** — what's new, changed, unchanged, or stale. Equivalent to `deploy -n`. |

Mirror-delete (removing remote files absent locally) is **opt-in** via
`--delete` and confirmed before running — the one genuine footgun.

Example:

```
$ mason deploy
Target: home (share.example.com:/srv/site)
Comparing local ./public ↔ remote …  done
Plan: 12 new · 3 changed · 184 unchanged · 1 to remove   (28.4 MB to send)
Sending  ████████████░░░░  18.1/28.4 MB  6.2 MB/s  img/cover.png
✓ Done in 5.1s — 15 files updated, 28.4 MB sent, 1 removed
```

When nothing changed:

```
✓ Already up to date (199 files, 0 B sent)
```

---

## Setup / admin family

| Command | Behavior |
|---------|----------|
| `mason setup` | Local onboarding wizard: ensure an SSH key exists, write/validate config, test connectivity. |
| `mason doctor` | Diagnostics: SSH reachability, remote directory permissions, symlink serving, Caddy root sanity. |
| `mason remote-init` | One-time server bootstrap: create the store layout, configure the deploy user, print/install the Caddy snippet. |
| `mason target ls\|add\|use\|rm` | Manage named targets without hand-editing config. |

---

## Config fields referenced by commands

A sketch of the per-target fields the commands above rely on (final schema lives
in DESIGN_REFINEMENT.md):

```yaml
default: home

targets:
  home:
    host: share.example.com
    user: mason
    root: /srv/share              # site root served by Caddy
    base_url: https://share.example.com
    key_path: ~/.ssh/id_ed25519
    share:
      default_path: inbox/        # implicit --as for un-organized shares
    deploy:
      build_dir: ./public         # local tree to deploy
      remote_path: /srv/site      # where deploy lands (may differ from share root)
```

Config discovery: `.mason` in the current directory walking up to the project
root, falling back to `~/.mason` for user-level defaults — so ad-hoc
`mason share` works from anywhere.

---

## Command index (quick reference)

```
Share — create / read
  mason share <file> [--as <path>]
  mason ls [prefix]
  mason info <ref>
  mason url <ref> [--long]

Share — update / delete
  mason mv <src> <dst>
  mason cp <src> <dst>
  mason rm <ref>
  mason disable <ref>
  mason enable <ref>

Share — maintenance
  mason prune
  mason sync

Deploy
  mason init
  mason deploy [target]
  mason status [target]

Setup / admin
  mason setup
  mason doctor
  mason remote-init
  mason target ls|add|use|rm
```

---

## Documentation chain

| Step | Document | Role |
|------|----------|------|
| 1 | [PROJECT_DESCRIPTION.md](./PROJECT_DESCRIPTION.md) | Motivation and original vision |
| 2 | [DESIGN.md](./DESIGN.md) | First-pass architecture — historical record |
| 3 | [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md) | Revised architecture |
| 4 | **COMMANDS.md** *(this file)* | Authoritative CLI specification |
| 5 | [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) | Implementation guide + work plan |

**Precedence for implementation:** [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md) → this file → [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md) → [DESIGN.md](./DESIGN.md) → [PROJECT_DESCRIPTION.md](./PROJECT_DESCRIPTION.md).

Implementation work is tracked with [contextq](https://github.com/norlinga/contextq) ([docs](https://contextq.longship.dev)). See `.cursor/skills/contextq/SKILL.md` for the agent coordination protocol.
