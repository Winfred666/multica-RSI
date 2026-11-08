```mermaid
flowchart TD
    review["review"]
    handling["handling"]
    comparison["comparison"]
    waiting["waiting"]
    finished(["finished"])
    waiting -->|"review"| review
    review -->|"accept / resteer / cancel"| review
    review -->|"handle"| handling
    review -->|"compare"| comparison
    review -->|"wait"| waiting
    review -->|"done"| finished
    handling -->|"accept / resteer / cancel"| review
    handling -->|"handle"| handling
    handling -->|"compare"| comparison
    handling -->|"wait"| waiting
    handling -->|"done"| finished
    comparison -->|"accept / resteer / cancel"| review
    comparison -->|"handle"| handling
    comparison -->|"compare"| comparison
    comparison -->|"wait"| waiting
    comparison -->|"done"| finished
    comparison -->|"select"| review
```

Runtime assertion recovery: the current or target precheck enters `error`;
one `repaired` callback resumes the saved check without repeating a completed
action. A repeated assertion failure enters `blocked`. `quit` is available only
at the initial state and deletes the task record. Workflow exceptions retain state.

State: review

```mermaid
flowchart TD
    entry["Enter review"] --> check["precheck"]
    check --> view["Read-only view: review_view"]
    view --> recall["Dynamic choices: review_options"]
    recall --> target["Target precheck verifies action effects"]
    target --> comparison["Exit: comparison"]
    target --> finished["Exit: finished"]
    target --> handling["Exit: handling"]
    target --> review["Exit: review"]
    target --> waiting["Exit: waiting"]
```

State: handling

```mermaid
flowchart TD
    entry["Enter handling"] --> check["precheck"]
    check --> view["Read-only view: review_view"]
    view --> recall["Dynamic choices: review_options"]
    recall --> target["Target precheck verifies action effects"]
    target --> comparison["Exit: comparison"]
    target --> finished["Exit: finished"]
    target --> handling["Exit: handling"]
    target --> review["Exit: review"]
    target --> waiting["Exit: waiting"]
```

State: comparison

```mermaid
flowchart TD
    entry["Enter comparison"] --> check["precheck"]
    check --> view["Read-only view: comparison_view"]
    view --> recall["Dynamic choices: comparison_options"]
    recall --> target["Target precheck verifies action effects"]
    target --> comparison["Exit: comparison"]
    target --> finished["Exit: finished"]
    target --> handling["Exit: handling"]
    target --> review["Exit: review"]
    target --> waiting["Exit: waiting"]
```

State: waiting

```mermaid
flowchart TD
    entry["Enter waiting"] --> check["precheck"]
    check --> recall["review: no action"]
    recall --> target["Target precheck verifies action effects"]
    target --> review["Exit: review"]
```

State: finished

```mermaid
flowchart TD
    entry["Enter finished"] --> check["precheck"]
    check --> finished["Final: remove task record; return guidance"]
```

State: review

```mermaid
flowchart TD
    delivery[Native child delivery] --> commit{Reported commit matches?}
    commit -->|no| exception[Workflow exception; fresh delivery required]
    commit -->|yes| verdict[Review evidence and choose verdict]
    verdict --> accept[Accept: integration.merge then cleanup]
    accept --> settle[Post result and settle child]
    verdict --> rework[Return correction through native steering]
```
