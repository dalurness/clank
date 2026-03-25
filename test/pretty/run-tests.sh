#!/usr/bin/env bash
# Pretty-print / terse transformation test suite
# Tests: qualified expansion, unqualified expansion, import expansion,
#        round-trip, comments/strings preserved, CLI modes
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"
CLANK="npx tsx $ROOT/src/main.ts"
PASS=0
FAIL=0

fail() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }
pass() { echo "PASS: $1"; PASS=$((PASS + 1)); }

# Helper: assert stdin pretty output matches expected
assert_pretty() {
  local label="$1" input="$2" expected="$3"
  local actual
  actual=$(echo "$input" | $CLANK pretty --stdin 2>/dev/null)
  if [ "$actual" = "$expected" ]; then
    pass "$label"
  else
    fail "$label"
    echo "  expected: $expected"
    echo "  actual:   $actual"
  fi
}

assert_terse() {
  local label="$1" input="$2" expected="$3"
  local actual
  actual=$(echo "$input" | $CLANK terse --stdin 2>/dev/null)
  if [ "$actual" = "$expected" ]; then
    pass "$label"
  else
    fail "$label"
    echo "  expected: $expected"
    echo "  actual:   $actual"
  fi
}

# ── Test 1: Qualified string functions ──
assert_pretty "qualified str.slc" \
  'str.slc(s, 0, 5)' \
  'string.slice(s, 0, 5)'

assert_pretty "qualified str.has" \
  'str.has(s, "hello")' \
  'string.contains(s, "hello")'

assert_pretty "qualified str.pfx" \
  'str.pfx(s, "http")' \
  'string.starts-with(s, "http")'

# ── Test 2: Qualified filesystem functions ──
assert_pretty "qualified fs.read" \
  'fs.read("file.txt")' \
  'filesystem.read("file.txt")'

assert_pretty "qualified fs.mkdir" \
  'fs.mkdir("/tmp/test")' \
  'filesystem.make-directory("/tmp/test")'

# ── Test 3: Qualified collection functions ──
assert_pretty "qualified col.sortby" \
  'col.sortby(xs, fn(a, b) => cmp(a.name, b.name))' \
  'collection.sort-by(xs, fn(a, b) => compare(a.name, b.name))'

assert_pretty "qualified col.group" \
  'col.group(xs, fn(x) => x)' \
  'collection.group-by(xs, fn(x) => x)'

# ── Test 4: Qualified map/set functions ──
assert_pretty "qualified map.del" \
  'map.del(m, "key")' \
  'map.delete(m, "key")'

assert_pretty "qualified set.inter" \
  'set.inter(a, b)' \
  'set.intersection(a, b)'

# ── Test 5: Qualified server functions ──
assert_pretty "qualified srv.mw" \
  'srv.mw(handler)' \
  'server.middleware(handler)'

assert_pretty "qualified srv.res" \
  'srv.res(200, "ok")' \
  'server.response(200, "ok")'

# ── Test 6: Qualified error/process/env functions ──
assert_pretty "qualified err.wrap" \
  'err.wrap(e, "failed")' \
  'error.wrap(e, "failed")'

assert_pretty "qualified proc.bg" \
  'proc.bg(cmd)' \
  'process.background(cmd)'

assert_pretty "qualified env.get" \
  'env.get("HOME")' \
  'environment.get("HOME")'

# ── Test 7: Qualified datetime functions ──
assert_pretty "qualified dt.fmt" \
  'dt.fmt(t, "YYYY-MM-DD")' \
  'datetime.format(t, "YYYY-MM-DD")'

# ── Test 8: Qualified regex/csv functions ──
assert_pretty "qualified rx.rep" \
  'rx.rep(s, pattern, replacement)' \
  'regex.replace(s, pattern, replacement)'

assert_pretty "qualified csv.dec" \
  'csv.dec(raw)' \
  'csv.decode(raw)'

