```mermaid
flowchart TD
    check["check"]
    commit["commit"]
    report["report"]
    finished(["finished"])
    commit -->|"committed"| report
    commit -->|"recheck"| check
    report -->|"publish"| finished
    report -->|"recheck"| check
    check -->|"verified: query"| report
    check -->|"verified: dev / exp"| commit
```

Runtime assertion recovery: the current or target precheck enters `error`;
one `repaired` callback resumes the saved check without repeating a completed
action. A repeated assertion failure enters `blocked`. `quit` is available only
at the initial state and deletes the task record. Workflow exceptions retain state.

State: check

```mermaid
flowchart TD
    entry["Enter check"] --> check["precheck"]
    check --> view["Read-only view: check_view"]
    view --> recall["Dynamic choices: verified_options"]
    recall --> target["Target precheck verifies action effects"]
    target --> commit["Exit: commit"]
    target --> report["Exit: report"]
```

State: commit

```mermaid
flowchart TD
    entry["Enter commit"] --> check["precheck"]
    check --> view["Read-only view: commit_view"]
    view --> recall["committed: committed; recheck: recheck"]
    recall --> target["Target precheck verifies action effects"]
    target --> check["Exit: check"]
    target --> report["Exit: report"]
```

State: report

```mermaid
flowchart TD
    entry["Enter report"] --> check["precheck"]
    check --> recall["publish: publish; recheck: recheck"]
    recall --> target["Target precheck verifies action effects"]
    target --> check["Exit: check"]
    target --> finished["Exit: finished"]
```

State: finished

```mermaid
flowchart TD
    entry["Enter finished"] --> check["precheck"]
    check --> finished["Final: remove task record; return guidance"]
```
