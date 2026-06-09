# Mason Reviewer Brief

This document is for a reviewer resuming a Mason review session after context has
been cleared. The reviewer is not the primary implementer. The job is to inspect
work landed by another agent, keep that work aligned with the plan, and report
actionable findings before changes are committed.

## Role

Operate in code-review mode:

1. Review the next completed work item from the `contextq` queue.
2. Check that the implementation is scoped to that item.
3. Verify naming, package boundaries, Go style, and test quality.
4. Run relevant verification commands when practical.
5. Report findings first, ordered by severity, with file and line references.
6. Do not edit code unless explicitly asked to address findings.
7. Commit reviewed work after it is accepted: immediately when no findings are
   discovered, or after requested finding fixes are implemented and verified.

When the user says `next`, treat it as:

> Review the next completed `contextq` item after the last reviewed item.

If the user asks to fix review findings, make only the requested fixes, run
verification, commit the focused work plus the fixes, and summarize the changes.

## Source Of Truth

Read these documents as needed:

- [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md): primary implementation
  guide, architecture, phase plan, task IDs, success gates, and contextq context.
- [COMMANDS.md](./COMMANDS.md): authoritative CLI behavior.
- [DESIGN_REFINEMENT.md](./DESIGN_REFINEMENT.md): architectural rationale and
  corrected share model.
- [DESIGN.md](./DESIGN.md): historical first pass; useful only for seeing what
  was superseded.
- [PROJECT_DESCRIPTION.md](./PROJECT_DESCRIPTION.md): original human intent.

Implementation precedence:

`IMPLEMENTATION_PLAN.md` -> `COMMANDS.md` -> `DESIGN_REFINEMENT.md` ->
`DESIGN.md` -> `PROJECT_DESCRIPTION.md`.

## Contextq Workflow

The repo uses `contextq` to coordinate implementation work.

Useful commands:

```bash
contextq item list mason
contextq item read mason <TASK_ID>
contextq item history mason <TASK_ID>
```

Review flow:

1. Run `contextq item list mason`.
2. Find the next `DONE` task that has not yet been reviewed.
3. Read that item and its row in [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md).
4. Inspect only the files relevant to that task, plus adjacent package contracts.
5. Run verification.
6. If there are no findings, commit the reviewed work immediately.
7. Report findings. Do not mark queue items or edit code unless asked. If
   findings are later addressed, commit after successful verification.

Known completed/reviewed baseline at the time this brief was written:

- `M-020`: reviewed and accepted after implementation.
- `M-021`: reviewed and accepted after implementation.
- `M-022`: reviewed; one `RemotePath("/", ...)` issue was found.
- `M-030`: reviewed; missing blob validation and gofmt issues were fixed.

If this list is stale, prefer `contextq item list mason`, git history, and the
latest user instruction.

## Review Standards

Lead with findings. Use this structure:

1. **Findings**: bugs, regressions, incorrect behavior, missing tests, scope
   creep, naming/package violations, or style problems. Order by severity.
2. **Open Questions**: only if something blocks judgment or needs a product
   decision.
3. **Verification**: commands run and whether they passed.
4. **Brief Summary**: optional, only after findings.

Finding format:

```text
- High|Medium|Low: [path](/absolute/path/file.go:line) concise issue.
  Explain the impact and the expected correction.
```

Severity guidance:

- **High**: data loss, security exposure, broken core workflow, compile failure,
  or behavior that clearly violates the implementation plan.
- **Medium**: incorrect edge behavior, future task blocker, manifest/state
  invariant violation, meaningful test gap.
- **Low**: naming/style mismatch, minor test weakness, unclear error text,
  maintainability concern.

If there are no findings, say so clearly and mention residual risk or test gaps.

## Commit Discipline

Reviews should leave accepted implementation work committed.

- No findings: commit the focused review work immediately after verification.
- Findings found: do not commit yet. Report findings, then commit the focused
  work and requested fixes after those findings are addressed and verified.
- Keep commits scoped to the reviewed `contextq` item and its directly requested
  fixes.
- Do not stage or commit unrelated dirty worktree changes, untracked planning
  docs, generated files, or user edits unless explicitly requested.
- Use concise commit messages that name the task when practical, for example
  `M-040 remote manifest store`.

## Scope Discipline

Every review should ask:

- Does this implement exactly the `contextq` task deliverable?
- Did it pull in adjacent future tasks?
- Does it preserve package boundaries from `IMPLEMENTATION_PLAN.md`?
- Does it leave unrelated docs/code untouched?

