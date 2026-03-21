#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
CLANK="${1:-npx tsx src/main.ts}"
source "$DIR/../test-helpers.sh"
VM_FLAG="${2:-}"
run_phase_tests "$DIR" "$CLANK" "$VM_FLAG"
