"""Provider-neutral source-control access required by analysis callers."""

from __future__ import annotations

from typing import Protocol

from codehost.models import PullRequest, TreeEntry
from codehost.resources import Change, Commit, Issue, PullRequestTimeline


class CodeHost(Protocol):
    """Read one repository's changes and files at exact source revisions."""

    repository: str

    def fetch(self, number: int) -> PullRequest: ...

    def tree(self, revision: str, repository: str | None = None) -> list[TreeEntry]: ...

    def read_source(self, revision: str, path: str, repository: str | None = None) -> str: ...

    def read_entry(self, entry: TreeEntry, revision: str, repository: str | None = None) -> str: ...

    def get_commit(self, sha: str) -> Commit: ...

    def get_pr(self, number: int) -> Change: ...

    def get_issue(self, number: int) -> Issue: ...

    def get_pr_timeline(self, number: int) -> PullRequestTimeline: ...
