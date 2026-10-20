def main(api, payload):
    prompt = payload.get("config", {}).get("prompt", "").strip()
    if not prompt:
        return {"skipped": "configure the continuation prompt"}
    count = 0
    for issue in api.issues():
        if category(issue) != "in_progress" or not api.target(issue):
            continue
        if api.runs(issue["id"], active=True) or any(not finished(child) for child in api.children(issue["id"])):
            continue
        rules = api.request("GET", f"/api/issues/{issue['id']}/wakeups")
        if any(rule["enabled"] for rule in rules):
            continue
        runs = api.runs(issue["id"])
        if not runs:
            continue
        latest = max(runs, key=lambda row: row["created_at"])
        if latest["status"] != "completed" or not latest.get("completed_at"):
            continue
        ended = datetime.fromisoformat(latest["completed_at"].replace("Z", "+00:00")).timestamp()
        if time.time() - ended < 300:
            continue
        key = "continue:" + latest["id"]
        state_key = "issue:" + issue["id"]
        if api.state(state_key).get("last_run") == latest["id"]:
            continue
        current = api.issue(issue["id"])
        if not current or category(current) != "in_progress":
            continue
        if api.wake(current, key, prompt):
            api.state(state_key, {"last_run": latest["id"]})
            count += 1
    return {"wakeups": count}


if __name__ == "__main__":
    run(main)
