#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <make-target> [MAKE_VAR=value ...]" >&2
    exit 1
fi

target="$1"
shift
interval="${INTERVAL:-5}"

iteration=1
failures=0
tmplog="run-${target}-tmp.log"
: > "$tmplog"
while true; do
    printf '\n\033[0;36m--- Iteration %d: make %s %s ---\033[0m\n' "$iteration" "$target" "$*"

    if make "$target" "$@" 2>&1 | tee -a "$tmplog"; then
        printf '\033[0;32mIteration %d passed (%d failures so far)\033[0m\n' "$iteration" "$failures"
    else
        failures=$((failures + 1))
        logfile=$(printf 'run-%s-%03d.log' "$target" "$failures")
        mv "$tmplog" "$logfile"
        : > "$tmplog"
        printf '\033[0;31mIteration %d failed (failure #%d, see %s)\033[0m\n' "$iteration" "$failures" "$logfile"
    fi

    iteration=$((iteration + 1))
    sleep "$interval"
done
rm -f "$tmplog"
