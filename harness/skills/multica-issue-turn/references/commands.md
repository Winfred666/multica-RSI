Apply conditional passages only to the current turn. Placeholder values denote actual supplied run context, never literal arguments. Retain the runtime notice or identify a missing value rather than guessing.

## Source: `writeAvailableCommands`

## Available Commands

Prefer `--output json` for structured data. The default brief lists only the core agent loop and common issue create/update tasks; for everything else run `multica --help` or `multica <command> --help`.

`--output json` writes JSON to stdout; confirmations and warnings go to stderr. Do not merge them (`2>&1`) into anything that parses the output — that makes a write that SUCCEEDED look like it failed and invites a duplicate retry.

### Core
- `multica issue get <id> --output json` — full issue.
- `multica issue comment list <issue-id> [--roots-only] [--summary] [--thread <comment-id> [--tail N] | --recent N] [--since <RFC3339>] [--compact] --output json` — thread-aware comment reads. Read in two bounded steps: scan with `--roots-only --summary`, then expand one thread with `--thread <id> --tail N`. Careful with `--recent N`: it caps THREADS, not comments, and can return the whole history on a small issue. Resolved-thread folding, paging cursors, and full flag semantics: `--help`.
- `multica issue create --title "..." [--description-file <path>] [--priority X] [--status X] [--assignee X | --assignee-id <uuid>] [--parent <issue-id>] [--stage N] [--project <project-id>] [--due-date <YYYY-MM-DD>] [--attachment <path>]` — create an issue. Put agent-authored long descriptions in `--description-file <path>` (heredoc stdin can swallow trailing flags, #4182); shared paths work with `--allow-external-file`.
- `multica issue update <id> [--title X] [--description-file <path>] [--priority X] [--status X] [--assignee X] [--parent <issue-id>] [--stage N] [--project <project-id>] [--due-date <YYYY-MM-DD>]` — update fields; pass `--parent ""` to clear parent.
- `multica issue assign <id> (--to X | --to-id <uuid> | --unassign)` — change ownership; assigning to an agent can start a run.

- `multica issue status <id> <status>` — use the current turn's `[Project workflow]` commands when present. Otherwise use the workspace status catalog; an invalid status returns the valid keys.

*Applies generally within this section.*

- `multica issue status <id> cancelled --duplicate-of <original>` — cancel an issue that duplicates another and record the mark, so the original lists it; a plain cancel plus a comment leaves no link.
- `multica issue wakeup <create|list|get|update|disable|trigger|delete|runs|events>` — persist an event, condition or time wakeup on this issue, then finish the current run. When the platform can check the fact itself, use one `--until-*` condition so no run starts before it holds; check the rule's `last_error` for missing PR links or snapshots, which are retried automatically. Conditions, PR rules and actor filters: `multica issue wakeup create --help` and the multica-platform issues reference.
- `multica issue children <id> [--output json]` — list a parent's sub-issues grouped by stage.
- `multica issue comment add <issue-id> [--content "..." | --content-file <path> | --content-stdin] [--parent <comment-id>] [--attachment <path>]` — post a comment. Agent-authored bodies MUST use `--content-file` (`--allow-external-file` for shared paths); full flags: `multica issue comment add --help`.
- `multica repo checkout <url> [--ref <branch-or-sha>] [--fresh]` — repository checkout on a dedicated branch; re-running it keeps a checkout that has uncommitted or unpushed work, or is already on this task's branch, and only fetches; a clean, fully pushed checkout on another branch starts over on a new branch. `--fresh` discards uncommitted and untracked files for a new branch; commits stay on the old branch, so push any you still need first.

Commits use the user's configured Git identity; keep it unless asked. For a requested override in a managed checkout use `git config --worktree user.name` / `user.email` — plain or `--local` config can leak into a shared cache; never change the global identity.


*Applies when: ctx.IsSquadLeader*

### Squad maintenance
- `multica squad member set-role <squad-id> --member-id <id> --member-type <agent|member> --role <role> [--output json]` — change role in place (use this instead of remove+add).


## Source: `writeIssueBodyFormatting`

## Issue Body Formatting

An issue title already serves as its H1. By default, do not add a Markdown H1 (`# ...`) to an issue body or description; start with prose or `##` subheadings. Only add an H1 when the user specifically requests one.


## Source: `writeAlwaysUseCLI`

## Important: Always Use the `multica` CLI

Access Multica platform resources only through the `multica` CLI — never `curl` / `wget`. For anything the CLI doesn't cover, post a comment mentioning the workspace owner rather than working around it.
