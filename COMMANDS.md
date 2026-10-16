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

They share config, servers, and transport (SSH/rsync), but they are *not* the
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
| `--json` | Emit machine-readable output (for `jq`, scripting) |
| `-y, --yes` | Answer yes to confirmations (never changes *which* questions are asked) |
| `-q, --quiet` | Minimal output |
| `-v, --verbose` | Extra detail |
| `--config <path>` | Use this project `.mason` instead of searching upward |

`-n/--dry-run` belongs to `deploy` (where it equals `status`); `provision` and `setup`
use `--print` to show their scripts instead of running them.

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

- **No `--as`** → the file lands at `<inbox>/<filename>`, where `inbox` is the
  share store's optional `inbox:` setting in `~/.mason` (e.g. `inbox/`); without it,
  at `<filename>`. The inbox is just the implicit `--as`.
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
| `mason init [url]` | Write the project `.mason` (site URL + build folder; URL defaults to a domain-like folder name, build folder is detected), then offer to `provision`. |
| `mason deploy [site]` | rsync the build folder to the site's path, with a pre-flight plan, live progress, and a post-run summary. `[site]` is a name under `sites:` or a host; default is the first site. |
| `mason status [site]` | Show the deploy plan **without transferring**. Same as `deploy --dry-run`. |

Mirror-delete (removing remote files absent locally) is **opt-in** via
`--delete` and confirmed before running — the one genuine footgun. Dotfiles are
never deployed (except `/.well-known/`), and remote dotfiles are never deleted.
Before planning, deploy checks the site is provisioned and says `mason provision`
if not. `build: .` (the whole project) works but warns on every deploy.

Example:

```
$ mason deploy
Site:  https://blog.example.com → example.com:/var/www/blog.example.com
Build: dist
Plan:  12 new · 3 changed · 184 unchanged · 1 to remove   (28.4 MB to send)
Sending ████████████░░░░░░░░░░░░  50%  14.20M  6.20MB/s
✓ Done in 5.1s — 15 files updated, 28.4 MB sent, 1 removed
```

The progress line is live on a terminal; piped output gets a single `Sending …`;
`-v` prints rsync's raw lines. When nothing changed:

```
✓ Already up to date (199 files, 0 B sent)
```

---

## Setup / admin family

| Command | Behavior |
|---------|----------|
| `mason setup <server>` | Make this machine able to publish to a server: reuse what already works, else create `~/.ssh/mason` and (after asking) bootstrap the server as an admin; verify; record in `~/.mason`. `--share <url>` also provisions the share store. Replaces v1 `setup` + `remote-init`. |
| `mason provision [site]` | Create the site's folder and its Mason-managed Caddy vhost (named by host), validate, reload — as an admin, after asking. Replaces v1 `site-init`. |
| `mason key ls\|add\|rm` | Manage which machines may publish (the mason user's `authorized_keys`), no admin needed. |
| `mason doctor [site]` | Read-only checks for everything configured: server, share store, and (in a project) the site. Exits non-zero on failure. |

The admin commands (`setup` when bootstrapping, `provision`, `setup --share`) share one
model: Mason renders an idempotent POSIX `sh` script, shows what it will do, asks, and
runs it over SSH as the admin (`--admin`, default `root`; `--admin-key` to pick a key).
`--print` writes the script to stdout instead, for review or `| ssh root@host sh`.

### Server bootstrap (inside `mason setup`)

Once per server: the `mason` service user and group, this machine's key (with
`no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-user-rc`), Caddy added to
the mason group, and `import /etc/caddy/mason.d/*.caddy` appended to the Caddyfile once.
No vhosts, no site or share folders.

### Site provisioning (`mason provision`)

- creates the site's folder (`2750`, mason-owned, group-readable for Caddy),
- writes `/etc/caddy/mason.d/<host>.caddy`, starting with `# managed by mason`,
- `caddy validate`s, restoring the previous file on failure, then reloads (or restarts
  when Caddy's group membership changed).

**Apex domains vs subdomains** are auto-detected via the Public Suffix List (so
multi-label TLDs like `.co.uk` classify correctly): a **subdomain** gets one
`file_server` vhost; an **apex domain** is served from the bare apex with a permanent
`www` → apex redirect. Every site vhost 404s dotfile paths except `/.well-known/*`.
Caddy issues TLS for every name it serves, so an apex domain needs A/AAAA records for
both the apex and `www`; `mason doctor` warns when the redirect is missing.

A vhost without the `# managed by mason` line is treated as hand-edited: `provision`
prints the diff and changes nothing unless `--force` (which backs the original up).

Flags of note: `--spa` (or `spa: true` on the site) adds `try_files {path} /index.html`;
`--apex` / `--subdomain` override detection; `--no-www` drops the redirect;
`--no-reload` leaves validate/reload to you.

```sh
mason provision                 # default site, asks first
mason provision live --print    # review the script for another environment
```

---

## Configuration

Config is two small files — `~/.mason` (server, share store) and a project `.mason`
(site URL(s), build folder) — merged at load. **[CONFIG.md](./CONFIG.md)** is the
authoritative reference: every key, derivation rules, validation, and v1 migration.

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
  mason init [url]
  mason deploy [site] [--delete] [-n]
  mason status [site]

Setup / admin
  mason setup <server> [--share <url>] [--print]
  mason provision [site] [--print] [--force]
  mason key ls|add|rm
  mason doctor [site]
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
