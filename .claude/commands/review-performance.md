---
description: Review Go code for performance problems, and create issues for findings
argument-hint: [files|path|changes]
allowed-tools: Task, Bash(bd:*), Bash(git:*), Read, Grep, Glob
---

<objective>
Audit the codebase for performance bottlenecks, produce a prioritized list of concrete optimization opportunities, and create a tracking issue for each actionable finding.
</objective>

<process>
1. **Profile hot paths** — Identify the critical execution paths: pool acquire/release, instance startup/shutdown, namespace cleanup, and CRD caching
2. **Scan for known anti-patterns** — Search for:
 - Unnecessary allocations in loops (slice growth without prealloc, string concatenation)
 - Lock contention (mutexes held across I/O, broad lock scopes)
 - Redundant I/O (repeated file reads, uncached lookups, sequential operations that could parallelize)
 - Inefficient Kubernetes client usage (missing shared informers, per-call client creation)
3. **Check concurrency design** — Review goroutine lifecycle, channel usage, and sync primitives for unnecessary serialization or missed parallelism
4. **Assess resource cleanup** — Verify timely release of file handles, network connections, and temporary directories
5. **Prioritize findings** — Rank each finding by: (a) frequency of the hot path, (b) estimated latency/memory impact, (c) implementation complexity
6. **Create issues** — For each finding rated Medium or High impact, create a tracking issue using `bd`:
 - Run `bd add --title "<title>" --body "<body>"` for each issue
 - Use the `perf` label if supported, otherwise prefix title with `perf: `
 - Group related micro-optimizations into a single issue rather than one per line
 - After all issues are created, run `bd sync` once to sync with git
</process>

<output_format>
For each finding, provide:
- **Location**: `file_path:line_number`
- **Category**: allocation | lock contention | redundant I/O | concurrency | resource leak
- **Current behavior**: What the code does now
- **Proposed change**: Specific refactoring with code sketch
- **Impact**: High / Medium / Low with one-sentence justification
- **Complexity**: High / Medium / Low
- **Issue**: Issue ID after creation, or "skipped — low impact" for Low findings

Sort findings by impact descending, then complexity ascending.

**Issue body format:**
Problem

[What the current code does and why it's suboptimal]

Location

file_path:line_number

Proposed Fix

[Concrete change with code sketch]

Impact

[Expected improvement and justification]

Notes

- "Needs benchmark" if improvement is theoretical
- Any tradeoffs (readability, complexity)
</output_format>

<edge_cases>
- If a potential optimization trades readability for speed, flag the tradeoff explicitly in the issue body
- If an optimization requires benchmarking to validate, note "needs benchmark" rather than assuming improvement
- Do not suggest optimizations that change public API behavior or signatures
- Ignore test files — focus on library code under the root package and `internal/`
- If `bd` is not available, fall back to listing findings without creating issues and inform the user
- Do not create issues for Low impact findings — mention them in the summary only
</edge_cases>

<success_criteria>
- Every finding references a specific file and line range
- Each proposed change is concrete enough to implement without further research
- No vague recommendations like "consider caching" without specifying what to cache and where
- Findings do not duplicate optimizations already present (check recent commits for prior perf work)
- Every Medium and High impact finding has a corresponding tracking issue
- Issues have descriptive titles and structured bodies sufficient for another developer to implement
- `bd sync` is run exactly once after all issues are created
</success_criteria>