# ── Test 9: Unqualified built-in expansions ──
assert_pretty "unqualified len" \
  'let n = len(xs)' \
  'let n = length(xs)'

assert_pretty "unqualified cat" \
  'let combined = cat(a, b)' \
  'let combined = concatenate(a, b)'

assert_pretty "unqualified cmp" \
  'cmp(a, b)' \
  'compare(a, b)'

assert_pretty "unqualified some?" \
  'some?(x)' \
  'is-some(x)'

assert_pretty "unqualified ok?" \
  'ok?(result)' \
  'is-ok(result)'

assert_pretty "unqualified eq/neq" \
  'if eq(a, b) then neq(c, d) else 0' \
  'if equal(a, b) then not-equal(c, d) else 0'

# ── Test 10: Import statement expansion ──
assert_pretty "import std.str" \
  'use std.str (slc, pfx, cat)' \
  'use std.string (slice, starts-with, concatenate)'

assert_pretty "import std.fs" \
  'use std.fs (read, rm, mv)' \
  'use std.filesystem (read, remove, move)'

assert_pretty "import std.json" \
  'use std.json (dec, merge)' \
  'use std.json (decode, merge)'

# ── Test 11: Module that doesn't change ──
assert_pretty "import no-change" \
  'use std.http (get, post)' \
  'use std.http (get, post)'

# ── Test 12: Bare module import (no parens) ──
assert_pretty "bare import std.srv" \
  'use std.srv' \
  'use std.server'

assert_pretty "bare import std.dt" \
  'use std.dt' \
  'use std.datetime'

# ── Test 13: Terse direction — verbose → terse ──
assert_terse "terse string.slice" \
  'string.slice(s, 0, 5)' \
  'str.slc(s, 0, 5)'

assert_terse "terse filesystem.read" \
  'filesystem.read("file.txt")' \
  'fs.read("file.txt")'

assert_terse "terse collection.sort-by" \
  'collection.sort-by(xs, cmp)' \
  'col.sortby(xs, cmp)'

assert_terse "terse length" \
  'let n = length(xs)' \
  'let n = len(xs)'

assert_terse "terse import" \
  'use std.string (slice, starts-with)' \
  'use std.str (slc, pfx)'

assert_terse "terse server.middleware" \
  'server.middleware(handler)' \
  'srv.mw(handler)'

# ── Test 14: Round-trip: pretty then terse ──
ROUNDTRIP_INPUT='use std.str (slc, pfx, cat)
use std.fs (read, rm)

status-freq : (path: Str) -> <io, exn> Map<Str, Int> =
  path |> fs.read |> str.lines |> map(fn(line) => split(line, " ") |> col.nth(8))
       |> filter(fn(x) => some?(x)) |> map(unwrap) |> col.group(fn(x) => x)'

roundtrip=$(echo "$ROUNDTRIP_INPUT" | $CLANK pretty --stdin 2>/dev/null | $CLANK terse --stdin 2>/dev/null)
if [ "$roundtrip" = "$ROUNDTRIP_INPUT" ]; then
  pass "round-trip: pretty then terse"
else
  fail "round-trip: pretty then terse"
fi

# ── Test 15: Round-trip: terse then pretty ──
VERBOSE_INPUT='use std.string (slice, starts-with, concatenate)
use std.filesystem (read, remove)

let n = length(xs)'

roundtrip2=$(echo "$VERBOSE_INPUT" | $CLANK terse --stdin 2>/dev/null | $CLANK pretty --stdin 2>/dev/null)
if [ "$roundtrip2" = "$VERBOSE_INPUT" ]; then
  pass "round-trip: terse then pretty"
else
  fail "round-trip: terse then pretty"
fi

# ── Test 16: Comments are preserved (not transformed) ──
assert_pretty "comments preserved" \
  '# str.slc should not change in comments
str.slc(s, 0, 5)' \
  '# str.slc should not change in comments
