package gitstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestLazyFetchAndRetainedSHA(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	remote := filepath.Join(root, "remote.git")
	git(t, "init", "-q", work)
	git(t, "-C", work, "config", "user.name", "Test")
	git(t, "-C", work, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(work, "file"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, "-C", work, "add", "file")
	git(t, "-C", work, "commit", "-qm", "initial")
	sha := git(t, "-C", work, "rev-parse", "HEAD")
	git(t, "init", "-q", "--bare", remote)
	git(t, "-C", work, "push", "-q", remote, "HEAD:refs/pull/3/head")
	git(t, "-C", work, "push", "-q", remote, "HEAD:refs/heads/main")
	store, err := New(filepath.Join(root, "objects"), "https://example.invalid/owner/repo.git", "github", "")
	if err != nil {
		t.Fatal(err)
	}
	store.remote = remote
	if has(t, store, sha) {
		t.Fatal("object fetched before request")
	}
	commit, err := store.Read(context.Background(), sha, "refs/pull/3/head")
	if err != nil {
		t.Fatal(err)
	}
	if commit.Message != "initial" || !has(t, store, sha) {
		t.Fatalf("missing fetched commit: %+v", commit)
	}
	store.remote = filepath.Join(root, "nonexistent.git")
	if _, err := store.Read(context.Background(), sha, "refs/pull/3/head"); err != nil {
		t.Fatalf("cached SHA should not fetch: %v", err)
	}
	if _, err := store.Read(context.Background(), strings.Repeat("b", 40), "refs/pull/3/head"); err == nil {
		t.Fatal("unavailable SHA should fail")
	}
	direct, err := New(filepath.Join(root, "direct"), "https://example.invalid/owner/repo.git", "github", "")
	if err != nil {
		t.Fatal(err)
	}
	direct.remote = remote
	if _, err := direct.Read(context.Background(), sha, ""); err != nil {
		t.Fatalf("exact-SHA fetch failed: %v", err)
	}
	git(t, "-C", remote, "update-ref", "-d", "refs/pull/3/head")
	fallback, err := New(filepath.Join(root, "fallback"), "https://example.invalid/owner/repo.git", "github", "")
	if err != nil {
		t.Fatal(err)
	}
	fallback.remote = remote
	if _, err := fallback.Read(context.Background(), sha, "refs/pull/3/head"); err != nil {
		t.Fatalf("single-SHA fallback failed: %v", err)
	}
}

func has(t *testing.T, s *Store, sha string) bool {
	t.Helper()
	_, err := s.inspect(context.Background(), sha)
	return err == nil
}
