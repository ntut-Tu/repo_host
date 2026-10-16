package codehost

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/cache"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeReader struct {
	snapshots int
	issues    int
	err       error
	empty     bool
}

func (f *fakeReader) Snapshot(_ context.Context, number int) (model.Snapshot, error) {
	f.snapshots++
	if f.err != nil {
		return model.Snapshot{}, f.err
	}
	if f.empty {
		return model.Snapshot{}, nil
	}
	message, date := "provider message", "2026-01-01T00:00:00Z"
	return model.Snapshot{Change: model.Change{Number: number, HeadSHA: sha, LinkedIssues: []int{}},
		Events: []model.Event{{Kind: "commit", SHA: stringPointer(sha), Message: &message, OccurredAt: date}}}, nil
}
func (f *fakeReader) Issue(_ context.Context, number int) (model.Issue, error) {
	f.issues++
	if f.err != nil {
		return model.Issue{}, f.err
	}
	return model.Issue{Number: number, LinkedPRs: []int{}}, nil
}
func stringPointer(value string) *string { return &value }

type fakeGit struct {
	ensures   int
	reads     int
	ensureErr error
	readErr   error
}

func (f *fakeGit) Ensure(_ context.Context, shas []string, hint string) error {
	f.ensures++
	return f.ensureErr
}
func (f *fakeGit) Read(_ context.Context, sha string, hint string) (model.Commit, error) {
	f.reads++
	if f.readErr != nil {
		return model.Commit{}, f.readErr
	}
	return model.Commit{SHA: sha, Message: "local git message"}, nil
}

func TestSnapshotsPersistAcrossServiceInstances(t *testing.T) {
	files, err := cache.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reader, git := &fakeReader{}, &fakeGit{}
	first := New(reader, git, files, "github")
	if _, err := first.PR(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	second := New(reader, git, files, "github")
	timeline, err := second.Timeline(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if reader.snapshots != 1 || git.ensures != 2 || git.reads != 1 || *timeline.Events[0].Message != "local git message" {
		t.Fatalf("cache or Git mismatch: snapshot calls=%d timeline=%+v", reader.snapshots, timeline)
	}
	if _, err := second.Issue(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Issue(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if reader.issues != 1 {
		t.Fatalf("issue cache missed: %d", reader.issues)
	}
}

func (f *fakeReader) FetchRef(number int) string { return "refs/pull/2/head" }

type failedCache struct{ err error }

func (f failedCache) Get(string, string, any) bool                 { return false }
func (f failedCache) Put(string, string, any, time.Duration) error { return f.err }

func TestHostingFailureBoundaries(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("dependency failed")
	for _, operation := range []string{"pr", "issue", "timeline"} {
		for _, dependency := range []string{"provider", "cache", "git"} {
			if operation == "issue" && dependency == "git" {
				continue
			}
			t.Run(operation+"/"+dependency, func(t *testing.T) {
				reader, git := &fakeReader{}, &fakeGit{}
				files, _ := cache.New(t.TempDir())
				var storage Cache = files
				switch dependency {
				case "provider":
					reader.err = failure
				case "cache":
					storage = failedCache{failure}
				case "git":
					git.ensureErr = failure
				}
				service := New(reader, git, storage, "github")
				var err error
				switch operation {
				case "pr":
					_, err = service.PR(ctx, 1)
				case "issue":
					_, err = service.Issue(ctx, 1)
				case "timeline":
					_, err = service.Timeline(ctx, 1)
				}
				if !errors.Is(err, failure) {
					t.Fatal(err)
				}
			})
		}
	}
	files, _ := cache.New(t.TempDir())
	git := &fakeGit{readErr: failure}
	service := New(&fakeReader{}, git, files, "gitlab")
	if _, err := service.Timeline(ctx, 1); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := service.Commit(ctx, sha); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	git.readErr = nil
	if _, err := service.Commit(ctx, sha); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Commit(ctx, "HEAD"); err == nil {
		t.Fatal("invalid SHA accepted")
	}
	timeline, err := service.Timeline(ctx, 1)
	if err != nil || timeline.OrderingBasis != "commit time and comment creation time; push time is unavailable" {
		t.Fatalf("%+v %v", timeline, err)
	}
	files, _ = cache.New(t.TempDir())
	service = New(&fakeReader{empty: true}, git, files, "github")
	timeline, err = service.Timeline(ctx, 2)
	if err != nil || timeline.Events == nil {
		t.Fatalf("%+v %v", timeline, err)
	}
	if ValidateNumber(0) == nil || ValidateNumber(1) != nil {
		t.Fatal("number validation")
	}
}
