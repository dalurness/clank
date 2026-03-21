#!/usr/bin/env bash
# Run all Clank test phases
# Usage: ./run-tests.sh [path-to-clank-binary] [--vm]
#   --vm  Run programs through the bytecode compiler + VM instead of the tree-walker

set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
CLANK="${1:-npx tsx src/main.ts}"
VM_FLAG=""
EXIT=0

# Check for --vm in any argument position
for arg in "$@"; do
  if [[ "$arg" == "--vm" ]]; then
    VM_FLAG="--vm"
  fi
done

for suite_dir in "$DIR"/phase*/ "$DIR"/examples/; do
  suite="$(basename "$suite_dir")"
  if [[ -x "$suite_dir/run-tests.sh" ]]; then
    echo "═══ $suite ═══"
    "$suite_dir/run-tests.sh" "$CLANK" $VM_FLAG || EXIT=1
    echo ""
  fi
done

exit $EXIT
