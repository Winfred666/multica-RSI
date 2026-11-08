from datetime import datetime, timezone


INSTRUCTION = (
    "Review these items for deletion: remove stale discussion about fixed problems, old scores "
    "or rounds; keep useful guidance and results. Submit each id, revision and action "
    "(delete or keep), then repeat until complete. Do not rewrite or summarize comments."
)


def continuation(issue_id):
    return (
        "Continue. If you forget the reply style requirements, run "
        "`multica workspace get --output json | python3 -c 'import json,sys; "
        'print("\\n\\n".join(json.load(sys.stdin)["context"].strip().split("\\n\\n")[-2:]))'
        "'`. If you drift from the issue goal, run "
        f"`multica issue get {issue_id} --output json`."
    )


def candidates(api, issue_id, state):
    headers = {}
    rows = api.request("GET", f"/api/issues/{issue_id}/comments?fold=false", response_headers=headers)
    if any(value.lower() == "true" for key, value in headers.items() if key.lower() == "x-comments-truncated"):
        raise ValueError("comment inventory is truncated")
    by_id = {row["id"]: row for row in rows}
    threads = {}
    for row in rows:
        root = row
        while root.get("parent_id"):
            root = by_id[root["parent_id"]]
        threads.setdefault(root["id"], []).append(row)
    protected = set()
    for task in api.runs(issue_id, active=True):
        protected.add(task.get("trigger_comment_id"))
        for field in ("coalesced_comment_ids", "delivered_comment_ids", "supplement_comment_ids"):
            protected.update(task.get(field) or [])
    for row in rows:
        updated = datetime.fromisoformat(row["updated_at"].replace("Z", "+00:00"))
        if (updated > datetime.fromisoformat(state["cutoff"]) or row.get("supplement_status") in ("pending", "delivering")
                or any(receipt.get("status") in ("pending", "delivering") for receipt in row.get("supplements", []))):
            protected.add(row["id"])
    open_comments, closed_threads = [], []
    for root_id, thread in threads.items():
        discussion = sorted((row for row in thread if not row.get("deleted_at")), key=lambda row: (row["created_at"], row["id"]))
        if not discussion:
            continue
        closed = by_id[root_id].get("thread_resolved") or any(row.get("resolved_at") for row in discussion)
        if closed:
            if root_id not in state.get("kept", []) and not any(row["id"] in protected for row in discussion):
                closed_threads.append({"id": root_id, "revision": by_id[root_id]["revision"],
                    "content": discussion[-1]["content"], "attachments": discussion[-1].get("attachments", []),
                    "members": {row["id"]: row["revision"] for row in discussion}})
        else:
            for row in discussion:
                if row["id"] not in protected and row["id"] not in state.get("kept", []):
                    open_comments.append({"id": row["id"], "revision": row["revision"], "content": row["content"],
                        "attachments": row.get("attachments", []), "members": {row["id"]: row["revision"]}})
    return ("comments", open_comments) if open_comments else ("threads", closed_threads)


def completed(api, payload):
    issue_id = payload.get("issue_id")
    task_id = (payload.get("input") or {}).get("task_id")
    if not issue_id or not task_id:
        return {"scheduled": False}
    task = next((row for row in api.runs(issue_id) if row["id"] == task_id), {})
    count = (task.get("input_trend") or {}).get("context_compression_count")
    agent = task.get("agent_id")
    if task.get("status") != "completed" or not agent or type(count) is not int or count < 2:
        return {"scheduled": False}
    threshold = count // 2 * 2
    key = f"issue:{issue_id}:{agent}"
    if threshold <= api.state(key).get("threshold", 0):
        return {"scheduled": False}
    api.request("POST", f"/api/issues/{issue_id}/wakeups", {
        "agent_id": agent, "kind": "at", "after_seconds": 1,
        "idempotency_key": fingerprint([api.installation, issue_id, agent, threshold]),
        "instruction": continuation(issue_id) + f" Review old issue discussion with the Issue comments plugin hook for issue_id {issue_id}. "
                       + INSTRUCTION + " Do not change issue status or post a cleanup comment.",
    })
    api.state(key, {"threshold": threshold, "cutoff": datetime.now(timezone.utc).isoformat(), "kept": []})
    return {"scheduled": True, "threshold": threshold}


def main(api, payload):
    if payload.get("trigger") == "event" and payload.get("event_type") == "task.completed":
        return completed(api, payload)
    actor = payload.get("actor") or {}
    if payload.get("trigger") != "agent" or payload.get("hook_key") != "comments" or actor.get("type") != "agent":
        raise PermissionError("comment review requires an agent")
    data = payload.get("input") or {}
    issue = api.issue(data["issue_id"])
    key = f"issue:{issue['id']}:{actor['id']}"
    state = api.state(key)
    if not state.get("cutoff"):
        return {"items": [], "complete": True, "instruction": continuation(issue["id"])}
    decisions = data.get("decisions", [])
    if not isinstance(decisions, list) or len(decisions) > 5:
        raise ValueError("submit at most five decisions")
    batch = {row["id"]: row for row in state.get("batch", [])}
    if len({row["id"] for row in decisions}) != len(decisions):
        raise ValueError("submit one decision per item")
    for decision in decisions:
        mode, items = candidates(api, issue["id"], state)
        current = next((row for row in items if row["id"] == decision["id"]), None)
        saved = batch.get(decision["id"])
        if (not saved or current != saved or decision.get("revision") != saved["revision"]
                or decision.get("action") not in ("delete", "keep")):
            raise ValueError("item changed or decision is invalid; list items again")
        if decision["action"] == "delete":
            for comment_id in reversed(saved["members"]):
                api.request("DELETE", f"/api/comments/{comment_id}/keep-replies")
        else:
            state["kept"].append(decision["id"])
        api.state(key, state)
    mode, items = candidates(api, issue["id"], state)
    state["batch"] = items[:5]
    if not items:
        api.state(key, {"threshold": state["threshold"]})
        return {"items": [], "complete": True, "instruction": continuation(issue["id"])}
    api.state(key, state)
    return {"mode": mode, "items": [{k: v for k, v in row.items() if k != "members"} for row in items[:5]],
            "complete": False, "instruction": INSTRUCTION}


if __name__ == "__main__":
    run(main)
