---
description: Pick up the next ready issue and work it end-to-end
allowed-tools: Bash(bd:*), Skill
---

# Do Next Issue Workflow

Automatically pick up the next available issue and work it using the `do-issue` workflow.

## Step 1: Find Ready Issues

Run `bd ready` to list issues that are ready to work (no blockers, not in progress):

```bash
bd ready
```

## Step 2: Parse the Output

Extract the first issue ID from the output. Issue IDs follow the format `beads-XXXX` (e.g., `beads-0001`, `beads-0042`).

### If no ready issues exist

Tell the user: "No ready issues found. Use `bd list --status=open` to see all open issues or `bd create` to create a new one."

Then stop - do not proceed to Step 3.

## Step 3: Invoke do-issue

Use the Skill tool to invoke the `do-issue` command with the extracted issue ID:

```
skill: "do-issue"
args: "<issue-id>"
```

Replace `<issue-id>` with the actual ID extracted from Step 2.

---

## Example

If `bd ready` outputs:
```
beads-0015  feature  P2  Add metrics export functionality
beads-0018  bug      P1  Fix connection timeout handling
```

Extract `beads-0015` (the first issue) and invoke:
```
skill: "do-issue"
args: "beads-0015"
```
