"""Offline regression tests; GitHub fixtures come from ntut-Tu/cloth_shop_server."""

import json
import unittest
from pathlib import Path

import httpx
from codehost_lite import CodeHostError, GitHub, GitLab

FIXTURES = Path(__file__).parent / "fixtures"
REPOSITORY = "ntut-Tu/cloth_shop_server"
SHA = "dff33b95920be90cdbd4ac555653366b6dcc6610"


def github_fixture(name: str) -> dict:
    return json.loads((FIXTURES / f"github_{name}.json").read_text())


class GitHubTests(unittest.TestCase):
    def test_all_resources_and_no_prefetch_or_cache(self):
        requests = []
        pr_data = github_fixture("pr")
        issue_data = github_fixture("issue")
        commit_data = github_fixture("commit")
        responses = {
            "/repos/ntut-Tu/cloth_shop_server/pulls/31": pr_data,
            "/repos/ntut-Tu/cloth_shop_server/issues/29": issue_data,
            f"/repos/ntut-Tu/cloth_shop_server/commits/{SHA}": commit_data,
        }

        def respond(request):
            requests.append(request)
            self.assertEqual(request.method, "GET")
            self.assertEqual(request.headers["Authorization"], "Bearer example-token")
            self.assertEqual(request.headers["X-GitHub-Api-Version"], "2022-11-28")
            return httpx.Response(200, json=responses[request.url.path])

        with httpx.Client(transport=httpx.MockTransport(respond)) as client:
            github = GitHub(REPOSITORY, client, token="example-token")
            pr = github.get_pr(31)
            issue = github.get_issue(29)
            commit = github.get_commit(SHA)
            pr_data["title"] = "Changed on the server"
            self.assertEqual(github.get_pr(31).title, "Changed on the server")
            self.assertEqual(github.get_issue(29), issue)
            self.assertEqual(github.get_commit(SHA), commit)
        self.assertEqual(len(requests), 6)
        self.assertEqual(pr.number, 31)
        self.assertEqual(pr.head_sha, pr_data["head"]["sha"])
        self.assertEqual(pr.source_branch, pr_data["head"]["ref"])
        self.assertEqual(pr.target_branch, pr_data["base"]["ref"])
        self.assertEqual(issue.number, 29)
        self.assertEqual(issue.description, issue_data["body"] or "")
        self.assertEqual(commit.sha, SHA)
        self.assertEqual(commit.message, commit_data["commit"]["message"])
        self.assertEqual(commit.committed_at, commit_data["commit"]["committer"]["date"])

    def test_merged_pr_and_null_body(self):
        data = github_fixture("pr") | {"merged": True, "state": "closed", "body": None}
        with httpx.Client(
            transport=httpx.MockTransport(lambda _: httpx.Response(200, json=data))
        ) as client:
            pr = GitHub(REPOSITORY, client).get_pr(31)
        self.assertEqual(pr.state, "merged")
        self.assertEqual(pr.description, "")

    def test_issue_endpoint_must_not_silently_return_a_pr(self):
        data = github_fixture("issue") | {"pull_request": {"url": "https://example.com/pr"}}
        with httpx.Client(
            transport=httpx.MockTransport(lambda _: httpx.Response(200, json=data))
        ) as client:
            with self.assertRaisesRegex(CodeHostError, "use get_pr"):
                GitHub(REPOSITORY, client).get_issue(31)

    def test_enterprise_root_and_branch_ref(self):
        def respond(request):
            self.assertEqual(
                str(request.url),
                "https://github.example.com/api/v3/repos/ntut-Tu/cloth_shop_server/commits/feature%2Fdemo",
            )
            self.assertNotIn("authorization", request.headers)
            return httpx.Response(200, json=github_fixture("commit"))

        with httpx.Client(transport=httpx.MockTransport(respond)) as client:
            github = GitHub(REPOSITORY, client, api_url="https://github.example.com/api/v3/")
            self.assertEqual(github.get_commit("feature/demo").sha, SHA)


