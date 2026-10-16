# Plan: Config v2 — servers, sites, and one share store

**Status:** Implemented and migrated (2026-10-15) on branch `config-v2`. Website (C9) not started.
**Breaking:** Yes, deliberately. No compatibility shim; old-format files fail with a
pointer to `CONFIG.md`. The 25 `sites/*/.mason` files and `~/.mason` are rewritten in
one pass (see [Migration](#migration)).

This plan resolves every finding from the 2026-10-15 UX review. Each change below
names the finding(s) it closes.

---

## Findings this plan closes

| # | Finding | Closed by |
|---|---------|-----------|
| F1 | Caddy snippets named by target name collide (`home`, `staging` reused across projects; `/etc/caddy/mason.d/staging.caddy` exists today) | Site identity = URL host |
| F2 | Local share cache keyed by target name | Cache keyed by share URL host |
| F3 | A project `.mason` shadows `~/.mason` entirely; `mason share` inside a site project wrote a share store into the site's web root (happened twice, 2026-10-03) | Share is configured only in `~/.mason`; project files cannot define one |
| F4 | `doctor` assumes every target is a share target | Role-aware doctor |
| F5 | Two SSH stacks: Go client ignores host keys, ssh-agent, passphrases, `~/.ssh/config`; rsync uses OpenSSH and may prompt on first deploy | One stack: system `ssh` |
| F6 | `root`/`base_url` required for deploy-only use; every project repeats host/user/root/remote_path | New schema; derived defaults |
| F7 | Onboarding is three commands with three ideas of a "target"; key install is indirect and silently skipped when `.pub` is missing | `mason setup <server>` |
| F8 | `init` / `setup` / `remote-init` / `site-init` naming; `deploy [target]` duplicates `-t`; `-y` also disables `setup` prompts; global `--dry-run` meaningless for most commands | Command surface cleanup |
| F9 | Deploy shows a static `Sending …` unless `-v` (spec promises live progress) | Live progress on a TTY |
| F10 | `build_dir: ./` publishes the whole project (e.g. `node_modules/`) | Build-dir detection + warning |
| F11 | Existing Caddy vhosts lack the dotfile 404 (fixed in code for new `site-init` output only) | Provisioning rewrites vhosts it owns; one-time hardening script for today |

Already fixed on branch `fix/deploy-dotfiles-and-plan-stats` (not part of this plan):
dotfile exclusion on deploy, dotfile 404 in new static vhosts, deploy plan stat
parsing (directory counts, thousands separators, human-readable sizes).

---

## Mental model

Four ideas, nothing else:

- **Server** — a machine Mason reaches over SSH. Just an SSH destination
  (`example.com`, `mason@example.com:2222`, or a `~/.ssh/config` alias). User defaults
  to `mason`. There is no server registry: a destination string is the whole definition.
- **Site** — a public URL whose files come from a project folder. Needs a URL and a
  build folder; everything else is derived. Its **identity is its URL host**.
- **Environments** — a project may define several sites (`staging`, `live`).
- **Share store** — one personal drop box, defined once per machine in `~/.mason`.

---

## Schema

### `~/.mason` (per machine)

```yaml
server: example.com                 # default server for sites and share
share: https://files.example.com   # optional; omit to disable share commands
```

Full form, every field optional except where noted:

```yaml
server: example.com                 # SSH destination; user defaults to "mason"
key: ~/.ssh/mason                    # optional; else ssh-agent / ~/.ssh/config decide
share:
  url: https://files.example.com   # required if share is used
  path: /var/www/share               # default /var/www/<url host>
  inbox: inbox/                      # default path for `mason share` without --as (was default_path)
  server: example.com               # default: top-level server
```

### Project `.mason`

Single site (the common case — 22 of 25 current projects):

```yaml
site: https://recipes.example.com
build: dist
```

Several environments — the first one listed is the default:

```yaml
build: dist
sites:
  staging: https://staging.shop.example.com
  live: https://shop.example.com
```

Per-site overrides when a default is wrong:

```yaml
site:
  url: https://family.example.com
  path: /var/www/family      # default /var/www/<url host>
  server: family.example.com
  spa: false                          # provisioning option, recorded so re-provisioning is reproducible
```

### Derivation rules

| Value | Default |
|-------|---------|
| SSH user | `mason` (unless the destination says `user@`) |
| SSH port / key / host keys / proxies | OpenSSH: destination, then `~/.ssh/config`, then agent |
| Site path on server | `/var/www/<url host>` |
| Site name (for `deploy <name>`) | key under `sites:`, or the URL host for `site:` |
| Caddy snippet | `/etc/caddy/mason.d/<url host>.caddy` |
| Share cache | `$XDG_CACHE_HOME/mason/<share url host>/` |
| Server for a site | site `server:` → project `server:` → `~/.mason` `server:` |

### Layering

Project and user files are **merged, not shadowed**. A project file may set `server`,
`key`, `site`/`sites`, and `build`. It may **not** set `share` — a project file with a
`share:` key is an error. Share commands read `~/.mason` regardless of the cwd, so a share
can never land in a site's web root (F3).

### Validation

- Unknown keys are errors (as today), with a hint naming the closest valid key.
- An old-format file (`targets:` / `default:` keys) fails with:
  `error: ~/code/.../.mason uses the pre-v2 format; see CONFIG.md#migrating-from-v1`.
- `build` must exist and be a directory at deploy time. `build: .` (or `./`) is allowed but
  warns once per deploy: `deploying the whole project folder — set build: to your output dir`.
- Two sites in one project may not share a URL host.

---

## SSH: one stack (F5)

Replace the Go `x/crypto/ssh` client with the system `ssh` binary for remote commands,
the same binary rsync already uses. Gains: `~/.ssh/config` aliases/ports/ProxyJump,
ssh-agent and passphrase-protected keys, and real host-key verification — all identical
between `mason ls` and `mason deploy`.

- Non-interactive commands run `ssh -o BatchMode=yes` so nothing hangs on a prompt;
  connection failures map to actionable errors (`host key unknown — run mason setup
  example.com`, `permission denied (publickey) — is this machine's key authorized?`).
- `mason setup` is the one place that accepts a new host key (`StrictHostKeyChecking=accept-new`),
  printing the fingerprint.
- Connection reuse: `ControlMaster=auto`, `ControlPersist=60s`, socket under the Mason
  cache dir, so multi-call commands (`share`, `doctor`) cost one handshake.
- `remote.CommandRunner` stays the seam; tests keep their fakes. `golang.org/x/crypto`
  drops out of `go.mod`.

---

## Onboarding (F7)

```sh
mason setup example.com
```

One idempotent command for "this machine ↔ this server":

1. **Key.** Use `key:` if configured; else if `ssh mason@host true` already works
   (agent/config), keep that; else generate `~/.ssh/mason` (ed25519, comment
   `mason@<hostname>` so `authorized_keys` lines identify machines; passphrase prompted,
   empty allowed).
2. **Server bootstrap** (only if step 3 fails or `--bootstrap` is passed). Connects as an
   admin (`--admin root` default; uses your normal SSH identity), shows the bootstrap
   script, asks to apply (`-y` skips the question), then runs it: mason user/group, this
   machine's key, Caddy group, `import mason.d/*.caddy`. `--print` emits the script and
   exits, for review or `| ssh root@host sh`.
3. **Verify** as `mason@host`.
4. **Write** `~/.mason` (`server:`), merging with any existing file.
5. Optional share store: `mason setup example.com --share https://files.example.com`
   provisions the share tree + vhost (today's `remote-init` share half) and records `share:`.

Second machine, no admin access from it: `mason setup` prints the exact line to run
from a machine that already has access:

```sh
mason key add 'ssh-ed25519 AAAA… mason@laptop2'
```

`mason key ls` / `mason key rm <comment>` manage `authorized_keys` entries by machine.

---

## Command surface (F8, F9, F10)

| v1 | v2 | Notes |
|----|----|-------|
| `setup` + `remote-init` | `setup <server> [--share URL]` | One onboarding command (above) |
| `init` | `init` | Writes the short project file: prompts for URL, detects build dir (`dist`, `public`, `build`, `_site`, `out`), then offers to provision |
| `site-init` | `provision [site]` | Same script/print/apply model; snippet named by URL host; records `spa:` |
| `deploy [target]`, `status [target]` | `deploy [site]`, `status [site]` | Positional site name; `-t` removed |
| `target ls/add/use/rm` | removed | Nothing to manage: sites live in projects, server is one line |
| `doctor` | `doctor` | Role-aware (F4): server checks always; share checks if `share` set; site checks inside a project |
| — | `key add/ls/rm` | Per-machine authorized keys |
| share family | unchanged | `share ls info url mv cp rm disable enable prune sync` |

Flags:
- `-y` only answers confirmations; it never changes which questions are asked.
- `-n/--dry-run` moves from global to the commands where it means something
  (`deploy`, `provision`, `setup`, `rm`, `mv`, `cp`). `prune` stays dry-run by default.
- `--json`, `-q`, `-v`, `--config` stay global.

Deploy output (F9): on a TTY, a single live line driven by rsync `--info=progress2`
(`Sending ████████░░ 18.1/28.4 MB  6.2 MB/s`); plain lines when not a TTY; `-v` adds
per-file names.

Deploy preflight: if the site's path does not exist on the server, fail with
`https://x.example.com is not provisioned on example.com — run: mason provision`.

---

## Provisioning and existing vhosts (F1, F11)

- `provision` writes `/etc/caddy/mason.d/<url host>.caddy`, including the dotfile 404
  and, for apex domains, the www redirect. Re-running regenerates the file — the snippet is
  Mason-owned. A hand-edited snippet is detected (marker comment `# managed by mason` absent)
  and left alone unless `--force`, with a diff shown.
- The share store vhost keeps its private-prefix 404 (`/objects/*`, `/.mason/*`). Share
  paths may legitimately be dotfiles, so it does not get the general dotfile rule.
- **Today**, before v2 ships: a one-time script adds the dotfile 404 to every existing
  static vhost in `mason.d` (backup, `caddy validate`, restore on failure, reload).

---

## Migration

1. **Server, once:** rename `mason.d/staging.caddy` → `staging.app.example.com.caddy`;
   add `# managed by mason` markers to snippets that match the generated shape.
2. **Local, once:** a throwaway script (not shipped) rewrites `~/.mason` and each
   `sites/*/.mason` per the mapping below, prints a diff per file, and writes only on
   confirmation. Unmappable files are listed, not guessed.

| v1 | v2 |
|----|----|
| `host` + `user` + `port` | `server:` (omitted when equal to the user default) |
| `key_path` | `key:` (omitted when it is the OpenSSH default) |
| `base_url` (site project) | `site:` URL |
| `deploy.build_dir` | `build:` (leading `./` dropped) |
| `deploy.remote_path` / `root` | dropped when equal to `/var/www/<host>`, else `path:` |
| several targets | `sites:` map, default target listed first |
| `root` + `base_url` in `~/.mason` | `share: {url, path}` |
| `share.default_path` | `share.inbox` |

3. **Other machine:** clone, build, read `CONFIG.md#migrating-from-v1`, run `mason setup
   example.com` (authorizes that machine's key and writes its `~/.mason`).

---

## Documentation

- **`CONFIG.md`** (new): the complete reference — every key, its default, derivation
  rules, layering, examples for single/multi-site/override/share, and a v1 → v2
  migration section. `COMMANDS.md` and `README.md` link to it rather than restating it.
- `COMMANDS.md`, `README.md`: updated command surface.
- The Mason website: config reference page generated from the same content; getting
  started rewritten around `mason setup` → `mason init` → `mason deploy`.
- `DESIGN*.md`, `IMPLEMENTATION_PLAN.md`: historical; add a one-line pointer to this plan.

---

## Work breakdown

| Step | Scope | Tests |
|------|-------|-------|
| C1 | `internal/config`: v2 schema, layering/merge, derivation, validation, old-format error | table tests per rule; fixtures from the 25 real files |
| C2 | `internal/remote`: system-ssh `CommandRunner`, ControlMaster, error mapping; delete Go SSH client | fake exec runner; one opt-in live test |
| C3 | Site identity: snippet, cache, and name derived from URL host; `provision` replaces `site-init` | script golden tests (existing style) |
| C4 | `setup <server>` + `key add/ls/rm`; bootstrap script split from share provisioning (per PLAN_INIT_SPLIT.md) | injected prompter/keygen/runner |
| C5 | Command surface: `init` rewrite, `deploy`/`status` positional, flag moves, remove `target`/`remote-init`/`site-init` | CLI tests |
| C6 | Role-aware `doctor`; deploy preflight; TTY progress; `build: .` warning | CLI tests |
| C7 | `CONFIG.md`, `COMMANDS.md`, `README.md`; Trellis sidecars for every touched unit | `trellis lint` clean |
| C8 | Migration script run against `sites/` and `~/.mason`; server snippet rename | dry-run diff reviewed before write |
| C9 | Website updates | site build |

---

## Decisions log

- **`rm` semantics — working as intended.** `rm` removes one endpoint so a guessable
  object-path URL can be dropped while a less guessable slug stays live. A whole-share
  removal may come later as a separate verb (`rm-all` / `rm-only`); out of scope here.
- **A backup project sharing a live site's web root** — kept as-is (no resolving domain of
  its own); its project file is migrated like any other. Deploying it overwrites the live site.
- **A site whose domain lapsed** — no action; its vhost and project are migrated as-is.
- **Server hardening done 2026-10-15** — leaked dotfiles removed, the two stray share stores
  inside site web roots removed, dotfile 404 live on all 23 static vhosts (F11's "today" step
  complete).

- **The one project with its own `host`** turned out to be another DNS name for the same
  server; its project file uses the default server and keeps only its custom `path:`.
- **Migration done 2026-10-15** — server snippets renamed by host and marked managed
  (one vhost with a custom `header` line left hand-edited); `~/.mason` and all 25
  `sites/*/.mason` rewritten to v2 (v1 copy at `~/.mason.v1.bak`); `mason status` verified in
  every project.
