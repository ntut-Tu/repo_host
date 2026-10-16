"""Normalized pull request metadata and changed files."""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class ChangedFile:
    """One file changed by a PR/MR.

    Attributes:
        path: Repository-relative path on the new side of the diff.
        status: Change kind, such as added, modified, renamed, or removed.
        patch: Unified diff, or None when the host did not supply one.
        previous_path: Original path for a renamed file, otherwise None.
    """

    path: str
    status: str
    patch: str | None
    previous_path: str | None = None


@dataclass(frozen=True)
class PullRequest:
    """Normalized GitHub PR or GitLab merge request.

    Attributes:
        number: Repository-scoped PR number or GitLab MR IID.
        title: Change title.
        description: Change description; empty when absent.
        url: Web URL of the PR/MR.
        head_ref: Source commit SHA used for retrieving existing tests.
        base_ref: Base commit SHA reported by the host.
        files: Changed files supplied by the host.
        head_repository: Source repository name or project ID, including forks.
            None means use the configured repository.
    """

    number: int
    title: str
    description: str
    url: str
    head_ref: str
    base_ref: str
    files: tuple[ChangedFile, ...]
    head_repository: str | None = None


@dataclass(frozen=True)
class TreeEntry:
    """One source-revision tree entry with an optional provider blob ID."""

    path: str
    type: str
    blob_id: str | None = None
