"""Boundary cases for code-host transport, immutable IDs, and expiry."""

import json
import unittest
from unittest.mock import patch
from urllib.error import HTTPError, URLError
from urllib.request import Request

from codehost.cache import ResourceCache
from codehost.errors import RemoteError
from codehost.http import HttpClient, Response, _CredentialSafeRedirect
from codehost.resources import require_sha


class Opened:
    def __init__(self, body):
        self.body = body
        self.headers = {"content-type": "application/json"}

    def __enter__(self):
        return self

    def __exit__(self, *args):
        return None

    def read(self):
        return self.body


class Opener:
    def __init__(self, outcome):
        self.outcome = outcome
        self.requests = []

    def open(self, request, timeout):
        self.requests.append((request, timeout))
        if isinstance(self.outcome, Exception):
            raise self.outcome
        return Opened(self.outcome)


class TransportCacheTests(unittest.TestCase):
    def test_json_post_and_get_use_bounded_transport(self):
        client = HttpClient(timeout=4)
        opener = Opener(b'{"ok":true}')
        client._opener = opener
        self.assertTrue(client.post_json("https://host/graphql", {"query": "{}"}).json()["ok"])
        self.assertEqual(json.loads(opener.requests[0][0].data), {"query": "{}"})
        self.assertEqual(opener.requests[0][1], 4)
        client.get("https://host/read")
        self.assertEqual(opener.requests[1][0].get_method(), "GET")

    def test_invalid_json_and_request_failures_are_explicit(self):
        with self.assertRaises(RemoteError):
            Response(b"invalid", {}).json()
        client = HttpClient()
        for failure in (URLError("offline"), HTTPError("https://host/read", 429, "limit", {}, None)):
            client._opener = Opener(failure)
            with self.assertRaises(RemoteError):
                client.get("https://host/read")

    def test_redirect_removes_credentials_on_origin_change(self):
        handler = _CredentialSafeRedirect()
        request = Request("https://host/read", headers={"Authorization": "secret", "PRIVATE-TOKEN": "secret"})
        redirected = handler.redirect_request(request, None, 302, "moved", {}, "https://other/read")
        self.assertNotIn("Authorization", redirected.headers)
        self.assertNotIn("Private-token", redirected.headers)
        same = handler.redirect_request(request, None, 302, "moved", {}, "https://host/next")
        self.assertIn("Authorization", same.headers)

    def test_cache_expiry_preserves_immutable_commits(self):
        with patch("codehost.cache.monotonic", side_effect=[1, 5]):
            cache = ResourceCache(ttl_seconds=2)
            cache.put("pr", "4", "old")
            cache.put("commit", "sha", "fixed", immutable=True)
            self.assertIsNone(cache.get("pr", "4"))
            self.assertEqual(cache.get("commit", "sha"), "fixed")
        self.assertIsNone(cache.get("absent", "1"))

    def test_only_full_commit_shas_are_accepted(self):
        self.assertEqual(require_sha("a" * 40), "a" * 40)
        with self.assertRaises(ValueError):
            require_sha("main")

    def test_cache_capacity_evicts_least_recent_entry(self):
        cache = ResourceCache(max_entries=2)
        cache.put("commit", "a", "a", immutable=True)
        cache.put("commit", "b", "b", immutable=True)
        self.assertEqual(cache.get("commit", "a"), "a")
        cache.put("commit", "c", "c", immutable=True)
        self.assertIsNone(cache.get("commit", "b"))
        with self.assertRaises(ValueError):
            ResourceCache(max_entries=0)
