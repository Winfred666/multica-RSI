def child_reply(api, child):
    lines = []
    for key in ('git_repository', 'git_branch', 'git_parent_branch'):
        value = (child.get('metadata') or {}).get(key)
        if value:
            lines.append(key + ': ' + value)
    roots = api.request('GET', f"/api/issues/{child['id']}/comments?roots_only=true&summary=true")
    comments = {}
    for root in sorted(roots, key=lambda r: r.get('last_activity_at', r.get('created_at', '')))[-3:]:
        for comment in api.request('GET', f"/api/issues/{child['id']}/comments?thread={root['id']}&tail=30"):
            comments[comment['id']] = comment
    comments = sorted(comments.values(), key=lambda c: c.get('created_at', ''))
    replies = [c for c in comments if c.get('author_type') == 'agent' and
               c.get('author_id') == child.get('assignee_id') and not c.get('deleted_at') and c.get('content')]
    if replies:
        reply = replies[-1]
        lines.append('Child reply ' + reply['id'] + ':\n' + reply['content'][:6000])
    return '\n'.join(lines)


def transition(api, issue):
    headers = {}
    rows = api.request("GET", f"/api/issues/{issue['id']}/timeline", response_headers=headers)
    truncated = "activity" in headers.get("X-Timeline-Truncated", "").split(",")
    return next((row["id"] for row in reversed(rows)
                 if row.get("action") == "status_changed" and
                 row.get("details", {}).get("to") == issue["status"]), None if truncated else issue["id"])


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
            met = bool(children) and all(finished(child) for child in children)
            candidates = [(parent["id"], met,
                           sorted((child["id"], transition(api, child)) for child in children) if met else [],
                           "All sub-issues are finished.")]
        else:
            wanted = {"in_review": "in_review", "blocked": "blocked"}[hook]
            candidates = [(child["id"], category(child) == wanted,
                           transition(api, child) if category(child) == wanted else None,
                           f"Sub-issue {child.get('identifier', child['id'])} entered {wanted}.") for child in children]
        for entity, met, observation, fact in candidates:
            if not met:
                continue
            key = hook + ":" + entity
            previous = api.state(key)
            transitions = dict(observation) if hook == "all_finished" else {entity: observation}
            transitions = {issue_id: value or previous.get("transitions", {}).get(issue_id)
                           for issue_id, value in transitions.items()}
            if None in transitions.values():
                continue
            observation = sorted(transitions.items()) if hook == "all_finished" else transitions[entity]
            subject = fingerprint(observation)
            state = {"parent": parent["id"], "subject": subject, "transitions": transitions}
            if previous.get("parent") == parent["id"] and previous.get("subject") == subject:
                if previous.get("transitions") != transitions:
                    api.state(key, state)
                continue
            delivery = fingerprint([key, parent["id"], observation])
            if hook != 'all_finished':
                child = next(c for c in children if c['id'] == entity)
                fact += '\n' + child_reply(api, child)
            if api.wake(parent, delivery, fact + "\n\n" + prompt):
                api.state(key, state)
                count += 1
    return {"wakeups": count}


if __name__ == "__main__":
    run(main)
