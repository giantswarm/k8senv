---
description: Work an issue end-to-end with implementation and review agents
argument-hint: <issue-id>
allowed-tools: Task, Bash(bd:*), Bash(git status:*), Bash(git diff:*), Bash(make:*), Read, Edit, Write, Grep, Glob
---

# Do Issue Workflow

Work an issue from start to finish: understand requirements, clarify intent, implement with agents, review, and handle findings.

## Critical Rules

**You MUST follow this entire workflow. Do not skip phases or take shortcuts.**

1. **Follow ALL phases in order** - Each phase builds on the previous one
2. **Use `go-code-writer` for implementation** - DO NOT implement code directly yourself
3. **Use `go-code-reviewer` for review** - Always review implementation before finalizing
4. **Create follow-up issues** - Minor/medium findings MUST become tracked issues
5. **Use TodoWrite to track progress** - Create todos for each phase and update as you go

## Phase 1: Understand the Issue

1. **Verify issue ID provided:**
   - If `$ARGUMENTS` is empty, ask: "Please provide an issue ID (e.g., `/do-issue beads-123`)"

2. **Fetch issue details:**
   ```bash
   bd show $ARGUMENTS
   ```

3. **If issue not found**, stop and report the error

4. **Mark issue in progress:**
   ```bash
   bd update $ARGUMENTS --status in_progress
   ```

5. **Extract from issue:** title, description, type, priority, acceptance criteria, dependencies

## Phase 2: Clarify Requirements

Use AskUserQuestion to gather missing information:

### Required Clarifications
- What is the expected behavior vs current behavior (for bugs)?
- What are the acceptance criteria (if not defined)?
- Which files or packages should this affect?
- Are there constraints or non-functional requirements?

### Optional Aspects
Proactively ask about:
- Should this include tests? (unit, integration, both?)
- Documentation updates needed?
- Error handling patterns to follow?
- Performance considerations?
- Backward compatibility requirements?

### Design Decisions (if architectural)
- Preferred patterns or approaches?
- Integrate with existing code or standalone?
- Interface design preferences?

**Wait for user responses before proceeding.**

## Phase 3: Implement with go-code-writer

**IMPORTANT: DO NOT implement code directly. You MUST use the `go-code-writer` agent.**

Use Task tool to invoke the implementation agent:

```
subagent_type: "go-code-writer"
prompt: |
  Implement the following issue:

  **Issue ID:** [id]
  **Title:** [title]
  **Description:** [description]
  **Acceptance Criteria:** [criteria]
  **Clarified Requirements:** [summary from Phase 2]

  Follow existing project patterns. Ensure tests pass. Add error handling with context.
```

Capture: files modified, design decisions, verification results.

## Phase 4: Review with go-code-reviewer

Use Task tool to invoke the review agent:

```
subagent_type: "go-code-reviewer"
prompt: |
  Review the changes made for issue [id].

  **Files changed:** [list from Phase 3]
  **What was implemented:** [summary]

  Focus on: correctness, error handling, interface design, naming, concurrency safety, project patterns.
```

Parse review output by severity: Critical, Important, Minor.

## Phase 5: Handle Review Findings

### Critical and Important Issues
Re-invoke `go-code-writer` to fix:

```
subagent_type: "go-code-writer"
prompt: |
  Fix the following issues from code review:

  **Critical Issues:** [list with file:line]
  **Important Issues:** [list with file:line]

  Apply suggested fixes. Ensure tests still pass.
```

### Medium and Low Severity Issues

**IMPORTANT: You MUST create follow-up issues for ALL medium/low findings. Do not skip this step.**

Create follow-up issues:

```bash
bd create --title="[finding description]" --type=task --priority=3
```

Guidelines:
- Group related minor findings when appropriate
- Include original issue ID as context
- Priority 3 for medium, 4 for low
- Include file:line references
- Report created issue IDs in output summary

## Phase 6: Finalize

1. **Run verification:**
   ```bash
   make build && make test && make lint
   ```

2. **If all pass, close issue:**
   ```bash
   bd close $ARGUMENTS --reason "Implemented and reviewed"
   ```

3. **If verification fails**, keep issue open and report what needs fixing

## Phase 7: Save Learnings

Append to `memory.txt` any:
- New patterns discovered in this codebase
- Gotchas or unexpected behaviors
- Design decisions and rationale
- Testing insights
- Review findings that reveal broader themes

Format (following existing memory.txt pattern):
```markdown
## [Topic Title]

[2-4 sentences describing the learning]

**Problem pattern:** (if applicable)
**Fix:** (if applicable)
```

## Error Handling

| Scenario | Action |
|----------|--------|
| No issue ID | Ask user for ID |
| Issue not found | Report error, suggest `bd list` |
| Issue already closed | Ask if user wants to reopen |
| Agent fails | Report error, ask how to proceed |
| Build/test fails | Report failures, do not close issue |

## Output Summary

Provide at completion:
- Status: Completed / Needs Attention
- Files changed
- Test results
- Review findings summary (critical/important fixed, minor filed)
- Follow-up issues created (list IDs)
- Learnings saved: Yes/No

---

## Workflow Overview

**Use TodoWrite to track your progress through each phase.**

```
Phase 1: Understand    → Fetch issue, mark in progress
Phase 2: Clarify       → Ask user questions, gather requirements
Phase 3: Implement     → Use go-code-writer agent (NOT direct implementation)
Phase 4: Review        → Use go-code-reviewer agent
Phase 5: Handle        → Fix critical/important, create issues for minor
Phase 6: Finalize      → Run tests, close issue
Phase 7: Learnings     → Save patterns to memory.txt
```

**At the start of this workflow, create a todo list with all 7 phases. Mark each phase in_progress as you start it and completed when done. This ensures no phase is skipped.**
