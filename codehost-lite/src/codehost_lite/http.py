"""Shared request handling; the caller owns the HTTP client and its lifetime."""

from collections.abc import Callable
from typing import Any, TypeVar
from urllib.parse import quote, urlsplit

import httpx

Resource = TypeVar("Resource")


class CodeHostError(RuntimeError):
    """Request or response failure with an optional upstream HTTP status code."""

    def __init__(self, message: str, status_code: int | None = None) -> None:
        super().__init__(message)
        self.status_code = status_code


class JsonApi:
    """Make one GET per read, with no cache, retries, or follow-up requests."""

    def __init__(self, client: httpx.Client, root: str, headers: dict[str, str]) -> None:
        url = urlsplit(root)
        if (
            url.scheme not in {"https", "http"}
            or not url.hostname
            or url.username is not None
            or url.password is not None
            or url.query
            or url.fragment
        ):
            raise ValueError(
                "API URL must be an HTTP(S) URL without credentials, query or fragment"
            )
        self._client = client
        self._root = root.rstrip("/")
        self._headers = headers

    def read(self, path: str, decode: Callable[[dict[str, Any]], Resource]) -> Resource:
        """Decode a resource; errors omit credentials, URLs and upstream response bodies."""
        try:
            response = self._client.get(
                self._root + path,
                headers=self._headers,
                follow_redirects=False,
            )
        except httpx.TimeoutException:
            raise CodeHostError("Code-host request timed out") from None
        except httpx.RequestError:
            raise CodeHostError("Could not connect to the code host") from None
        if not response.is_success:
            raise CodeHostError(
                f"Code-host request failed (HTTP {response.status_code})",
                response.status_code,
            )
        try:
            data = response.json()
            if not isinstance(data, dict):
                raise TypeError
            return decode(data)
        except (AttributeError, KeyError, TypeError, ValueError):
            raise CodeHostError("Code host returned an unexpected resource response") from None


def repository_path(repository: str, *, github: bool = False) -> str:
    """Encode owner/repo or a GitLab namespace/project path, including subgroups."""
    parts = repository.split("/")
    if (
        len(parts) < 2
        or (github and len(parts) != 2)
        or any(not part or part in {".", ".."} or part.strip() != part for part in parts)
    ):
        raise ValueError("Repository must be owner/repo (GitHub) or namespace/project (GitLab)")
    return quote(repository, safe="/" if github else "")


def resource_number(number: int) -> int:
    """Reject invalid PR/MR/issue numbers before sending a request."""
    if type(number) is not int or number <= 0:
        raise ValueError("Resource number must be a positive integer")
    return number


def commit_ref(ref: str) -> str:
    """Encode a SHA, branch or tag as one API path segment."""
    if not ref or ref in {".", ".."} or any(character.isspace() for character in ref):
        raise ValueError("Commit ref must be a non-empty SHA, branch or tag")
    return quote(ref, safe="")
