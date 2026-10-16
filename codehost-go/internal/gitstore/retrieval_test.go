package gitstore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/cache"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codebase"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codehost"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/httpapi"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

type repositoryFixture struct {
	store             *Store
	work, remote, sha string
}

func repository(t *testing.T) repositoryFixture {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	remote := filepath.Join(root, "remote.git")
	git(t, "init", "-q", work)
	git(t, "-C", work, "config", "user.name", "Test")
	git(t, "-C", work, "config", "user.email", "test@example.invalid")
	if err := os.MkdirAll(filepath.Join(work, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"src/main.go": "package example\n// first\n", "a b.txt": "hello\n", "binary": "\x00\xff", "empty": "", "-option": "literal", "lfs": "version https://git-lfs.github.com/spec/v1\noid sha256:abc\n"} {
		if err := os.WriteFile(filepath.Join(work, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../../outside", filepath.Join(work, "link")); err != nil {
		t.Fatal(err)
	}
	git(t, "-C", work, "add", ".")
	git(t, "-C", work, "commit", "-qm", "initial")
	sha := git(t, "-C", work, "rev-parse", "HEAD")
	git(t, "-C", work, "update-index", "--add", "--cacheinfo", "160000,"+sha+",module")
	git(t, "-C", work, "commit", "-qm", "add submodule entry")
	sha = git(t, "-C", work, "rev-parse", "HEAD")
	git(t, "-C", work, "tag", "-a", "v1", "-m", "release")
	git(t, "init", "-q", "--bare", remote)
	git(t, "-C", work, "push", "-q", remote, "HEAD:refs/heads/main", "HEAD:refs/pull/7/head", "refs/tags/v1")
	store, err := New(filepath.Join(root, "objects"), "https://example.invalid/owner/repo.git", "github", "")
	if err != nil {
		t.Fatal(err)
	}
	store.remote = remote
	return repositoryFixture{store, work, remote, sha}
}
func TestRevisionSnapshotsAndSharedStore(t *testing.T) {
	f := repository(t)
	ctx := context.Background()
	source, err := codebase.New(f.store, "owner/repo", codebase.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := source.Resolve(ctx, "branch", "main")
	if err != nil || revision.SHA != f.sha || revision.Tree == "" {
		t.Fatalf("%+v %v", revision, err)
	}
	tagged, err := source.Resolve(ctx, "tag", "v1")
	if err != nil || tagged.SHA != f.sha {
		t.Fatalf("%+v %v", tagged, err)
	}
	if err := os.WriteFile(filepath.Join(f.work, "src/main.go"), []byte("package changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, "-C", f.work, "commit", "-qam", "advance")
	git(t, "-C", f.work, "push", "-q", f.remote, "HEAD:refs/heads/main", "HEAD:refs/pull/7/head")
	advanced, err := source.Resolve(ctx, "branch", "main")
	if err != nil || advanced.SHA == f.sha {
		t.Fatalf("%+v %v", advanced, err)
	}
	// Disable the remote: both capabilities must now use the same retained objects.
	f.store.remote = filepath.Join(t.TempDir(), "unavailable.git")
	files, _ := cache.New(t.TempDir())
	hosting := codehost.New(snapshotReader{f.sha}, f.store, files, "github")
	if _, err := hosting.PR(ctx, 7); err != nil {
		t.Fatal(err)
	}
	original, err := source.File(ctx, f.sha, codebase.Selection{Path: "src/main.go"})
	if err != nil || !strings.Contains(original.Content, "first") {
		t.Fatalf("%+v %v", original, err)
	}
	updated, err := source.File(ctx, advanced.SHA, codebase.Selection{Path: "src/main.go"})
	if err != nil || updated.Content != "package changed\n" {
		t.Fatalf("%+v %v", updated, err)
	}
	tree, err := source.Tree(ctx, f.sha, codebase.TreeQuery{Recursive: true})
	if err != nil || !tree.Complete || len(tree.Entries) != 9 {
		t.Fatalf("%+v %v", tree, err)
	}
	for _, selection := range []codebase.Selection{{Path: "link"}, {Path: "a b.txt"}, {Path: "-option"}} {
		if _, err := source.File(ctx, f.sha, selection); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.File(ctx, f.sha, codebase.Selection{Path: "module"}); !errors.Is(err, codebase.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := source.File(ctx, f.sha, codebase.Selection{Path: "link/secret"}); !errors.Is(err, codebase.ErrNotFound) {
		t.Fatal(err)
	}
	result, err := source.Search(ctx, codebase.SearchQuery{SHA: f.sha, Path: "src", Query: "first"})
	if err != nil || len(result.Matches) != 1 || result.Matches[0].Line != 2 {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestMissingTreesAndBlobsAreRepaired(t *testing.T) {
	for _, kind := range []string{"tree", "blob"} {
		t.Run(kind, func(t *testing.T) {
			f := repository(t)
			ctx := context.Background()
			if _, err := f.store.Resolve(ctx, "sha", f.sha); err != nil {
				t.Fatal(err)
			}
			expression := f.sha + ":src"
			if kind == "blob" {
				expression += "/main.go"
			}
			oid := git(t, "-C", f.store.path, "rev-parse", expression)
			if err := os.Remove(filepath.Join(f.store.path, "objects", oid[:2], oid[2:])); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.inspect(ctx, oid); !errors.Is(err, codebase.ErrNotFound) {
				t.Fatal(err)
			}
			_, data, err := f.store.File(ctx, f.sha, "src/main.go", 1024)
			if err != nil || !strings.Contains(string(data), "first") {
				t.Fatalf("repair %s: %q %v", kind, data, err)
			}
		})
	}
}
func TestConcurrentColdReadsAndCancellation(t *testing.T) {
	f := repository(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := f.store.File(ctx, f.sha, "src/main.go", 1024); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := f.store.File(canceled, f.sha, "src/main.go", 1024); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() { finished <- f.store.lock(ctx, func() error { close(entered); <-release; return nil }) }()
	<-entered
	deadline, stop := context.WithCancel(ctx)
	stop()
	if err := f.store.Ensure(deadline, []string{f.sha}, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	// A broken repository must produce an operational error, not trigger fetch.
	if err := os.Remove(filepath.Join(f.store.path, "HEAD")); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Ensure(ctx, []string{f.sha}, ""); !errors.Is(err, codebase.ErrStorage) {
		t.Fatal(err)
	}
}

type snapshotReader struct{ sha string }

func (s snapshotReader) Snapshot(context.Context, int) (model.Snapshot, error) {
	return model.Snapshot{Change: model.Change{Number: 7, HeadSHA: s.sha, LinkedIssues: []int{}}}, nil
}
func (s snapshotReader) Issue(context.Context, int) (model.Issue, error) {
	return model.Issue{Number: 1, LinkedPRs: []int{7}}, nil
}
func (s snapshotReader) FetchRef(int) string { return "refs/pull/7/head" }

func TestHTTPRetrievalWithRealGit(t *testing.T) {
	f := repository(t)
	source, _ := codebase.New(f.store, "owner/repo", codebase.Limits{})
	files, _ := cache.New(t.TempDir())
	hosting := codehost.New(snapshotReader{f.sha}, f.store, files, "github")
	routes := httpapi.Handler(hosting, source, "test-token")
	cases := []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/v1/revisions/resolve", `{"kind":"pr","value":"7"}`, 200},
		{"GET", "/v1/trees/" + f.sha + "?recursive=true", "", 200},
		{"GET", "/v1/files/" + f.sha + "?path=src/main.go&start=2&end=2", "", 200},
		{"GET", "/v1/files/" + f.sha + "?path=missing", "", 404},
		{"GET", "/v1/files/" + f.sha + "?path=binary", "", 415},
		{"GET", "/v1/files/" + f.sha + "?path=../secret", "", 422},
		{"POST", "/v1/files/batch", `{"sha":"` + f.sha + `","files":[{"path":"empty"},{"path":"missing"}]}`, 200},
		{"POST", "/v1/search", `{"sha":"` + f.sha + `","query":"first","path":"src"}`, 200},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Authorization", "Bearer test-token")
			response := httptest.NewRecorder()
			routes.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			if tc.status == 200 {
				var result map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result["sha"] != f.sha || result["repository"] != "owner/repo" {
					t.Fatal(result)
				}
			}
		})
	}
}

func TestCorruptObjectIsNotAMiss(t *testing.T) {
	f := repository(t)
	ctx := context.Background()
	if err := f.store.Ensure(ctx, []string{f.sha}, ""); err != nil {
		t.Fatal(err)
	}
	blob := git(t, "-C", f.store.path, "rev-parse", f.sha+":src/main.go")
	object := filepath.Join(f.store.path, "objects", blob[:2], blob[2:])
	if err := os.Chmod(object, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(object, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.inspect(ctx, blob); !errors.Is(err, codebase.ErrStorage) {
		t.Fatalf("corruption became absence: %v", err)
	}
}
func TestGitBoundaries(t *testing.T) {
	f := repository(t)
	ctx := context.Background()
	if _, err := f.store.Resolve(ctx, "branch", "bad..ref"); !errors.Is(err, codebase.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := f.store.Resolve(ctx, "other", "main"); !errors.Is(err, codebase.ErrInvalid) {
		t.Fatal(err)
	}
	if err := f.store.Ensure(ctx, []string{"HEAD"}, ""); !errors.Is(err, codebase.ErrInvalid) {
		t.Fatal(err)
	}
	if err := f.store.Ensure(ctx, []string{f.sha}, "--bad"); !errors.Is(err, codebase.ErrInvalid) {
		t.Fatal(err)
	}
	if err := f.store.Ensure(ctx, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.command(ctx, "", 1, "--version"); !errors.Is(err, codebase.ErrLimit) {
		t.Fatal(err)
	}
	if _, err := f.store.Tree(ctx, f.sha, "src/main.go", false); !errors.Is(err, codebase.ErrInvalid) {
		t.Fatal(err)
	}
	if _, _, err := f.store.File(ctx, f.sha, "src/main.go", 1); !errors.Is(err, codebase.ErrLimit) {
		t.Fatal(err)
	}
	if _, _, err := f.store.File(ctx, f.sha, "", 100); !errors.Is(err, codebase.ErrInvalid) {
		t.Fatal(err)
	}
	first, err := New(t.TempDir(), "https://example.invalid/repo.git", "github", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(filepath.Dir(first.path), "https://example.invalid/repo.git", "github", "second")
	if err != nil || first.path == second.path {
		t.Fatal("credential scopes share storage")
	}
	for _, remote := range []string{"file:///tmp/repo", "https://token@example.invalid/repo", "https://example.invalid/repo?token=x"} {
		if _, err := New(t.TempDir(), remote, "github", ""); err == nil {
			t.Fatal("unsafe remote accepted")
		}
	}
}
