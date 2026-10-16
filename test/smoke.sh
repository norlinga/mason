#!/usr/bin/env bash
#
# Mason end-to-end smoke test (manual; opt-in).
#
# Requires a configured share store — run `mason setup <server> --share <url>`
# first. It exercises the share MVP and lifecycle gates (G5/G7) against the real
# server: upload, link-only re-share, disable/enable (path and slug), cp, and prune.
#
# Usage:
#   ./test/smoke.sh                 # builds mason, uses the share store in ~/.mason
#   MASON=./mason ./test/smoke.sh   # use an existing binary
#
set -euo pipefail

MASON="${MASON:-}"

cleanup_bin=""
if [ -z "$MASON" ]; then
	echo "building mason…"
	cleanup_bin="$(mktemp -d)/mason"
	go build -o "$cleanup_bin" ./cmd/mason
	MASON="$cleanup_bin"
fi

mason() { "$MASON" "$@"; }
code() { curl -s -o /dev/null -w '%{http_code}' "$1"; }
slug_of() { mason info "$1" --json | grep -o '"slug": *"[^"]*"' | head -1 | sed -E 's/.*"slug": *"([^"]*)".*/\1/'; }
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1" >&2; exit 1; }

ts="$(date +%s)"
base="smoke/smoke-${ts}.txt"
copy="smoke/smoke-${ts}-copy.txt"
dup="smoke/smoke-${ts}-cp.txt"

tmpfile="$(mktemp)"
echo "mason smoke ${ts} $(head -c 16 /dev/urandom | base64)" > "$tmpfile"

created=()
cleanup() {
	set +e
	for ref in "${created[@]:-}"; do
		[ -n "$ref" ] || continue
		s="$(slug_of "$ref" 2>/dev/null)"
		mason rm "$ref" >/dev/null 2>&1
		[ -n "$s" ] && mason rm "s/$s" >/dev/null 2>&1
	done
	mason prune -y >/dev/null 2>&1
	rm -f "$tmpfile"
	[ -n "$cleanup_bin" ] && rm -f "$cleanup_bin"
}
trap cleanup EXIT

echo "== preflight: doctor =="
mason doctor

echo "== share (upload) =="
mason share "$tmpfile" --as "$base"
created+=("$base")
long="$(mason url "$base" --long)"
short="$(mason url "$base")"
[ "$(code "$short")" = "200" ] || fail "short URL did not serve (200)"
pass "uploaded blob serves over the short URL"

echo "== re-share same bytes to a new path (link-only) =="
mason share "$tmpfile" --as "$copy"
created+=("$copy")
[ "$(code "$(mason url "$copy")")" = "200" ] || fail "second path did not serve"
pass "same bytes served at a second path"

echo "== ls =="
mason ls smoke/

echo "== disable / enable (object-path endpoint) =="
mason disable "$base"
[ "$(code "$long")" = "404" ] || fail "disabled long URL still served"
mason enable "$base"
[ "$(code "$long")" = "200" ] || fail "re-enabled long URL did not serve"
pass "object-path endpoint toggles"

echo "== disable / enable (slug endpoint) =="
slug="$(slug_of "$base")"
[ -n "$slug" ] || fail "could not resolve slug"
mason disable "s/$slug"
[ "$(code "$short")" = "404" ] || fail "disabled slug still served"
mason enable "s/$slug"
[ "$(code "$short")" = "200" ] || fail "re-enabled slug did not serve"
pass "slug endpoint toggles"

echo "== cp (new slug, no upload) =="
mason cp "$base" "$dup"
created+=("$dup")
[ "$(code "$(mason url "$dup")")" = "200" ] || fail "cp short URL did not serve"
pass "cp created a working share"

echo "== prune (dry-run) =="
mason prune -n

echo
echo "ALL CHECKS PASSED"
