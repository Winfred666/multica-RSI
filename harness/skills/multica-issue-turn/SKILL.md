---
name: multica-issue-turn
description: Advance a Multica issue or comment turn using the original review, work, status, command, and sub-issue instructions from Multica's runtime prompt.
---

# multica-issue-turn — system v0

Advance the current issue with its actual run context. Read the applicable issue workflow in `references/workflow.md` unless it is already present in this conversation. Reuse unchanged instructions already in context; open other references only for missing guidance. Agent Identity and the current workspace acceptance policy remain authoritative.

- [commands.md](references/commands.md) — CLI command flags or issue-body formatting.
- [workflow.md](references/workflow.md) — Issue reads, work/status handoffs, children or mentions.
- [environment.md](references/environment.md) — Repository and project-resource guidance.
- [background-and-files.md](references/background-and-files.md) — Run-owned processes, persistent services or attachments.

These references adapt native workflow guidance from revision `ad5ab50ed` for the selected workspace SOPs. For delegated child delivery and settlement, follow `multica-surface-delivery` and the selected sub-issue SOP.
