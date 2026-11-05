---
name: multica-subissue-v2
description: Create query, dev or exp children; prepare their inherited sandbox/workdir, execute commands there and report measured results.
---

Use for child creation and workdir preparation in a native Multica task. Before
each operation, initialize or resume its guidance:

```sh
python3 <this-skill-directory>/wrapper.py
```

Follow the returned options. `create --spec-file child.json [--tag query|dev|exp]`
prepares one child; add `--start` to dispatch its assigned agent. The JSON spec needs
`title` and permits `description_file`, `project`, `stage`, `priority`, `due_date`
and `assignee_id`. Starting requires `assignee_id`.

`query` is the default, including when no lifecycle label is present. It shares the
parent's workdir; `--shared-dir FOLDER` selects a subfolder. In the child, `prepare`
exposes that workdir as `shared/`; use relative paths through it. `exec -- COMMAND
ARGS` runs there under the inherited sandbox without owning a new branch or
locking shared query work. `dev` and `exp` own branches from the parent's branch.
For branch work, supply `--repo` only if issue ancestry and the current checkout
cannot identify one repository.

The first `exp` needs `--baseline-score SCORE --direction min|max`; matching siblings
reuse those values. Competitive experiments also need one fresh
`--experiment-group GROUP --experiment-group-size COUNT`. Keep that count fixed and
create every member before waiting. Ungrouped children report independently.

For delivery, load `multica-surface-delivery` and run its installed `wrapper.py` and follow
its choices. To resume a delivery receipt, use `report --answers-file FILE --thread
ID [--score SCORE]` with passed measured answers and their evidence thread; `exp`
requires a finite measured score. Completion returns `finished`; another operation
starts with a bare wrapper call.

Use `multica-surface-block` for an external blocker. Parent review uses
`multica-subissue-review`; its returned `review_settle.py` command owns child
acceptance and branch settlement.
