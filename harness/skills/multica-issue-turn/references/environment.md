Apply conditional passages only to the current turn. Placeholder values denote actual supplied run context, never literal arguments. Retain the runtime notice or identify a missing value rather than guessing.

## Source: `writeRepositories`


*Applies when: not (len(ctx.Repos) == 0)*

## Repositories

Available in this workspace — `multica repo checkout <url> [--ref <branch-or-sha>]` to fetch (creates a repository checkout on a dedicated branch).


*Applies when: not (len(ctx.Repos) == 0) and for each entry in ctx.Repos*

<line>

*Applies when: not (len(ctx.Repos) == 0) and pinned*


A supplied branch ref identifies a starting point; it does not mean the native task directory already contains a checkout. Use the prepared SOP worktree and its branch binding. Deliver to the same line: open pull requests with `gh pr create --base <that-branch>`. If what it starts from is a tag or a commit rather than a branch, treat it as a starting point only and confirm the target branch before opening a pull request.

*Applies when: not (len(ctx.Repos) == 0) and len(ctx.Repos) > 0*


If `multica repo checkout` reports that it KEPT an existing checkout, you are continuing work that began earlier — possibly before this project was last reconfigured. The branch it names is the branch your work sits ON: the head of a pull request, never its base. It does not record where that work was meant to land. Keep delivering where this work was already going — the base of its existing pull request, or the target the task states — and ask if neither settles it.
*Applies when: not (len(ctx.Repos) == 0) and len(ctx.Repos) > 0 and pinned*

 Do not retarget it to a starting point listed above: that is the project's current setting, which may have changed since this work began.
*Applies when: not (len(ctx.Repos) == 0) and len(ctx.Repos) > 0*



*Applies when: not (len(ctx.Repos) == 0)*



## Source: `writeProjectContext` — resource guidance

Apply when the current task supplies project resources. The project title, description, and resource listing remain current runtime inputs.

Project resources (also written to `.multica/project/resources.json`):

<current-project-resources>

Resources are pointers; open them only when relevant. For repository work, resolve the current issue or ancestor's `git_repo_root` metadata and run repository commands through `multica-subissue-v2 exec` in its prepared worktree. The `github_repo` resource does not require another checkout. Do not run `multica repo checkout` for query or untagged issues; that command creates a branch.

## Runtime input mapping

The repository `<line>` starts with `- <url>`, followed by ` — <description>` when present and ` (starts from <ref>)` when the trimmed ref is nonempty (the runtime renders the ref as inline code). `pinned` means at least one supplied repository has a nonempty trimmed ref. The sub-issue contract reference applies only when the `multica-platform` skill is actually installed; when it is absent, the original renderer adds no reference. Use the live workspace status catalog as already rendered by the backend, including exact keys, descriptions, and omission notices.

Use [multica-surface-delivery](../multica-surface-delivery/SKILL.md) for the original `## Comment Formatting`, `## Output`, and current-thread shell recipes referenced above. Native issue-comparison hints, comment deltas, role resolution, status rendering, and reply-target grouping stay in the backend. This text adds no read, write, callback, or receipt checkpoint beyond the original instructions.
