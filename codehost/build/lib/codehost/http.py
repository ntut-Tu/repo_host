"""HTTP transport with bounded request timeout and credential-aware redirects."""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Mapping
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

from codehost.errors import RemoteError


@dataclass(frozen=True)
class Response:
    """HTTP response data returned by the transport.

    Attributes:
        body: Raw response bytes.
        headers: Response headers as supplied by urllib.
    """

    body: bytes
    headers: Mapping[str, str]

    def json(self) -> Any:
        """Decode the body as JSON.

        Returns:
            The decoded JSON value.

        Raises:
            RemoteError: The response is not valid JSON or cannot be decoded.
        """
        try:
            return json.loads(self.body)
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise RemoteError("Remote response is not valid JSON") from exc


class _CredentialSafeRedirect(HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, new_url):
        redirected = super().redirect_request(request, fp, code, msg, headers, new_url)
        if redirected and urlsplit(request.full_url)[:2] != urlsplit(new_url)[:2]:
            private = {"authorization", "private-token"}
            redirected.headers = {
                key: value for key, value in redirected.headers.items()
                if key.lower() not in private
            }
            redirected.unredirected_hdrs = {
                key: value for key, value in redirected.unredirected_hdrs.items()
                if key.lower() not in private
            }
        return redirected


class HttpClient:
    """Perform synchronous GET requests for remote data adapters."""

    def __init__(self, timeout: float = 20.0) -> None:
        """Configure the underlying urllib opener.

        Args:
            timeout: Timeout in seconds for blocking network operations.
        """
        self.timeout = timeout
        self._opener = build_opener(_CredentialSafeRedirect())

    def get(self, url: str, headers: Mapping[str, str] | None = None) -> Response:
        """Read a remote resource into memory.

        Authorization and PRIVATE-TOKEN headers are removed on redirects to
        a different scheme or authority.

        Args:
            url: Resource URL.
            headers: Optional request headers, including authentication.

        Returns:
            Response bytes and headers.

        Raises:
            RemoteError: urllib reports an HTTPError or URLError.
        """
        return self._send(Request(url, headers=dict(headers or {})))

    def post_json(self, url: str, payload: Mapping[str, Any],
                  headers: Mapping[str, str] | None = None) -> Response:
        """Send a JSON request without exposing credentials on redirects."""
        request_headers = {**dict(headers or {}), "Content-Type": "application/json"}
        body = json.dumps(payload).encode("utf-8")
        return self._send(Request(url, data=body, headers=request_headers, method="POST"))

    def _send(self, request: Request) -> Response:
        try:
            with self._opener.open(request, timeout=self.timeout) as response:
                return Response(response.read(), dict(response.headers.items()))
        except HTTPError as exc:
            raise RemoteError(f"Remote request failed with HTTP {exc.code}: {request.full_url}") from exc
        except URLError as exc:
            raise RemoteError(f"Remote request failed: {request.full_url}: {exc.reason}") from exc
