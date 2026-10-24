def main(api, payload):
    prompt = payload.get("config", {}).get("prompt", "").strip()
    if not prompt:
        return {"skipped": "configure the continuation prompt"}
    issue_id = payload.get("issue_id")
    task_id = payload.get("input", {}).get("task_id")
    if payload.get("event_type") != "task.completed" or not issue_id or not task_id:
        return {"continued": 0}
    issue = api.issue(issue_id)
    if not issue or category(issue) != "in_progress":
        return {"continued": 0}
    agent = api.target(issue)
    if not agent or api.runs(issue_id, active=True):
        return {"continued": 0}
    if any(not finished(child) for child in api.children(issue_id)):
        return {"continued": 0}
    if any(rule["enabled"] for rule in api.request("GET", f"/api/issues/{issue_id}/wakeups")):
        return {"continued": 0}
    runs = api.runs(issue_id)
    if not runs:
        return {"continued": 0}
    latest = max(runs, key=lambda row: row["created_at"])
    if latest["id"] != task_id or latest["status"] != "completed":
        return {"continued": 0}
    state_key = "issue:" + issue_id
    if api.state(state_key).get("last_run") == task_id:
        return {"continued": 0}
    try:
        api.request("POST", f"/api/issues/{issue_id}/comments", {
            "content": f"[@Agent](mention://agent/{agent})\n\n{prompt}", "type": "comment",
        })
    except PermissionError:
        return {"continued": 0}
    api.state(state_key, {"last_run": task_id})
    return {"continued": 1}


if __name__ == "__main__":
    run(main)
