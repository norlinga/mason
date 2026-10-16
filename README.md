# Mason

A small Go CLI for publishing to a personal VPS behind Caddy. It **deploys** static
sites (rsync) and **shares** files (content-addressed blobs plus short links). Nothing
runs on the server: the server is just SSH, rsync, a filesystem layout, and Caddy.

## Requirements

- **This machine:** Go (to build), `ssh`, `rsync`.
- **The server:** SSH access as an admin (usually root) for the one-time bootstrap,
  `rsync`, and `caddy`.

```sh
go build -o mason ./cmd/mason   # or: go install ./cmd/mason, or: make build
```

## Quick start

```sh
mason setup example.com            # once per machine (bootstraps the server if needed)

cd ~/code/sites/blog.example.com
mason init                          # writes .mason (site URL + build folder), offers to provision
mason deploy                        # plan → progress → summary
```

That's it. Re-running any of these is safe.

### 1. Set up this machine: `mason setup <server>`

`setup` makes this machine able to publish to a server:

- If your existing SSH setup already reaches `mason@<server>` (an agent, `~/.ssh/config`,
  or `--key`), setup uses it.
- Otherwise it creates `~/.ssh/mason`, and after asking it bootstraps the server as an
  admin (`--admin`, default `root`). The bootstrap creates the unprivileged `mason`
  user, authorizes this machine's key (restricted to running commands), gives Caddy
  read access, and adds one `import` line to the Caddyfile.
- It checks the connection, then records the server in `~/.mason`.

```sh
mason setup example.com
mason setup example.com --share https://files.example.com   # also create your share store
mason setup example.com --print                               # just show the scripts
```

**A second machine.** If the new machine has no admin access, setup prints a line to run
on a machine that already publishes:

```sh
mason key add 'ssh-ed25519 AAAA… mason@laptop2'
mason key ls                     # which machines can publish
mason key rm mason@old-laptop    # revoke one
```

### 2. Add a site: `mason init`

Run `init` in the project folder. It suggests the URL from the folder name and detects
the build folder (`dist`, `public`, `build`, `_site`, `out`):

```yaml
# .mason
site: https://blog.example.com
build: dist
```

It then offers to **provision** the site: create its folder on the server and a
Mason-managed Caddy vhost. You can also run that step on its own:

```sh
mason provision            # asks, then runs as root over SSH
mason provision --print    # show the script instead
mason provision --spa      # single-page app: unknown paths fall back to /index.html
```

Apex domains (`example.com`) are served from the bare domain, with a permanent
`www` → apex redirect. Subdomains are served as-is. Caddy issues TLS for every name it
serves, so an apex domain needs DNS records for both `example.com` and
`www.example.com`. Every vhost returns 404 for dotfiles, except `/.well-known/`.
`provision` never overwrites a vhost you edited by hand (one without the
`# managed by mason` line) unless you pass `--force`; it shows the diff instead.

### 3. Deploy: `mason deploy [site]`

```sh
mason status               # what would change, nothing sent
mason deploy               # deploy the default site
mason deploy live          # another site in this project
mason deploy --delete      # also remove server files that are gone locally (asks first)
```

Dotfiles (`.git`, `.env`, `.mason`, …) are never deployed. Projects with several
environments list them under `sites:`, and the first one is the default. See
[CONFIG.md](./CONFIG.md).

## Sharing files

You need a share store first: `mason setup <server> --share https://share.example.com`.

```sh
mason share ./talk.mp3                          # → prints the long and short URLs
mason share ./talk.mp3 --as podcasts/ep-01.mp3  # choose the path
mason ls [prefix]                               # list shares
mason info podcasts/ep-01.mp3                   # one share in detail
mason url s/f76 [--long]                        # print one URL
```

A share is a **link**, not a file. Sharing the same bytes again only adds a link; nothing
is uploaded twice. Each share has two URLs, its path and its short slug (`s/<slug>`),
and you control them separately:

```sh
mason mv podcasts/ep-01.mp3 archive/ep-01.mp3   # move the path
mason cp podcasts/ep-01.mp3 archive/ep-01.mp3   # second copy: new path and new slug
mason disable s/f76 / mason enable s/f76        # turn one URL off and on
mason rm s/f76                                   # remove one URL (e.g. drop a guessable path, keep the slug)
mason prune                                      # delete unreferenced files (shows first; -y to delete)
mason sync                                       # refresh the local cache
```

## Checking things: `mason doctor`

`doctor` checks whatever is configured: the server (SSH, rsync, Caddy import), your
share store, and the current project's site (folder, managed vhost, Caddy, HTTP). It
exits non-zero if a check fails.

## Configuration

See **[CONFIG.md](./CONFIG.md)** for every option, how defaults are derived, and how to
migrate from v1. In short, `~/.mason` names your server and share store, and a project's
`.mason` names its site(s) and build folder.

## Global flags

`--json`, `-y/--yes` (answers confirmations), `-q/--quiet`, `-v/--verbose`,
`--config <project .mason>`. Plain output is columnar and greppable; `--json` is the
scripting form. Mason uses the system `ssh` for everything, so `~/.ssh/config`,
ssh-agent and known_hosts work as usual.
