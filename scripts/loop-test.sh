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

    start=$SECONDS
    if make "$target" "$@" 2>&1 | tee -a "$tmplog"; then
        elapsed=$((SECONDS - start))
        printf '\033[0;32mIteration %d passed (%d failures so far) (%dm %ds)\033[0m\n' "$iteration" "$failures" $((elapsed/60)) $((elapsed%60))
    else
        elapsed=$((SECONDS - start))
        failures=$((failures + 1))
        logfile=$(printf 'run-%s-%03d.log' "$target" "$failures")
        mv "$tmplog" "$logfile"
        : > "$tmplog"
        printf '\033[0;31mIteration %d failed (failure #%d, see %s) (%dm %ds)\033[0m\n' "$iteration" "$failures" "$logfile" $((elapsed/60)) $((elapsed%60))
    fi

    iteration=$((iteration + 1))
    sleep "$interval"
done
rm -f "$tmplog"
