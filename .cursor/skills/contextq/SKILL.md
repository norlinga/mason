---
name: contextq
description: Use when coordinating local multi-agent work with contextq queues, including creating queues, reading queue context, pushing work keys, taking work only via item pop (FIFO), updating item state, and inspecting item history.
---

# contextq Agent Skill

`contextq` is an AI-agent-facing local coordination tool. Use it when multiple agents or scripts need to share work state through named queues, FIFO `item pop`, and durable item history.

Canonical upstream: [github.com/norlinga/contextq](https://github.com/norlinga/contextq) · [contextq.longship.dev](https://contextq.longship.dev)

## Mason project

This repo uses `contextq.config` with storage under `.contextq-data/`. Before implementing Mason work items, read the design chain: [IMPLEMENTATION_PLAN.md](../../../IMPLEMENTATION_PLAN.md) → [COMMANDS.md](../../../COMMANDS.md) → [DESIGN_REFINEMENT.md](../../../DESIGN_REFINEMENT.md).

## Core Rules

- **Take work only with `item pop`.** This is the only way to move an item from `AVAILABLE` to `CLAIMED`. Do not choose a key from `item list`, `item read`, or `queue read` counts and treat it as yours until `pop` succeeds.
- **`CLAIMED` is set by `pop`, not `update`.** `item update` accepts only `DONE`, `FAILED`, or `CANCELED`. Never try to pass `CLAIMED` to `update`.
- Read the queue before acting on it. Queue context is agent-facing task guidance.
- Read the queue again immediately before `item pop`. Other agents may have changed queue state since your last read.
- Update the item you popped as soon as its outcome is known: `DONE`, `FAILED`, or `CANCELED`.
- Use `--json` when parsing command output programmatically.
- Use stable string keys for work items, such as file paths, issue IDs, or task IDs.
- Do not push duplicate available work for the same key. Re-push a key only after its latest lifecycle is terminal.

## Taking Work From a Queue

| Do | Don't |
| --- | --- |
| `contextq item pop <queue>` | Pick a key from `item list` and start work |
| Use the `key` returned by `pop` | Assume a key from `queue read` counts is still available |
| Treat `item list` / `item read` as observation only | Set `CLAIMED` via `item update` |

`item list`, `item read`, and `queue read` may show available keys or backlog counts, but they do not reserve or claim work. Another agent may `pop` between your inspection and your next command.

## Setup

This project uses a project-local config so all agents share the same root:

```json
{
  "root": ".contextq-data"
}
```

If no config is present, `contextq` defaults to `./contextq/`.

## Queue Workflow

List queues:

```sh
contextq queue list
```

Create a queue with required agent context:

```sh
contextq queue create "Refactor these files carefully. Preserve public behavior and update tests." --name refactor
```

Read queue context and counts (informational — not for choosing which key to claim):

```sh
contextq queue read refactor
```

Use JSON for machine-readable queue details:

```sh
contextq --json queue read refactor
```

Destroy a queue only when intentionally cleaning up shared state:

```sh
contextq queue destroy refactor -f
```

## Item Workflow

Add work items by key:

```sh
contextq item push refactor internal/cli/root.go
contextq item push refactor internal/fsstore/store.go
```

Before `pop`, refresh queue context (not to pick a key):

```sh
contextq queue read refactor
```

**Take work** — claim the oldest available item atomically (only path to `CLAIMED`):

```sh
contextq item pop refactor
```

Read the latest lifecycle for a key (debugging or after `pop` — not a substitute for `pop`):

```sh
contextq item read refactor internal/cli/root.go
```

List items for **observation only** — do not use output to select work to claim:

```sh
contextq item list refactor
contextq item list refactor --state AVAILABLE
contextq item list refactor --key internal/cli/root.go
contextq item list refactor --limit 10
```

Update the active lifecycle for the key you **popped** (terminal states only):

```sh
contextq item update refactor internal/cli/root.go DONE
contextq item update refactor internal/cli/root.go FAILED
contextq item update refactor internal/cli/root.go CANCELED
```

Inspect all lifecycle events for a key:

```sh
contextq item history refactor internal/cli/root.go
```

## Recommended Agent Loop

1. Run `contextq queue list` to find the relevant queue.
2. Run `contextq queue read <queue>` and follow its context.
3. Optionally run `contextq item list <queue> --state AVAILABLE` to inspect rough backlog — **never claim from this output**.
4. Run `contextq queue read <queue>` again immediately before `pop` (refresh context, not to pick a key).
5. Run `contextq item pop <queue>` — **only** way to take work; work on the returned `key`.
6. Do the work for the returned key.
7. Run `contextq item update <queue> <key> DONE` when complete.
8. If blocked or unsuccessful, update the key to `FAILED` or `CANCELED` and leave useful notes in your final message or external task system.

## State Semantics

- `AVAILABLE`: work can be claimed **only** via `item pop`.
- `CLAIMED`: work is owned after a successful `pop` (not set via `update`).
- `DONE`: work completed successfully.
- `FAILED`: work was attempted but did not succeed.
- `CANCELED`: work was intentionally abandoned or invalidated.

Terminal states are `DONE`, `FAILED`, and `CANCELED`. To retry a terminal key, push the same key again; this creates a fresh lifecycle at the back of the queue.

## Error Handling

Expected failures are part of normal coordination:

- `duplicate_available_key`: another available lifecycle already exists for that key.
- `no_available_items`: there is currently nothing to `pop`.
- `queue_name_ambiguous`: use the queue UUID instead of the name.
- `invalid_state_transition`: update from the current lifecycle state is not allowed (e.g. passing `CLAIMED` to `update`).
- `no_active_lifecycle`: the key has no non-terminal lifecycle to update.

When automating, run with `--json` and branch on the `code` field.
