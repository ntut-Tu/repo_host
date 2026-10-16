class RemoteError(RuntimeError):
    """A remote service could not provide required data."""


class IncompleteResponseError(RemoteError):
    """Provider response omits data required for a complete analysis."""
