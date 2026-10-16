"""Public provider-neutral code-host resource types."""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Literal


@dataclass(frozen=True)
class Commit:
    sha: str
    message: str
    committed_at: str | None = None


@dataclass(frozen=True)
class Change:
    number: int
    title: str
    description: str
    head_sha: str
    state: str
    linked_issues: tuple[int, ...] = ()


@dataclass(frozen=True)
class Issue:
    number: int
    title: str
    description: str
    linked_prs: tuple[int, ...] = ()


@dataclass(frozen=True)
class TimelineEvent:
    kind: Literal["commit", "comment", "merged"]
    occurred_at: str
    sha: str | None = None
    message: str | None = None
    author: str | None = None


@dataclass(frozen=True)
class PullRequestTimeline:
    change: Change
    events: tuple[TimelineEvent, ...]
    ordering_basis: str = "commit time and comment creation time; push time is unavailable"


def require_sha(sha: str) -> str:
    """Require an exact SHA-1 or SHA-256 object identifier."""
    if re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", sha) is None:
        raise ValueError("A full commit SHA is required")
    return sha
