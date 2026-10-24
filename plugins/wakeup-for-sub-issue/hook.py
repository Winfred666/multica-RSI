def main(api, payload):
    hook = payload["hook_key"]
    prompt = payload.get("config", {}).get(hook + "_prompt", "").strip()
    if not prompt:
        return {"skipped": "configure the wakeup prompt"}
    if payload.get("issue_id"):
        issue = api.issue(payload["issue_id"])
        parents = [api.issue(issue["parent_issue_id"])] if issue.get("parent_issue_id") else [issue]
    else:
        parent_ids = {row["parent_issue_id"] for row in api.issues() if row.get("parent_issue_id")}
        parents = [api.issue(parent_id) for parent_id in sorted(parent_ids)]
    count = 0
    for parent in parents:
        if not parent or finished(parent):
            continue
        children = api.children(parent["id"])
        if hook == "all_finished":
            candidates = [(parent["id"], bool(children) and all(finished(child) for child in children),
                           sorted((child["id"], child["updated_at"]) for child in children),
                           "All sub-issues are finished.")]
        else:
            wanted = {"in_review": "in_review", "blocked": "blocked"}[hook]
            candidates = [(child["id"], category(child) == wanted, child["updated_at"],
                           f"Sub-issue {child.get('identifier', child['id'])} entered {wanted}.") for child in children]
        for entity, met, observation, fact in candidates:
            key = hook + ":" + entity
            subject = fingerprint(sorted(child["id"] for child in children)) if hook == "all_finished" else entity
            previous = api.state(key)
            # shortcut: rearm on observed false; use durable transitions if every brief status flip must be delivered.
            if not met:
                if previous:
                    api.state(key, {})
                continue
            if previous.get("parent") == parent["id"] and previous.get("subject") == subject:
                continue
            delivery = fingerprint([key, parent["id"], observation])
            if api.wake(parent, delivery, fact + "\n\n" + prompt):
                api.state(key, {"parent": parent["id"], "subject": subject})
                count += 1
    return {"wakeups": count}


if __name__ == "__main__":
    run(main)
