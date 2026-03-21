#!/usr/bin/env bash
# Shared test helpers for Clank test phases
# Source this from per-phase run-tests.sh

run_phase_tests() {
  local phase_dir="$1"
  local clank="$2"
  local vm_flag="${3:-}"
  local pass=0
  local fail=0
  local total=0

  for test_file in "$phase_dir"/*.clk; do
    [[ -f "$test_file" ]] || continue
    local name
    name="$(basename "$test_file")"

    # Skip non-test files (library modules without expected output/error annotations)
    if ! grep -q '^# Expected' "$test_file"; then
      continue
    fi

    total=$((total + 1))

    # Check if this test expects an error (non-zero exit)
    local expect_error=false
    if grep -q '^# Expected:.*error' "$test_file"; then
      expect_error=true
    fi

    # Extract expected output lines (lines matching "# <expected line>" after "# Expected output:")
    local expected=""
    local in_expected=false
    while IFS= read -r line; do
      if [[ "$line" == "# Expected output:" ]]; then
        in_expected=true
        continue
      fi
      if $in_expected; then
        if [[ "$line" =~ ^#\  ]]; then
          expected+="${line#\# }"$'\n'
        elif [[ "$line" == "#" ]]; then
          # Empty expected output line
          expected+=$'\n'
        else
          break
        fi
      fi
    done < "$test_file"

    # Run the test
    local actual=""
    local exit_code=0
    actual=$($clank $vm_flag "$test_file" 2>&1) || exit_code=$?

    if $expect_error; then
      # Should have failed
      if [[ $exit_code -ne 0 ]]; then
        echo "  ✓ $name (expected error)"
        pass=$((pass + 1))
      else
        echo "  ✗ $name — expected error but got exit 0"
        fail=$((fail + 1))
      fi
    else
      # Should have succeeded
      if [[ $exit_code -ne 0 ]]; then
        echo "  ✗ $name — exited with code $exit_code"
        echo "    output: $actual"
        fail=$((fail + 1))
      elif [[ -n "$expected" ]]; then
        # Compare output (trim trailing newline from both)
        local expected_trimmed actual_trimmed
        expected_trimmed="$(printf '%s' "$expected" | sed 's/[[:space:]]*$//')"
        actual_trimmed="$(printf '%s' "$actual" | sed 's/[[:space:]]*$//')"
        if [[ "$actual_trimmed" == "$expected_trimmed" ]]; then
          echo "  ✓ $name"
          pass=$((pass + 1))
        else
          echo "  ✗ $name — output mismatch"
          echo "    expected: $(echo "$expected_trimmed" | head -3)"
          echo "    actual:   $(echo "$actual_trimmed" | head -3)"
          fail=$((fail + 1))
        fi
      else
        # No expected output specified, just check it ran
        echo "  ✓ $name"
        pass=$((pass + 1))
      fi
    fi
  done

  echo "  $pass/$total passed"
  [[ $fail -eq 0 ]]
}