string.slice(s, 0, 5)'

# ── Test 17: String contents are not transformed ──
assert_pretty "strings preserved" \
  'print("str.slc is the terse form")' \
  'print("str.slc is the terse form")'

# ── Test 18: User-defined identifiers are not transformed ──
assert_pretty "user idents preserved" \
  'let my-len = 42' \
  'let my-len = 42'

# ── Test 19: Keywords are not transformed ──
assert_pretty "keywords preserved" \
  'let x = fn(a) => if a then match a { _ => 0 } else 1' \
  'let x = fn(a) => if a then match a { _ => 0 } else 1'

# ── Test 20: Already-readable names pass through ──
assert_pretty "readable pass-through" \
  'map(xs, fn(x) => filter(ys, fn(y) => y > 0))' \
  'map(xs, fn(x) => filter(ys, fn(y) => y > 0))'

# ── Test 21: JSON output mode ──
json_out=$(echo 'len(xs)' | $CLANK pretty --stdin --json 2>/dev/null)
if echo "$json_out" | grep -q '"ok":true' && echo "$json_out" | grep -q '"transformations":1' && echo "$json_out" | grep -q '"direction":"pretty"'; then
  pass "--json output"
else
  fail "--json output"
  echo "  got: $json_out"
fi

# ── Test 22: File mode with --write ──
tmp=$(mktemp /tmp/clank-pretty-test-XXXXX.clk)
echo 'str.slc(s, 0, 5)' > "$tmp"
$CLANK pretty --write "$tmp" >/dev/null 2>&1
content=$(cat "$tmp")
if [ "$content" = 'string.slice(s, 0, 5)' ]; then
  pass "--write mode"
else
  fail "--write mode"
fi
rm "$tmp"

# ── Test 23: File mode with --diff ──
tmp=$(mktemp /tmp/clank-pretty-test-XXXXX.clk)
echo 'str.slc(s, 0, 5)' > "$tmp"
diffout=$($CLANK pretty --diff "$tmp" 2>/dev/null)
if echo "$diffout" | grep -q '+string.slice'; then
  pass "--diff mode"
else
  fail "--diff mode"
fi
rm "$tmp"

# ── Test 24: No input file error ──
if $CLANK pretty >/dev/null 2>&1; then
  fail "no-file error"
else
  pass "no-file error"
fi

# ── Test 25: Mixed qualified and unqualified in one line ──
assert_pretty "mixed line" \
  'let result = col.sortby(xs, fn(a, b) => cmp(a, b))' \
  'let result = collection.sort-by(xs, fn(a, b) => compare(a, b))'

# ── Test 26: http.del expansion ──
assert_pretty "http.del" \
  'http.del(url)' \
  'http.delete(url)'

# ── Test 27: env.get! with bang ──
assert_pretty "env.get! with bang" \
  'env.get!("PATH")' \
  'environment.get!("PATH")'

# ── Test 28: Full server example from spec ──
SERVER_TERSE='use std.srv

let routes = srv.new()
  |> srv.get("/health", fn(_req) => srv.res(200, "ok"))
  |> srv.mw(fn(req, next) => do {
    log.info(str.fmt("req: {} {}", [req.method, req.path]))
    next(req)
  })
  |> srv.start(8080)'

SERVER_VERBOSE='use std.server

let routes = server.new()
  |> server.get("/health", fn(_req) => server.response(200, "ok"))
  |> server.middleware(fn(req, next) => do {
    log.info(string.format("req: {} {}", [req.method, req.path]))
    next(req)
  })
  |> server.start(8080)'

actual=$(echo "$SERVER_TERSE" | $CLANK pretty --stdin 2>/dev/null)
if [ "$actual" = "$SERVER_VERBOSE" ]; then
  pass "full server example"
else
  fail "full server example"
fi

# ── Summary ──
echo ""
echo "Results: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
