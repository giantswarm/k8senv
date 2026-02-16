---
description: Review Go code for concurrency problems, and create issues for findings
argument-hint: [files|path|changes]
allowed-tools: Task, Bash(bd:*), Bash(git:*), Read, Grep, Glob
---

<objective>
Review all concurrency code in this repository for correctness and performance issues.
Launch two parallel review agents — one focused on correctness, one on performance.
Create a beads issue for each confirmed problem or actionable improvement.
</objective>

<process>
1. Identify all files containing concurrency primitives (sync.Mutex, sync.RWMutex,
 sync.WaitGroup, channels, atomic operations, goroutine launches, sync.Once,
 sync.Pool, context cancellation patterns)
2. Launch two parallel agents:
 - **Correctness agent**: Review for races, deadlocks, missed unlocks, channel
	 leaks, goroutine leaks, unsafe shared state, incorrect sync primitive usage,
	 and missing context cancellation propagation
 - **Performance agent**: Review for lock contention, unnecessary serialization,
	 oversized critical sections, channel bottlenecks, excessive goroutine creation,
	 and opportunities to use sync.Pool or atomic operations instead of mutexes
3. For each confirmed finding, create a beads issue with:
 - Title: concise description of the problem
 - Type: `bug` (correctness) or `task` (performance)
 - Priority: 1 (correctness bugs), 2 (performance improvements)
 - Description: file:line reference, explanation of the problem, and suggested fix

Do NOT create issues for speculative or low-confidence findings.
Do NOT modify any code — this is a read-only review.
</process>

<success_criteria>
- Every file with concurrency primitives was reviewed by both agents
- Each issue includes a specific file:line reference and clear explanation
- Zero false positives — every issue describes a real, reproducible concern
- Correctness issues are distinguished from performance issues via type field
</success_criteria>
