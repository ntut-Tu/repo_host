"""Per-client cache for immutable commits and briefly cached mutable resources."""

from __future__ import annotations

from collections import OrderedDict
from threading import RLock
from time import monotonic
from typing import Any


class ResourceCache:
    def __init__(self, ttl_seconds: float = 30.0, max_entries: int = 4096) -> None:
        if max_entries < 1:
            raise ValueError("Cache capacity must be positive")
        self.ttl_seconds = ttl_seconds
        self.max_entries = max_entries
        self._lock = RLock()
        self._values: OrderedDict[tuple[str, str], tuple[float | None, Any]] = OrderedDict()

    def get(self, kind: str, key: str) -> Any | None:
        with self._lock:
            item = self._values.get((kind, key))
            if item is None:
                return None
            expires, value = item
            if expires is not None and monotonic() >= expires:
                del self._values[(kind, key)]
                return None
            self._values.move_to_end((kind, key))
            return value

    def put(self, kind: str, key: str, value: Any, *, immutable: bool = False) -> None:
        expires = None if immutable else monotonic() + self.ttl_seconds
        with self._lock:
            self._values[(kind, key)] = (expires, value)
            self._values.move_to_end((kind, key))
            if len(self._values) > self.max_entries:
                self._values.popitem(last=False)
