# Mason configuration

Mason reads two files, both named `.mason`:

| File | Where | Says |
|------|-------|------|
| **User file** | `~/.mason` | which server this machine publishes to, and your share store |
| **Project file** | a site's project folder | the site's URL and which folder to deploy |

Mason merges them: a project inherits the server from `~/.mason`. Mason finds the
project file by walking up from the current folder, so every command works from any
subfolder of a project. `--config <path>` names a project file explicitly.

Most setups need four lines in total:

```yaml
# ~/.mason
server: example.com
share: https://files.example.com
```

```yaml
# ~/code/blog/.mason
site: https://blog.example.com
build: dist
```

You rarely write either file by hand: `mason setup <server>` writes `~/.mason`, and
`mason init` writes the project file.

---

## The model

- **Server**: a machine Mason reaches over SSH. Mason has no list of servers. A
  server is just an SSH destination, and the user defaults to `mason`.
- **Site**: a public URL whose files come from a project folder. Its URL host is its
  identity. Mason uses it for the site's folder on the server, its Caddy file and its name.
- **Environments**: a project can have several sites, such as `staging` and `live`.
- **Share store**: one personal drop box for `mason share`. It is defined once, in
  `~/.mason`, and never in a project.

---

## User file (`~/.mason`)

| Key | Default | Meaning |
|-----|---------|---------|
| `server` | — | Default server for sites and the share store. `host`, `user@host`, `host:port`, `[ipv6]:port`, or a `~/.ssh/config` alias. The user defaults to `mason`. |
| `key` | — | Private key to use. When unset, OpenSSH decides, using ssh-agent and `~/.ssh/config`. |
| `share` | — | Your share store: a URL, or a mapping (below). Without it, the share commands explain how to add one. |

`share` as a mapping:

| Key | Default | Meaning |
|-----|---------|---------|
| `url` | required | Public URL of the store, `https://` assumed. Must be just scheme and host. |
| `path` | `/var/www/<url host>` | Folder on the server. |
| `inbox` | — | Folder that `mason share` uses when you don't pass `--as`. |
| `server` | top-level `server` | Server holding the store. |

```yaml
server: example.com
key: ~/.ssh/mason
share:
  url: https://files.example.com
  path: /var/www/files
  inbox: inbox/
```

The user file may not contain `site`, `sites` or `build`, because those belong to projects.

## Project file (`.mason`)

| Key | Default | Meaning |
|-----|---------|---------|
| `site` | — | The site: a URL, or a mapping (below). |
| `sites` | — | Several sites, written as `name: URL` or `name: {mapping}`. **The first one is the default.** Use either `site` or `sites`. |
| `build` | — | Folder to deploy, relative to the project file. Dotfiles are never deployed, except `/.well-known/`. |
| `server` | from `~/.mason` | Server for this project's sites. |
| `key` | from `~/.mason` | Key for this project's server. |

A site as a mapping:

| Key | Default | Meaning |
|-----|---------|---------|
| `url` | required | Public URL, `https://` assumed. Must be just scheme and host, because a site is served from the root of its host. |
| `path` | `/var/www/<url host>` | Folder on the server. |
| `server` | project `server`, else `~/.mason` | Server for this site only. |
| `spa` | `false` | Single-page app: `mason provision` falls back to `/index.html` when a path doesn't match a file. |

A project file may not contain `share`, because there is one share store per machine.

### Examples

One site (the common case):

```yaml
site: https://blog.example.com
build: dist
```

Staging and live. `mason deploy` goes to staging, and `mason deploy live` goes live:

```yaml
build: dist
sites:
  staging: https://staging.shop.example.com
  live: https://shop.example.com
```

A site whose server folder doesn't match its host:

```yaml
build: .
site:
  url: https://family.example.com
  path: /var/www/family
```

A site on a different server than the one in `~/.mason`:

```yaml
site:
  url: https://example.org
  server: deploy@other.example.net:2222
build: dist
```

---

## How values are derived

