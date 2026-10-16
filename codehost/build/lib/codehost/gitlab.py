"""GitLab API transport, pagination and repository reads."""

from __future__ import annotations

from urllib.parse import quote, urlencode

from codehost.errors import IncompleteResponseError, RemoteError
from codehost.gitlab_resources import GitLabResources
from codehost.http import HttpClient
from codehost.models import ChangedFile, PullRequest, TreeEntry


class GitLabApi:
    """Share GitLab API configuration across MR and test retrieval adapters."""

    def __init__(
        self, project: str, token: str | None, client: HttpClient,
        api_url: str = "https://gitlab.com/api/v4",
    ) -> None:
        """Configure a GitLab project API connection.

        Args:
            project: Project path or numeric project ID represented as a string.
            token: PRIVATE-TOKEN credential, or None for unauthenticated access.
            client: Shared HTTP transport.
            api_url: API root including /api/v4 for self-managed GitLab.
        """
        self.project = project
        self.repository = project
        self.api_url = api_url.rstrip("/")
        self.client = client
        self.headers = {"Accept": "application/json"}
        if token:
            self.headers["PRIVATE-TOKEN"] = token
        self.resources = GitLabResources(self)

    def get_commit(self, sha: str):
        """Return one exact commit, using the SHA cache when available."""
        return self.resources.get_commit(sha)

    def get_pr(self, number: int):
        """Return merge request metadata and prefetch related resources."""
        return self.resources.get_pr(number)

    def get_issue(self, number: int):
        """Return issue metadata and linked merge request numbers."""
        return self.resources.get_issue(number)

    def get_pr_timeline(self, number: int):
        """Return time-ordered commits, comments, and merge marker."""
        return self.resources.get_pr_timeline(number)

    def root(self, project: str | None = None) -> str:
        """Build an encoded project API URL.

        Args:
            project: Source project override, such as an MR's fork.

        Returns:
            API root for the override or configured project.
        """
        return f"{self.api_url}/projects/{quote(project or self.project, safe='')}"

    def get(self, path: str, project: str | None = None):
        """Fetch a project-relative API resource as JSON.

        Args:
            path: Endpoint path beginning with '/', optionally with a query.
            project: Optional source project override.

        Returns:
            Decoded JSON response.

        Raises:
            RemoteError: Request or JSON decoding fails.
        """
        return self.client.get(self.root(project) + path, self.headers).json()

    def pages(self, path: str, project: str | None = None):
        """Iterate a list endpoint in pages of 100 until a short page is returned.

        Args:
            path: Endpoint path without page or per_page parameters.
            project: Optional source project override.

        Yields:
            Items in API response order.

        Raises:
            RemoteError: Retrieval fails or a page is not a JSON list.
        """
        page = 1
        while True:
            separator = "&" if "?" in path else "?"
            items = self.get(f"{path}{separator}per_page=100&page={page}", project)
            if not isinstance(items, list):
                raise RemoteError("Expected a paginated GitLab list")
            yield from items
            if len(items) < 100:
                return
            page += 1

    def read_source(self, revision: str, path: str, repository: str | None = None) -> str:
        """Return UTF-8 source for path at revision, including source forks."""
        url = (
            f"{self.root(repository)}/repository/files/{quote(path, safe='')}/raw?"
            f"{urlencode({'ref': revision})}"
        )
        return self.client.get(url, self.headers).body.decode("utf-8")

    def tree(self, revision: str, project: str | None = None) -> list[TreeEntry]:
        """List all repository tree pages pinned to the source revision."""
        query = urlencode({"recursive": "true", "ref": revision})
        return [
            TreeEntry(item["path"], item["type"], item.get("id"))
            for item in self.pages(f"/repository/tree?{query}", project)
        ]

    def read_file(self, path: str, revision: str, project: str | None = None) -> str:
        """Decode raw test contents as UTF-8 with replacement for invalid bytes."""
        response = self.client.get(
            f"{self.root(project)}/repository/files/{quote(path, safe='')}/raw?{urlencode({'ref': revision})}",
            self.headers,
        )
        return response.body.decode("utf-8", errors="replace")

    def read_entry(self, entry: TreeEntry, revision: str, repository: str | None = None) -> str:
        """Read a listed source file at the selected commit."""
        if entry.type != "blob":
            raise RemoteError("GitLab tree entry is not a readable blob")
        return self.read_file(entry.path, revision, repository)

    def pull_request(self, number: int) -> tuple[dict, list[dict]]:
        """Read MR metadata and diffs, rejecting incomplete diff responses."""
        data = self.get(f"/merge_requests/{number}")
        changes = list(self.pages(f"/merge_requests/{number}/diffs"))
        if any(item.get("collapsed") or item.get("too_large") for item in changes):
            raise IncompleteResponseError("GitLab returned an incomplete merge request diff")
        return data, changes

    def fetch(self, number: int) -> PullRequest:
        """Return normalized metadata and changed files for one MR."""
        data, changes = self.pull_request(number)
        files = tuple(
            ChangedFile(
                path=item["new_path"],
                status=(
                    "removed" if item.get("deleted_file") else
                    "added" if item.get("new_file") else
                    "renamed" if item.get("renamed_file") else "modified"
                ),
                patch=item.get("diff"),
                previous_path=item["old_path"] if item.get("renamed_file") else None,
            )
            for item in changes
        )
        return PullRequest(
            number=number,
            title=data["title"],
            description=data.get("description") or "",
            url=data["web_url"],
            head_ref=data["sha"],
            base_ref=data["diff_refs"]["base_sha"],
            files=files,
            head_repository=str(data.get("source_project_id") or self.project),
        )
