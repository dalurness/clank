#!/usr/bin/env bash
# Formatter test suite
# Tests: idempotency, correctness, --check, --diff, --stdin modes
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"
CLANK="npx tsx $ROOT/src/main.ts"
PASS=0
FAIL=0

fail() { echo "FAIL: $1"; FAIL=$((FAIL + 1)); }
pass() { echo "PASS: $1"; PASS=$((PASS + 1)); }

# ── Test 1: Idempotency on all fmt test files ──
for f in "$DIR"/*.clk; do
  name=$(basename "$f")
  formatted=$($CLANK fmt --stdin < "$f" 2>/dev/null)
  reformat=$(echo "$formatted" | $CLANK fmt --stdin 2>/dev/null)
  if [ "$formatted" = "$reformat" ]; then
    pass "idempotent: $name"
  else
    fail "idempotent: $name"
  fi
done

# ── Test 2: Canonical fmt test files are already formatted ──
for f in "$DIR"/*.clk; do
  name=$(basename "$f")
  formatted=$($CLANK fmt --stdin < "$f" 2>/dev/null)
  original=$(cat "$f")
  if [ "$formatted" = "$original" ]; then
    pass "canonical: $name"
  else
    fail "canonical: $name"
    diff <(echo "$original") <(echo "$formatted") || true
  fi
done

# ── Test 3: --check returns 0 for formatted files ──
for f in "$DIR"/*.clk; do
  name=$(basename "$f")
  if $CLANK fmt --check "$f" >/dev/null 2>&1; then
    pass "--check ok: $name"
  else
    fail "--check ok: $name"
  fi
done

# ── Test 4: --check returns 1 for unformatted input ──
tmp=$(mktemp /tmp/clank-fmt-test-XXXXX.clk)
echo 'main : () -> <io> () =  print(  "hello"  )' > "$tmp"
if $CLANK fmt --check "$tmp" >/dev/null 2>&1; then
  fail "--check detects unformatted"
else
  pass "--check detects unformatted"
fi
rm "$tmp"

# ── Test 5: --diff produces output for unformatted input ──
tmp=$(mktemp /tmp/clank-fmt-test-XXXXX.clk)
echo 'main : () -> <io> () =  print(  "hello"  )' > "$tmp"
diffout=$($CLANK fmt --diff "$tmp" 2>/dev/null)
if [ -n "$diffout" ]; then
  pass "--diff shows changes"
else
  fail "--diff shows changes"
fi
rm "$tmp"

# ── Test 6: --stdin reads and writes ──
result=$(echo 'main : () -> <io> () = print("hi")' | $CLANK fmt --stdin 2>/dev/null)
if echo "$result" | grep -q 'print("hi")'; then
  pass "--stdin round-trips"
else
  fail "--stdin round-trips"
fi

# ── Test 7: Formatting preserves program semantics (examples) ──
for f in "$ROOT"/test/examples/*.clk; do
  name=$(basename "$f")
  orig_out=$($CLANK "$f" 2>/dev/null || true)
  formatted=$($CLANK fmt --stdin < "$f" 2>/dev/null)
  tmp=$(mktemp /tmp/clank-fmt-test-XXXXX.clk)
  echo "$formatted" > "$tmp"
  fmt_out=$($CLANK "$tmp" 2>/dev/null || true)
  rm "$tmp"
  if [ "$orig_out" = "$fmt_out" ]; then
    pass "semantics: $name"
  else
    fail "semantics: $name"
  fi
done

# ── Test 8: Idempotency on all test files ──
idempotent_count=0
idempotent_fail=0
find "$ROOT/test" -name "*.clk" | while read f; do
  formatted=$($CLANK fmt --stdin < "$f" 2>/dev/null)
  if [ -z "$formatted" ]; then continue; fi
  reformat=$(echo "$formatted" | $CLANK fmt --stdin 2>/dev/null)
  if [ "$formatted" != "$reformat" ]; then
    fail "idempotent-all: $(basename $f)"
  fi
done
pass "idempotent-all: complete"

# ── Test 9: Operator precedence preservation ──
result=$(echo 'main : () -> <io> () = print(show((2 + 3) * 4))' | $CLANK fmt --stdin 2>/dev/null)
if echo "$result" | grep -q '(2 + 3) \* 4'; then
  pass "precedence parens preserved"
else
  fail "precedence parens preserved"
fi

# ── Summary ──
echo ""
echo "Results: $PASS passed, $FAIL failed"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
