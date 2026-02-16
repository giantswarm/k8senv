#!/usr/bin/env bash
set -euo pipefail

cd "$CLAUDE_PROJECT_DIR"

# Get list of modified files (staged and unstaged)
modified_files=$(git status --porcelain | awk '{print $2}')

if [[ -z "$modified_files" ]]; then
  exit 0
fi

run_fmt=false
run_lint=false
run_test=false

# Check each modified file
while IFS= read -r file; do
  if [[ "$file" == *.go ]]; then
    run_fmt=true
    run_lint=true
    run_test=true
  fi

  if [[ "$file" == go.mod || "$file" == go.sum ]]; then
    run_test=true
  fi

  if [[ "$file" == Makefile || "$file" == .golangci.yml ]]; then
    run_lint=true
  fi
done <<< "$modified_files"

# Execute commands
if [[ "$run_fmt" == true ]]; then
  echo "Running: make fmt"
  make fmt
fi

if [[ "$run_lint" == true ]]; then
  echo "Running: make lint"
  if ! make lint; then
    echo "" >&2
    echo "--------------------------------------------------------" >&2
    echo "LINT FAILED - Review output above" >&2
    echo "Fix all lint issues before proceeding." >&2
    echo "--------------------------------------------------------" >&2
    exit 2
  fi
fi

if [[ "$run_test" == true ]]; then
  echo "Running: make test"
  if ! make test; then
    echo "" >&2
    echo "--------------------------------------------------------" >&2
    echo "TESTS FAILED - Review output above" >&2
    echo "The tests failed. Fix all the failing tests." >&2
    echo "--------------------------------------------------------" >&2
    exit 2
  fi
fi