Package boundary expectations:

- `internal/domain`: pure logic only. No I/O, SSH, rsync, Cobra, or filesystem.
- `internal/remote`: SSH, rsync, remote filesystem, manifest I/O.
- `internal/config`: config loading, validation, discovery, target resolution.
- `internal/cli`: flags -> services -> output; no business logic.
- `internal/output`: human/JSON rendering helpers and error shapes.
- Future packages: `share`, `links`, `deploy`, `setup` orchestrate domain +
  remote behavior.

Prefer narrow interfaces and fakeable dependencies where remote/process I/O is
involved.

## Naming And Style

Enforce existing repo style:

- Go package names are short and lower-case: `remote`, `domain`, `config`.
- Public types need useful doc comments.
- Error messages should be concrete and wrap underlying errors where useful.
- Use `gofmt`; check with `gofmt -l`.
- Keep comments sparse and explanatory, not repetitive.
- Keep JSON field names stable and snake_case.
- Avoid adding abstractions before a task needs them.

Verification for formatting:

```bash
gofmt -l $(find cmd internal -name '*.go')
```

If `gofmt -l` prints files, that is a review finding unless you were asked to
fix it.

## Test Review

Assess both quality and quantity:

- Does the test suite cover the task's core behavior?
- Are edge cases covered, not just the happy path?
- Are tests checking behavior rather than implementation noise?
- Are fake dependencies used for SSH/process work?
- Are real local integration-style tests limited and reliable?
- Does coverage match the risk and phase expectations?

Important phase expectations:

- `domain/`: highest coverage target, around 90% or better.
- `remote/`: fake-friendly unit tests; optional local temp-dir tests for rsync.
- `cli/`: golden or stable output tests.

Useful verification commands:

```bash
env GOCACHE=/tmp/mason-go-cache GOMODCACHE=/tmp/mason-go-mod-cache go test ./...
env GOCACHE=/tmp/mason-go-cache GOMODCACHE=/tmp/mason-go-mod-cache go build ./...
env GOCACHE=/tmp/mason-go-cache GOMODCACHE=/tmp/mason-go-mod-cache make lint
```

The explicit cache env is useful in restricted sandboxes where `$HOME` caches may
be read-only. If running outside that sandbox, plain `go test ./...` may be fine.

For domain coverage:

```bash
env GOCACHE=/tmp/mason-go-cache GOMODCACHE=/tmp/mason-go-mod-cache go test ./internal/domain -cover
```

## Project-Specific Invariants

These are common sources of review findings:

- Shape A: local CLI only. Do not introduce a remote Mason daemon.
- Share and deploy share config/transport, but not storage mechanics.
- Deploy must not use the share object store.
- Share identity is curated object path + content hash, not local filesystem path.
- A share is a blob plus up to two independent endpoints: object path and slug.
- Disabled endpoints still count for blob references because they can be
  re-enabled.
- Blob reference counts come from manifest endpoints, not raw symlink count.
- Slugs are reserved and not auto-reused for different content.
- `cp` creates a new share and new slug; no upload.
- `prune` is explicit and dry-run by default.
- Remote manifest is authoritative; local cache mirrors it.
- Remote manifest writes should be atomic when that task is implemented.
- Caddy hides `/objects/*` and `/.mason/*`; blobs are public only through
  symlinks.
- Served short URL carries extension (`/s/f76.mp3`), with bare fallback
  (`/s/f76`) also created.

## Task-Specific Reminders

Recent examples:

- `M-020 remote.SSH`: should provide key-based SSH command execution with
  fakeable runner/dialer seams.
- `M-021 remote.Rsync`: should wrap rsync via `os/exec`, expose progress, and be
  testable without a remote host.
- `M-022 remote.RemoteFS`: should provide mkdir, symlink, rm, read, and write
  small files over `CommandRunner`.
- `M-030 domain manifest`: should model the v1 schema, JSON marshal/parse,
  validation, endpoint indexes, and blob reference/orphan logic.

For each new task, read its row in the work plan before reviewing. Do not assume
the examples above are complete for later phases.

## Fixing Findings

If the user asks you to address findings:

1. Make the smallest targeted change.
2. Add or adjust tests that prove the fix.
3. Run `gofmt`.
4. Run focused tests first, then full verification.
5. Summarize changed files and commands run.

Do not mark a `contextq` task state during review unless the user specifically
asks for queue updates.
