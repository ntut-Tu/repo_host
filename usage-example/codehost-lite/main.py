"""Edit the constants below, then run this file as shown in codehost-lite/RUNBOOK.md."""

import json
import ssl
from dataclasses import asdict

import httpx
from codehost_lite import GitHub, GitLab

# GitHub: real public demo resources; an empty token uses unauthenticated reads.
GITHUB_API_URL = "https://api.github.com"
GITHUB_REPOSITORY = "ntut-Tu/cloth_shop_server"
GITHUB_TOKEN = ""
GITHUB_PR_NUMBER = 31
GITHUB_ISSUE_NUMBER = 29
GITHUB_COMMIT_REF = "dff33b95920be90cdbd4ac555653366b6dcc6610"

# GitLab: replace these illustrative values before setting RUN_GITLAB = True.
# Include a reverse-proxy prefix if needed, e.g. https://host/gitlab/api/v4.
RUN_GITLAB = False
GITLAB_API_URL = "https://gitlab.example.com/api/v4"
GITLAB_REPOSITORY = "team/subgroup/demo-project"
GITLAB_TOKEN = ""
GITLAB_MR_IID = 1
GITLAB_ISSUE_IID = 2
GITLAB_COMMIT_REF = "main"
GITLAB_CA_FILE = ""  # Optional absolute path to your internal CA PEM file.


def main() -> None:
    """Call every resource API directly; clients own and close their HTTP connections."""
    with httpx.Client(timeout=30.0) as client:
        github = GitHub(
            GITHUB_REPOSITORY,
            client,
            token=GITHUB_TOKEN,
            api_url=GITHUB_API_URL,
        )
        print(
            "GitHub PR:",
            json.dumps(asdict(github.get_pr(GITHUB_PR_NUMBER)), ensure_ascii=False),
        )
        print(
            "GitHub issue:",
            json.dumps(asdict(github.get_issue(GITHUB_ISSUE_NUMBER)), ensure_ascii=False),
        )
        print(
            "GitHub commit:",
            json.dumps(asdict(github.get_commit(GITHUB_COMMIT_REF)), ensure_ascii=False),
        )

    if RUN_GITLAB:
        tls = ssl.create_default_context(cafile=GITLAB_CA_FILE or None)
        with httpx.Client(timeout=30.0, verify=tls) as client:
            gitlab = GitLab(
                GITLAB_REPOSITORY,
                client,
                token=GITLAB_TOKEN,
                api_url=GITLAB_API_URL,
            )
            print(
                "GitLab MR:",
                json.dumps(asdict(gitlab.get_pr(GITLAB_MR_IID)), ensure_ascii=False),
            )
            print(
                "GitLab issue:",
                json.dumps(asdict(gitlab.get_issue(GITLAB_ISSUE_IID)), ensure_ascii=False),
            )
            print(
                "GitLab commit:",
                json.dumps(asdict(gitlab.get_commit(GITLAB_COMMIT_REF)), ensure_ascii=False),
            )


if __name__ == "__main__":
    main()