| Value | Rule |
|-------|------|
| SSH user | `mason`, unless the destination says `user@` |
| SSH port, key, host keys, proxies | OpenSSH: the destination first, then `~/.ssh/config`, then ssh-agent |
| Site or share folder on the server | `/var/www/<url host>` |
| Site name (`mason deploy <name>`) | its key under `sites:`, or its URL host. You can also pass the host or URL. |
| Caddy file | `/etc/caddy/mason.d/<url host>.caddy` |
| Server for a site | the site's `server`, then the project's `server`, then `~/.mason` |
| Local share cache | `$XDG_CACHE_HOME/mason/<share host>/` |

## Validation

Mason rejects these, naming the file, the line, and usually the fix:

- an unknown key, with the nearest valid key suggested, or the v2 name if it's a v1 key;
- a duplicate key; both `site` and `sites` in one file; two sites with the same host;
- a URL with a path (`https://example.com/blog`); a URL given where a server should be;
- `share` in a project file, or `site`, `sites` or `build` in `~/.mason`;
- a file in the v1 format (see below).

Missing pieces are not load errors. A command that needs a site, server or share store
explains what to add when it runs.

---

## Migrating from v1

v1 files have `default:` and `targets:` at the top. Mason v2 rejects them with:

```
error: …/.mason uses the pre-v2 config format (default:/targets:); see CONFIG.md#migrating-from-v1
```

### On a machine that already publishes

1. Clone and build Mason v2.
2. Run `mason setup <server>`, for example `mason setup example.com`. Setup moves your v1
   `~/.mason` to `~/.mason.v1.bak`, checks that the connection still works, and
   writes a v2 `~/.mason`. Add your share store with `--share <url>`, plus
   `--share-path` if its folder isn't `/var/www/<host>`. If the old file had a
   `key_path`, pass it as `--key` so setup reuses that key instead of creating a new one.
3. Rewrite each project's `.mason` using the table below. Most become two lines.

### Field mapping

| v1 | v2 |
|----|----|
| `default:` + `targets:` | removed. One target becomes `site:`; several become `sites:`, with the old default listed first |
| `host`, `user`, `port` | `server: [user@]host[:port]`. Leave it out when it matches `~/.mason`; the user defaults to `mason` |
| `key_path` | `key:` (in `~/.mason`) |
| `base_url` of a site target | `site:` URL |
| `deploy.build_dir` | `build:`, with any leading `./` dropped |
| `deploy.remote_path` / `root` of a site target | removed when it equals `/var/www/<url host>`, otherwise `path:` |
| `root` + `base_url` of the share target | `share:` in `~/.mason`, with `url` and `path` |
| `share.default_path` | `share.inbox` |

Before:

```yaml
default: staging
targets:
  staging:
    host: example.com
    user: mason
    root: /var/www/staging.shop.example.com
    base_url: https://staging.shop.example.com
    deploy:
      build_dir: ./dist
      remote_path: /var/www/staging.shop.example.com
  live:
    host: example.com
    user: mason
    root: /var/www/shop.example.com
    base_url: https://shop.example.com
    deploy:
      build_dir: ./dist
      remote_path: /var/www/shop.example.com
```

After:

```yaml
build: dist
sites:
  staging: https://staging.shop.example.com
  live: https://shop.example.com
```

### Server-side changes

- **Caddy files are now named by host.** A site provisioned under its v1 target name
  (for example `mason.d/staging.caddy`) should be renamed to `<host>.caddy`. Otherwise
  `mason provision` writes a second file for the same host and `caddy validate` fails
  with a duplicate-site error, which leaves the server unchanged.
- **Mason-managed snippets start with `# managed by mason`.** `mason provision` replaces
  a snippet only if it has that line, and shows you the diff otherwise. Add the line
  to snippets you're happy for Mason to own, or run `mason provision --force` once;
  the original is backed up.
- **Commands that went away:** `remote-init` became `mason setup`, `site-init` became
  `mason provision`, and `target` was removed. The global `-t <name>` became
  `mason deploy <site>` and `mason status <site>`.
