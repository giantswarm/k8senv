---
description: Analyze open beads issues for duplicates, dependencies, and missing details
allowed-tools: Task, Bash(bd:*), Read, Grep, Glob, AskUserQuestion
---

# Analyze Issues Workflow

Analyze all open beads issues to find duplicates, identify dependencies, and enrich issues with codebase details.

## Critical Rules

1. **Always ask for approval** before merging duplicates or adding dependencies
2. **Use codebase search** to enrich issues with file:line references
3. **Preserve the higher-priority issue** when merging duplicates

## Phase 1: Gather All Open Issues

Run these commands to collect issue data:

```bash
bd list --status=open
```

For each issue ID in the list, run `bd show <id>` to get full details including description.

Build a working list with: id, title, description, type, priority.

## Phase 2: Detect Duplicates

Compare all issues looking for duplicates using these criteria:

| Match Type | Criteria | Action |
|------------|----------|--------|
| Exact match | Identical titles (ignoring case/whitespace) | Definite duplicate |
| File match | Same file name referenced in both titles | Likely duplicate |
| Word overlap | >80% word overlap in titles | Potential duplicate |

### Grouping Duplicates

Group potential duplicates together. For each group:
- Identify the **primary issue** (keep the one with: lower priority number, more detail, or earlier creation date)
- List the **duplicate issues** to close

### User Approval

Present all duplicate groups to the user:

```
## Duplicate Groups Found

**Group 1:**
- Primary: [id1] [P1] - Title 1 (keep this)
- Duplicate: [id2] [P2] - Title 2 (close this)
Reason: [exact match / same file / high word overlap]

**Group 2:**
...
```

Use AskUserQuestion to ask which duplicates to merge. Only proceed with user-approved merges.

### Merge Action

For each approved merge:

```bash
bd close <duplicate-id> --reason="Duplicate of <primary-id>"
```

## Phase 3: Identify Dependencies

Analyze remaining open issues for logical dependencies using these patterns:

| Pattern in Title | Dependency Logic |
|------------------|------------------|
| "Add tests for X" or "Test X" | May depend on implementing X first |
| "Document X" or "Add docs for X" | Depends on X being implemented |
| Bug fix that enables feature | Feature is blocked by the bug fix |
| "Refactor X" as prerequisite | Other changes to X depend on refactor |

Also look for:
- Issues referencing the same file (may have ordering)
- Issues where one's completion enables another
- Higher priority issues that should complete before lower priority ones

### User Approval

Present proposed dependencies:

```
## Proposed Dependencies

1. [id-A] "Add tests for pool.go" depends on [id-B] "Fix race in pool.go"
   Reason: Tests should validate the fix

2. [id-C] "Document timeout behavior" depends on [id-D] "Implement timeout"
   Reason: Documentation requires implementation
...
```

Use AskUserQuestion to ask which dependencies to add. Only proceed with user-approved dependencies.

### Add Dependencies

For each approved dependency:

```bash
bd dep add <issue-id> <depends-on-id>
```

## Phase 4: Enrich Issue Details

For each remaining open issue, check if it lacks specifics:

### Issues Needing Enrichment

- Issues mentioning a file without line numbers
- Issues with vague descriptions like "fix X" without specifics
- Issues that could benefit from code context

### Enrichment Process

1. **Search codebase** using Grep and Glob for relevant files/code
2. **Read files** to find specific line numbers, function names, or context
3. **Report findings** - do NOT automatically update issues, just report what was found

### Output Findings

For each issue that could be enriched:

```
**[id] - Title**
Current: [current description or "no description"]
Found:
- File: pkg/k8senv/file.go:42 - relevant function/code
- Context: [brief explanation of what the code does]
Suggested addition: [what to add to the description]
```

## Phase 5: Summary Report

Provide a final summary:

```
## Analysis Summary

### Duplicates Merged: X
- [id1] closed as duplicate of [id2] (reason)
...

### Dependencies Added: Y
- [id1] depends on [id2] (reason)
...

### Issues That Could Be Enriched: Z
- [id]: [suggested enrichment]
...

### Remaining Open Issues: N

Run `bd list --status=open` to see the updated issue list.
```

## Error Handling

| Scenario | Action |
|----------|--------|
| No open issues | Report "No open issues found" and stop |
| `bd show` fails | Skip that issue, continue with others |
| `bd close` fails | Report error, continue with other merges |
| `bd dep add` fails | Report error, continue with other dependencies |
| User declines all merges | Skip to Phase 3 |
| User declines all dependencies | Skip to Phase 4 |
