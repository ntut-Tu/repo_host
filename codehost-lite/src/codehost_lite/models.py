"""Immutable resource metadata shared by both providers."""

from dataclasses import dataclass


@dataclass(frozen=True)
class PullRequest:
    """A GitHub PR or GitLab MR; number is the repository-local number/IID.

    State is open, closed, merged, or the provider's other state (such as locked).
    A newly created GitLab MR can have no head SHA yet.
    """

    number: int
    title: str
    description: str
    state: str
    url: str
    head_sha: str | None
    source_branch: str
    target_branch: str


@dataclass(frozen=True)
class Issue:
    """One issue's metadata, excluding comments and linked resources."""

    number: int
    title: str
    description: str
    state: str
    url: str


@dataclass(frozen=True)
class Commit:
    """One resolved commit; SHA identifies the result even when requested by branch."""

    sha: str
    message: str
    committed_at: str | None
    author_name: str | None
    url: str
