def compression_count(runs, agent):
    rows = [row for row in runs if row.get("agent_id") == agent
            and row["status"] == "completed" and row.get("input_trend")]
    if not rows:
        return None
    latest = max(rows, key=lambda row: row["created_at"])
    count = latest["input_trend"].get("context_compression_count")
    return count if type(count) is int and count >= 0 else None


def comment_candidates(api, issue_id, state):
    headers = {}
    rows = api.request("GET", f"/api/issues/{issue_id}/comments?fold=true", response_headers=headers)
    if headers.get("X-Comments-Truncated", "").lower() == "true":
        raise ValueError("comment list is truncated; cleanup requires a complete thread window")
    by_id = {row["id"]: row for row in rows}
    threads = {}
    for row in rows:
        root = row
        visited = set()
        while root.get("parent_id"):
            if root["id"] in visited or root["parent_id"] not in by_id:
                root = None
                break
            visited.add(root["id"])
            root = by_id[root["parent_id"]]
        if root is not None:
            threads.setdefault(root["id"], []).append(row)
    protected = set()
    for task in api.runs(issue_id, active=True):
        protected.add(task.get("trigger_comment_id"))
        for field in ("coalesced_comment_ids", "delivered_comment_ids", "supplement_comment_ids"):
            protected.update(task.get(field) or [])
    candidates = []
    for root_id, comments in threads.items():
        if by_id[root_id].get("thread_resolved") or any(row.get("resolved_at") for row in comments):
            continue
        discussion = [row for row in comments if row.get("author_type") != "system" and not row.get("deleted_at")]
        latest = max(discussion, key=lambda row: (row["created_at"], row["id"]), default=None)
        for row in comments:
            if (row.get("deleted_at") or row["id"] in protected or row == latest
                    or row["id"] in state.get("preserved", [])
                    or max(row["created_at"], row["updated_at"]) > state["cutoff"]
                    or row.get("supplement_status") in ("pending", "delivering")
                    or any(receipt.get("status") in ("pending", "delivering") for receipt in row.get("supplements", []))):
                continue
            candidates.append(row)
    return candidates


def continuation(api, issue, task_id):
    task = next((row for row in api.runs(issue["id"], active=True) if row["id"] == task_id), {})
    comment_ids = set(task.get("delivered_comment_ids") or [])
    comment_ids.update(task.get("coalesced_comment_ids") or [])
    if task.get("trigger_comment_id"):
        comment_ids.add(task["trigger_comment_id"])
    if comment_ids:
        rows = api.request("GET", f"/api/issues/{issue['id']}/comments?fold=true")
        steering = [row for row in rows if row["id"] in comment_ids and not row.get("deleted_at")]
        if steering:
            latest = max(steering, key=lambda row: (row["updated_at"], row["created_at"], row["id"]))
            return "Continue the latest instruction:\n" + latest["content"]
        return "Continue the latest triggering or delivered instruction already in this run's context."
    if task.get("handoff_note"):
        return "Continue the instruction that started this run:\n" + task["handoff_note"]
    return "Continue solving the issue."


def review(api, payload, issue, key, state, task_id):
    if payload.get("actor") != {"type": "agent", "id": issue["assignee_id"]}:
        raise PermissionError("only the current assignee can review comments")
    data = payload.get("input") or {}
    decisions = data.get("decisions", [])
    if not isinstance(decisions, list) or len(decisions) > 20:
        raise ValueError("submit at most 20 comment decisions")
    ids = set()
    for decision in decisions:
        if not isinstance(decision, dict) or not isinstance(decision.get("id"), str):
            raise ValueError("each decision requires a comment id")
        if decision["id"] in ids or decision.get("action") not in ("delete", "update", "preserve"):
            raise ValueError("use one delete, update, or preserve decision per comment")
        ids.add(decision["id"])
        if type(decision.get("revision")) is not int or decision["revision"] < 1:
            raise ValueError("use the returned comment revision")
        if decision["action"] == "update":
            content = decision.get("content")
            if (not isinstance(content, str) or not content.strip() or len(content) > 500
                    or "\n" in content or "://" in content or "www." in content or "](" in content):
                raise ValueError("update content must be one short plain-text sentence without links")
    for decision in decisions:
        current = api.issue(issue["id"])
        if current.get("assignee_type") != "agent" or current.get("assignee_id") != issue["assignee_id"]:
            raise ValueError("issue assignee changed")
        candidates = {row["id"]: row for row in comment_candidates(api, issue["id"], state)}
        row = candidates.get(decision["id"])
        if not row or row.get("author_type") == "system" or row["revision"] != decision["revision"]:
            raise ValueError("comment changed or is protected; list comments again")
        if decision["action"] == "delete":
            api.request("DELETE", f"/api/comments/{row['id']}/keep-replies")
        elif decision["action"] == "update":
            api.request("PUT", f"/api/comments/{row['id']}", {
                "content": decision["content"].strip(), "attachment_ids": [],
                "expected_revision": row["revision"],
            })
        if decision["action"] != "delete":
            state.setdefault("preserved", []).append(row["id"])
        api.state(key, state)
    candidates = comment_candidates(api, issue["id"], state)
    for row in candidates:
        if row.get("author_type") != "system":
            continue
        # Recheck active-run inputs immediately before each destructive call.
        current = {c["id"]: c for c in comment_candidates(api, issue["id"], state)}.get(row["id"])
        if current and current["revision"] == row["revision"]:
            api.request("DELETE", f"/api/comments/{row['id']}/keep-replies")
    candidates = [row for row in comment_candidates(api, issue["id"], state) if row.get("author_type") != "system"]
    if candidates:
        return {"comments": [{k: row.get(k) for k in ("id", "revision", "parent_id", "author_type", "content", "attachments")}
                             for row in candidates[:20]], "complete": False}
    count = compression_count(api.runs(issue["id"]), issue["assignee_id"])
    if count is None:
        raise ValueError("assignee compression count unavailable")
    instruction = continuation(api, issue, task_id)
    api.state(key, {"baseline": count})
    return {"comments": [], "complete": True, "instruction": instruction}


def main(api, payload):
    data = payload.get("input") or {}
    issue_id = data.get("issue_id")
    if payload.get("trigger") != "agent" or payload.get("hook_key") != "comments" or not issue_id:
        raise ValueError("comment review requires an assignee agent and issue UUID")
    issue = api.issue(issue_id)
    if not issue or issue.get("assignee_type") != "agent":
        raise ValueError("issue must have an agent assignee")
    agent = issue["assignee_id"]
    if payload.get("actor") != {"type": "agent", "id": agent}:
        raise PermissionError("only the current assignee can review comments")
    runs = api.runs(issue["id"])
    active = [row for row in runs if row.get("agent_id") == agent and row["status"] == "running"]
    if len(active) != 1:
        raise ValueError("cleanup requires the assignee's current running task")
    task = active[0]
    key = f"issue:{issue['id']}:{agent}"
    state = api.state(key)
    count = compression_count(runs, agent)
    if "cutoff" not in state:
        if data.get("decisions"):
            raise ValueError("list comments before submitting decisions")
        if finished(issue) or category(issue) == "backlog" or count is None or count - state.get("baseline", 0) < 2:
            return {"comments": [], "complete": True, "instruction": continuation(api, issue, task["id"])}
        state = {"baseline": state.get("baseline", 0), "cutoff": task.get("started_at") or task["created_at"]}
        api.state(key, state)
    return review(api, payload, issue, key, state, task["id"])


if __name__ == "__main__":
    run(main)
