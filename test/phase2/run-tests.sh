#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
CLANK="${1:-npx tsx src/main.ts}"
VM_FLAG="${2:-}"
source "$DIR/../test-helpers.sh"
run_phase_tests "$DIR" "$CLANK" "$VM_FLAG"
