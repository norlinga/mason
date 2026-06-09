---
name: trellis-go
description: Use with the trellis skill when authoring or reviewing Trellis .trellis sidecars for Go source files; provides Go-specific handle, anchor, and consumes guidance.
---

# Trellis For Go

This addendum is advisory.
The canonical Trellis skill owns handle semantics.

## Handle Shape

Prefer handles that match Go's package-oriented contract surface:

```trellis
Provides:
  - parser.ParseFile
  - graph.Build
  - lint.SourceAnchorShape
  - cli.NewRootCmd
```

Use exported names when they are the contract.
Unexported helpers usually do not deserve handles unless they are the meaningful unit of intent in this codebase.

## Source Anchors

Good anchors:

```trellis
Provides:
  - parser.ParseFile @source("symbol:ParseFile")
  - graph.Graph.ProvidersOf @source("symbol:ProvidersOf")
  - lint.SourceAnchorShape @source("symbol:SourceAnchorShape")
```

For methods, use the method name as the anchor unless the local convention needs receiver context:

```trellis
Provides:
  - graph.Graph.Dependents @source("symbol:Dependents")
```

## Consumes

List:

- internal cross-package functions or types whose contract matters
- external package APIs whose signatures/semantics shape this unit
- framework types accepted or returned by public functions

Usually omit:

- stdlib incidentals such as `fmt.Errorf`, `strings.TrimSpace`, `len`
- private helper calls inside the same file
- methods on a type already listed when the type is the real contract

## Common Mistakes

Avoid:

```trellis
Provides:
  - ParseFile
  - internal/parser/parser.go
  - parser.go.line42
```

Prefer:

```trellis
Provides:
  - parser.ParseFile @source("symbol:ParseFile")
```
