---
description: Work on a very basic issue
argument-hint: <issue-id>
allowed-tools: Task, Bash(bd:*), Bash(git status:*), Bash(git diff:*), Bash(git add:*), Bash(make:*), Read, Edit, Write, Grep, Glob, Skill(commit-staged)
---

# Do Basic Issue Workflow

Fix/implement the given issue, validate with integration tests, commit, and close.

## Phase 1: Understand the Issue

1. **Verify issue ID provided:**
   - If `$ARGUMENTS` is empty, ask: "Please provide an issue ID (e.g., `/do-basic2 k8senv-rb50`)"

2. **Fetch issue details:**
   ```bash
   bd show $ARGUMENTS
   ```

3. **If issue not found**, stop and report the error

4. **Mark issue in progress:**
   ```bash
   bd update $ARGUMENTS --status in_progress
   ```

5. **Extract from issue:** title, description, type, acceptance criteria

## Phase 2: Implementation

1. **Check for blockers:**
   - If issue has unresolved dependencies, stop and report what's blocking

2. **Explore the relevant code** areas identified in the issue

3. **Implement the fix/feature:**
   - Read relevant files to understand context
   - Make minimal, focused changes to address the issue
   - Follow existing code patterns and conventions
   - If the issue requires new tests, add them in `tests/` following existing integration test patterns

## Phase 3: Validate

1. **Run linter:**
   ```bash
   make lint
   ```
   - Fix all lint errors before proceeding

2. **Run integration tests:**
   ```bash
   make test-integration
   ```
   - If any tests fail, investigate whether the failure is related to this issue
   - Fix failing tests and re-run until all pass

3. **If validation fails and cannot be resolved**, keep issue open and report what needs fixing

## Phase 4: Commit and Close

1. **Stage changed files** with `git add` (specific files, not `-A`)

2. **Commit** using the `/commit-staged` slash command

3. **Close the issue:**
   ```bash
   bd close $ARGUMENTS --reason "Implemented and verified"
   ```

## Output Summary

Provide at completion:
- Status: Completed / Needs Attention
- Files changed
- Test results
