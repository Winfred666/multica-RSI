```mermaid
flowchart TD
    ready["ready"]
    finished(["finished"])
    ready -->|"create"| finished
```

Runtime assertion recovery: the current or target precheck enters `error`;
one `repaired` callback resumes the saved check without repeating a completed
action. A repeated assertion failure enters `blocked`. `quit` is available only
at the initial state and deletes the task record. Workflow exceptions retain state.

State: ready

```mermaid
flowchart TD
    entry["Enter ready"] --> check["precheck"]
    check --> recall["create: create_action"]
    recall --> target["Target precheck verifies action effects"]
    target --> finished["Exit: finished"]
```

State: finished

```mermaid
flowchart TD
    entry["Enter finished"] --> check["precheck"]
    check --> finished["Final: remove task record; return guidance"]
```
