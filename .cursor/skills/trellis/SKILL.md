---
name: trellis
description: Use when creating, modifying, reviewing, or validating Trellis .trellis sidecar files; enforces Trellis-aware workflow gates, stable handles, graph checks, and linting discipline.
---

# Trellis Agent Skill

Trellis sidecars describe the contract of nearby source files.
A sidecar says what the source unit provides, what it consumes, what must remain true, what is out of scope, and which scenarios describe expected behavior.
The sidecar is the durable agreement; source code is the implementation of that agreement.

Use this canonical skill for Trellis rules that do not change by language.
For Mason's Go codebase, pair this skill with the `trellis-go` project skill.

## Recognize Trellis

A repository is Trellis-aware when either:

- it contains `.trellis` sidecars next to source files, or
- a `trellis` binary is available and the project expects Trellis checks.

Sidecars live beside source files by appending `.trellis` to the full source filename:

```text
src/payment_gateway.go
src/payment_gateway.go.trellis
```

Run Trellis commands from the application repository root unless the user says otherwise.

## Core Model

- `Provides:` names contracts this source unit owns.
- `Consumes:` names contracts whose changes would force this unit to change.
- `Invariants:` state what must remain true.
- `OutOfScope:` states what this unit deliberately does not do.
- `Scenario` blocks describe representative behavior.
- Capitalized RFC 2119 words such as `MUST`, `MUST NOT`, `SHALL`, `SHOULD`,
  and `MAY` are load-bearing when used in invariants and scenario steps.

## Stable Handles

A handle is the stable Trellis identity of a contract. It is the first
structured token in each `Provides:` or `Consumes:` entry:

```trellis
Provides:
  - Billing.Proration.calculate @source("line:42-68") -> Money

Consumes:
  - PaymentGateway.charge
```

The handles are `Billing.Proration.calculate` and `PaymentGateway.charge`.
The source anchor and description are not part of graph identity.

Handle rules:

- Handles are exact, case-sensitive graph keys.
- Do not normalize, lowercase, alias, or guess handles.
- Use dotted paths or typed prefixes such as `Event:subscription.created`.
- Keep handles globally unique in the workspace.
- Keep implementation locations out of handles; use `@source(...)`.
- Keep prose out of handles; prose belongs after the handle.
- Omit `Consumes:` when there are no meaningful contract dependencies.

Good handle test:

> If this behavior changed, would downstream code or reviewers need to know who
> consumes it?

Good stability test:

> If the implementation moved or the source symbol was renamed, would this
> handle still name the same contract?

## Source Anchors

Use source anchors for implementation locations:

```trellis
Provides:
  - Decision:ExampleWorkflow.rule @source("label:DECISION-PARAGRAPH")
  - Billing.Proration.calculate @source("line:42-68")
```

The handle says what the contract is.
The anchor says where it lives.

Prefer:

- `@source("label:<name>")` for labels, symbols, paragraphs, sections, and other named source locations.
- `@source("line:<n>")` or `@source("line:<start>-<end>")` for line anchors.

Do not invent an anchor if you are not confident.
A missing anchor is better than a misleading one.

## Consumes Discipline

A handle belongs in `Consumes:` only when a change to that contract's name, signature, semantics, or existence would force a corresponding change here.

List:

- internal contracts this unit truly depends on
- external library/framework APIs whose contract is load-bearing
- types, records, events, or data contracts that shape this unit's interface

Do not list:

- language built-ins
- incidental stdlib calls
- every method call on an already-modeled dependency
- placeholders such as `None`, `N/A`, or `Nothing external`
- framework plumbing unless it is intentionally modeled as a Trellis contract

When in doubt, prefer fewer entries with sharper meaning.

## Workflow Gates

Before creating a new source unit:

1. Search existing sidecars for similar `Provides:` handles.
2. If a plausible match exists, ask whether to extend it instead.
3. If creating a new unit, draft the sidecar before implementation.
4. Run `trellis lint` on the new or changed sidecar.

Before modifying an existing source unit:

1. Read its sidecar in full.
2. Stop and ask if the change crosses `OutOfScope:`.
3. Stop and ask if the change weakens an invariant.
4. If adding a new `Consumes:` handle, verify a provider exists or explicitly surface the new unresolved/external dependency.
5. After implementation, run `trellis lint <changed paths>`.
6. Update `@reviewed:` when the contract surface changed.

## Required Shape

A minimal sidecar:

```trellis
@owner: TeamName
@stability: stable
@since: 2026-01-01
@reviewed: 2026-05-25

Feature: Payment Gateway
  "Charges a payment token and returns a durable charge result."

  Provides:
    - PaymentGateway.charge @source("label:CHARGE")

  Invariants:
    - Charges MUST be idempotent for the same idempotency key

  Scenario (happy-path): Successful charge
    Given a valid payment token
    When the charge is submitted
    Then a charge result is returned

  Scenario (negative): Declined charge
    Given a declined payment token
    When the charge is submitted
    Then the failure is surfaced without creating a successful charge
```

## Common Mistakes

- Using source syntax as handles, such as `Foo::Bar#baz`, route paths, or paragraph-only names.
- Using broad handles such as `Rendered`, `GET`, `Create`, or `Handler`.
- Letting prose become the handle, such as `Rendered table wrapper`.
- Encoding source paths, line numbers, or labels into the handle.
- Listing every dependency touched by the code instead of contract-bearing dependencies.
- Writing sidecars for tests that merely realize scenarios from the source sidecar.
- Leaving empty blocks such as `OutOfScope:` with no entries.

## CLI Cheatsheet

```sh
trellis lint <paths>
trellis graph build <paths>
trellis graph deps <file.trellis>
trellis graph dependents <file.trellis>
trellis graph downstream <file.trellis>
trellis graph orphans <paths>
trellis locate <handle> [paths] --json
```

## Addendums

Language addendums give advisory handle patterns.
They do not override this canonical skill.

- `skill/addendums/trellis-for-go.md`
- `skill/addendums/trellis-for-rails.md`
- `skill/addendums/trellis-for-cobol.md`
- `skill/addendums/trellis-for-sql.md`
- `skill/addendums/trellis-for-java.md`
- `skill/addendums/trellis-for-python.md`
- `skill/addendums/trellis-for-dotnet.md`
- `skill/addendums/trellis-for-react.md`
- `skill/addendums/trellis-for-typescript-javascript.md`

When in doubt, follow the canonical rules first, then adapt names to the codebase's own language and conventions.
