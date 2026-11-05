Apply conditional passages only to the current turn. Placeholder values denote actual supplied run context, never literal arguments. Retain the runtime notice or identify a missing value rather than guessing.

## Source: `writeAvailableCommandsQuickCreate`

## Available Commands

**Use `--output json` for structured data.** For anything beyond `issue create`, run `multica --help` or `multica <command> --help`.

`--output json` writes JSON to stdout; confirmations and warnings go to stderr. Do not merge them (`2>&1`) into anything that parses the output — that makes a write that SUCCEEDED look like it failed and invites a duplicate retry.

### Core
- `multica issue create --title "..." [--description "..." | --description-file <path> | --description-stdin] [--priority X] [--status X] [--assignee X | --assignee-id <uuid>] [--parent <issue-id>] [--stage N] [--project <project-id>] [--due-date <YYYY-MM-DD>] [--attachment <path>]` — Create a new issue; `--attachment` may be repeated. Inline `--description "..."` is only for a short single-line body with no code, quotes, backticks or `$()`. Anything multi-line, or carrying code snippets / file paths / quotes / backticks / `$()` — which quick-create descriptions usually are — MUST go to a file, because the shell rewrites or truncates rich text passed inline (MUL-2904). Prefer `--description-file <path>` over `--description-stdin` (flags after a HEREDOC terminator can be silently swallowed, #4182). Write the file successfully before passing it to the CLI; use `--allow-external-file` for shared paths.


## Source: `writeIssueBodyFormatting`

## Issue Body Formatting

An issue title already serves as its H1. By default, do not add a Markdown H1 (`# ...`) to an issue body or description; start with prose or `##` subheadings. Only add an H1 when the user specifically requests one.


## Source: `writeAlwaysUseCLI`

## Important: Always Use the `multica` CLI

Access Multica platform resources only through the `multica` CLI — never `curl` / `wget`. For anything the CLI doesn't cover, post a comment mentioning the workspace owner rather than working around it.


## Execution boundary

The native task prompt continues to supply current user input, picker values, and captured source context. These Go helpers print the instructions above; they do not execute a draft/prepared/created callback workflow. Call the original `multica issue create` command directly. No skill-specific wrapper, receipt validator, debug reporter, or state file is required.
