"""Explicit, read-only live check against the user's demo repository; never auto-discovered."""

import httpx
from codehost_lite import GitHub

REPOSITORY = "ntut-Tu/cloth_shop_server"
TOKEN = ""  # Optional local token; never commit a real credential.
PR_NUMBER = 31
ISSUE_NUMBER = 29
COMMIT_SHA = "dff33b95920be90cdbd4ac555653366b6dcc6610"


def main() -> None:
    """Verify real PR, issue and commit identities using three GET requests."""
    with httpx.Client(timeout=30.0) as client:
        github = GitHub(REPOSITORY, client, token=TOKEN)
        pr = github.get_pr(PR_NUMBER)
        issue = github.get_issue(ISSUE_NUMBER)
        commit = github.get_commit(COMMIT_SHA)
    assert pr.number == PR_NUMBER and pr.head_sha and pr.title
    assert issue.number == ISSUE_NUMBER and issue.title
    assert commit.sha == COMMIT_SHA and commit.message
    assert pr.url == f"https://github.com/{REPOSITORY}/pull/{PR_NUMBER}"
    assert issue.url == f"https://github.com/{REPOSITORY}/issues/{ISSUE_NUMBER}"
    assert commit.url == f"https://github.com/{REPOSITORY}/commit/{COMMIT_SHA}"
    print(f"PASS: GitHub PR #{pr.number}, issue #{issue.number}, commit {commit.sha}")


if __name__ == "__main__":
    main()
