```mermaid
flowchart TD
    confirm["confirm"]
    describe["describe"]
    finished(["finished"])
    confirm -->|"confirm"| describe
    describe -->|"continue"| finished
```

Runtime assertion recovery: the current or target precheck enters `error`;
one `repaired` callback resumes the saved check without repeating a completed
action. A repeated assertion failure enters `blocked`. `quit` is available only
at the initial state and deletes the task record. Workflow exceptions retain state.

State: confirm

```mermaid
flowchart TD
    entry["Enter confirm"] --> check["precheck"]
    check --> recall["confirm: no action"]
    recall --> target["Target precheck verifies action effects"]
    target --> describe["Exit: describe"]
```

State: describe

```mermaid
flowchart TD
    entry["Enter describe"] --> check["precheck"]
    check --> recall["continue: block"]
    recall --> target["Target precheck verifies action effects"]
    target --> finished["Exit: finished"]
```

State: finished

```mermaid
flowchart TD
    entry["Enter finished"] --> check["precheck"]
    check --> finished["Final: remove task record; return guidance"]
```

State: describe

```mermaid
flowchart TD
    input[Reason and parent thread] --> child[Publish child blocker]
    child --> status[Persist blocked status]
    status --> parent{Has parent?}
    parent -->|yes| notify[Post help request with parent owner mention]
    notify --> dispatched{Native dispatch confirmed?}
    dispatched -->|no| exception[Workflow exception; reconcile saved receipt]
    dispatched -->|yes| finished[Exit: finished]
    parent -->|no| finished
```
