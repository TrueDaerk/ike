"""Fetch open GitHub issues, sort them by label priority (bug > qol > other),
and run a Claude Code instance for each one sequentially."""

import json
import subprocess
import time

WAIT_BETWEEN_ISSUES = 120  # seconds


def fetch_issues():
    result = subprocess.run(
        ["gh", "issue", "list", "--state", "open", "--json", "number,title,labels,body"],
        capture_output=True,
        text=True,
        check=True,
    )
    return json.loads(result.stdout)


def priority(issue):
    labels = {label["name"].lower() for label in issue.get("labels", [])}
    if "bug" in labels:
        return 0
    if "qol" in labels:
        return 1
    return 2


def handle_issue(issue):
    number = issue["number"]
    prompt = (
        f"Bitte kümmere dich um den Issue #{number}: {issue['title']}\n\n"
        f"Labels: {', '.join(label['name'] for label in issue.get('labels', []))}\n\n"
        f"Issue-Beschreibung:\n{issue.get('body') or '(keine Beschreibung)'}\n\n"
        "Wenn du fertig bist, committe, pushe, mach einen PR und merge in main"
    )
    labels = ", ".join(label["name"] for label in issue.get("labels", [])) or "-"
    body = issue.get("body") or "(keine Beschreibung)"
    print(f"=== Issue #{number}: {issue['title']} ===")
    print(f"Labels: {labels}")
    print(f"--- Beschreibung ---\n{body}\n--------------------")
    subprocess.run(["claude", "-p", prompt, "--permission-mode", "acceptEdits"])


def main():
    issues = sorted(fetch_issues(), key=priority)
    print(f"{len(issues)} offene Issues gefunden.")
    for i, issue in enumerate(issues):
        handle_issue(issue)
        if i < len(issues) - 1:
            print(f"Warte {WAIT_BETWEEN_ISSUES} Sekunden vor dem nächsten Issue...")
            time.sleep(WAIT_BETWEEN_ISSUES)


if __name__ == "__main__":
    main()