class GitLabTests(unittest.TestCase):
    def test_self_managed_prefix_nested_project_auth_and_all_resources(self):
        requests = []
        responses = {
            "merge_requests/7": {
                "id": 999,
                "iid": 7,
                "title": "Demo MR",
                "description": None,
                "state": "opened",
                "web_url": "https://gitlab.example.com/mr/7",
                "sha": SHA,
                "source_branch": "feature/demo",
                "target_branch": "main",
            },
            "issues/8": {
                "id": 888,
                "iid": 8,
                "title": "Demo issue",
                "description": "Issue body",
                "state": "closed",
                "web_url": "https://gitlab.example.com/issue/8",
            },
            "repository/commits/release%2Fdemo": {
                "id": SHA,
                "message": "Demo commit",
                "committed_date": "2026-10-16T00:00:00Z",
                "author_name": "Developer",
                "web_url": "https://gitlab.example.com/commit",
            },
        }
        root = "https://gitlab.example.com/gitlab/api/v4/projects/team%2Fsubgroup%2Fdemo/"

        def respond(request):
            requests.append(request)
            self.assertEqual(request.method, "GET")
            self.assertTrue(str(request.url).startswith(root))
            self.assertEqual(request.headers["PRIVATE-TOKEN"], "example-token")
            self.assertNotIn("authorization", request.headers)
            path = str(request.url).removeprefix(root)
            return httpx.Response(200, json=responses[path])

        with httpx.Client(transport=httpx.MockTransport(respond)) as client:
            gitlab = GitLab(
                "team/subgroup/demo",
                client,
                token="example-token",
                api_url="https://gitlab.example.com/gitlab/api/v4/",
            )
            pr = gitlab.get_pr(7)
            issue = gitlab.get_issue(8)
            commit = gitlab.get_commit("release/demo")
            responses["merge_requests/7"]["state"] = "merged"
            self.assertEqual(gitlab.get_pr(7).state, "merged")
            self.assertEqual(gitlab.get_issue(8), issue)
            self.assertEqual(gitlab.get_commit("release/demo"), commit)
            responses["merge_requests/7"]["sha"] = None
            self.assertIsNone(gitlab.get_pr(7).head_sha)
        self.assertEqual(len(requests), 7)
        self.assertEqual(pr.number, 7)  # Use IID, not the global ID 999.
        self.assertEqual(pr.state, "open")
        self.assertEqual(pr.description, "")
        self.assertEqual(pr.head_sha, SHA)
        self.assertEqual(pr.source_branch, "feature/demo")
        self.assertEqual(issue.number, 8)
        self.assertEqual(issue.state, "closed")
        self.assertEqual(commit.sha, SHA)
        self.assertEqual(commit.author_name, "Developer")


class FailureTests(unittest.TestCase):
    def test_http_errors_are_safe_and_keep_status(self):
        for provider in (GitHub, GitLab):
            for status in (301, 401, 403, 404, 429, 500):
                with self.subTest(provider=provider, status=status):
                    calls = []

                    def respond(request):
                        calls.append(request)
                        return httpx.Response(
                            status,
                            text="secret-upstream-body",
                            headers={"location": "https://other.example.com"},
                        )

                    with httpx.Client(
                        transport=httpx.MockTransport(respond),
                        follow_redirects=True,
                    ) as client:
                        api = provider(REPOSITORY, client, token="secret-token")
                        with self.assertRaises(CodeHostError) as raised:
                            api.get_issue(29)
                    self.assertEqual(raised.exception.status_code, status)
                    self.assertNotIn("secret", str(raised.exception))
                    self.assertEqual(len(calls), 1)  # No redirects or automatic retries.

    def test_invalid_json_and_incomplete_resource(self):
        for body in (b"not-json", b"[]", b"{}", b'{"number": 31}'):
            with self.subTest(body=body):
                with httpx.Client(
                    transport=httpx.MockTransport(lambda _: httpx.Response(200, content=body)),
                ) as client:
                    with self.assertRaisesRegex(CodeHostError, "unexpected resource"):
                        GitHub(REPOSITORY, client).get_pr(31)

    def test_timeouts_and_connection_errors(self):
        for failure in (httpx.ConnectError, httpx.ReadTimeout):
            with self.subTest(failure=failure):

                def respond(request):
                    raise failure("secret-url-and-token", request=request)

                with httpx.Client(transport=httpx.MockTransport(respond)) as client:
                    with self.assertRaises(CodeHostError) as raised:
                        GitLab(REPOSITORY, client).get_commit("main")
                self.assertIsNone(raised.exception.status_code)
                self.assertNotIn("secret", str(raised.exception))

    def test_bad_inputs_do_not_send_requests(self):
        def unexpected(request):
            self.fail("Invalid input must not reach the server")

        with httpx.Client(transport=httpx.MockTransport(unexpected)) as client:
            for provider in (GitHub, GitLab):
                api = provider(REPOSITORY, client)
                for number in (0, -1, True, "31"):
                    with self.assertRaises(ValueError):
                        api.get_pr(number)
                    with self.assertRaises(ValueError):
                        api.get_issue(number)
                for ref in ("", "..", ".", "bad ref"):
                    with self.assertRaises(ValueError):
                        api.get_commit(ref)
                for repo in ("repo", "/repo", "owner/", "owner/../repo"):
                    with self.assertRaises(ValueError):
                        provider(repo, client)
                for root in ("file:///tmp", "https://token@host/api", "https://host/api?x=1"):
                    with self.assertRaises(ValueError):
                        provider(REPOSITORY, client, api_url=root)


if __name__ == "__main__":
    unittest.main()
