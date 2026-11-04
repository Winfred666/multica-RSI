def main(api, payload):
    prompt = payload.get("config", {}).get("prompt", "").strip()
    if not prompt:
        return {"skipped": "configure the continuation prompt"}
    issue_id = payload.get("issue_id")
    task_id = payload.get("input", {}).get("task_id")
    status = {"task.completed": "completed", "task.failed": "failed"}.get(payload.get("event_type"))
    if not status or not issue_id or not task_id:
        return {"continued": 0}
    source = api.issue(issue_id)
    if not source:
        return {"continued": 0}
    issue = source
    if category(source) in {"in_review", "blocked", "done", "cancelled"} and source.get("parent_issue_id"):
        issue = api.issue(source["parent_issue_id"])
    if not issue or category(issue) != "in_progress":
        return {"continued": 0}
    target_id = issue["id"]
    agent = api.target(issue)
    if not agent or api.runs(target_id, active=True):
        return {"continued": 0}
    if any(api.runs(child["id"], active=True) for child in api.children(target_id)
           if not finished(child) and category(child) not in {"in_review", "blocked"}):
        return {"continued": 0}
    if any(rule["enabled"] for rule in api.request("GET", f"/api/issues/{target_id}/wakeups")):
        return {"continued": 0}
    runs = api.runs(issue_id)
    if not runs:
        return {"continued": 0}
    latest = max(runs, key=lambda row: row["created_at"])
    if latest["id"] != task_id or latest["status"] != status:
        return {"continued": 0}
    state_key = "issue:" + issue_id
    receipt = api.state(state_key)
    if receipt.get("last_run") == task_id:
        return {"continued": 0}
    thread_id = api.continuation_thread(issue_id, task_id, prompt, receipt)
    if not api.wake(issue, "ralph:" + task_id, prompt,
                    parent_comment_id=thread_id if target_id == issue_id else None):
        return {"continued": 0}
    api.state(state_key, {"last_run": task_id})
    return {"continued": 1}


if __name__ == "__main__":
    run(main)
