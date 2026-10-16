"""Order commit and human feedback snapshots without inferring causality."""

from codehost.resources import Change, Commit, PullRequestTimeline, TimelineEvent


def make_timeline(change: Change, commits: list[Commit],
                  comments: list[TimelineEvent], merged_at: str | None) -> PullRequestTimeline:
    events = [TimelineEvent("commit", item.committed_at or "", item.sha, item.message)
              for item in commits]
    events.extend(comments)
    if merged_at:
        events.append(TimelineEvent("merged", merged_at))
    events.sort(key=lambda item: (item.occurred_at, item.kind != "commit"))
    return PullRequestTimeline(change, tuple(events))
