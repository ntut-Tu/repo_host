package codebase

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const testSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type memorySource struct {
	entries []Entry
	files   map[string][]byte
	failure error
}

func (s *memorySource) Resolve(context.Context, string, string) (Revision, error) {
	return Revision{SHA: testSHA, Tree: testSHA}, s.failure
}
func (s *memorySource) Tree(context.Context, string, string, bool) ([]Entry, error) {
	return append([]Entry(nil), s.entries...), s.failure
}
func (s *memorySource) File(_ context.Context, _ string, name string, limit int) (Entry, []byte, error) {
	if s.failure != nil {
		return Entry{}, nil, s.failure
	}
	data, ok := s.files[name]
	if !ok {
		return Entry{}, nil, ErrNotFound
	}
	if len(data) > limit {
		return Entry{}, nil, ErrLimit
	}
	for _, entry := range s.entries {
		if entry.Path == name {
			return entry, data, nil
		}
	}
	return Entry{Path: name, OID: testSHA, Mode: "100644", Type: "blob"}, data, nil
}
func fixture(t *testing.T) (*Retriever, *memorySource) {
	t.Helper()
	source := &memorySource{entries: []Entry{{Path: "b", OID: testSHA, Mode: "100644", Type: "blob"}, {Path: "a", OID: testSHA, Mode: "100644", Type: "blob"}}, files: map[string][]byte{"a": []byte("one\ntwo\nthree"), "b": []byte("one\n"), "empty": {}, "binary": {0, 1}}}
	r, err := New(source, "owner/repo", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return r, source
}
func TestRevisionAndBoundTreePages(t *testing.T) {
	r, source := fixture(t)
	ctx := context.Background()
	revision, err := r.Resolve(ctx, "sha", testSHA)
	if err != nil || revision.Repository != "owner/repo" {
		t.Fatalf("%+v %v", revision, err)
	}
	if _, err = r.Resolve(ctx, "other", "x"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	first, err := r.Tree(ctx, testSHA, TreeQuery{Limit: 1})
	if err != nil || first.Complete || first.Entries[0].Path != "a" {
		t.Fatalf("%+v %v", first, err)
	}
	last, err := r.Tree(ctx, testSHA, TreeQuery{Limit: 1, Cursor: first.Next})
	if err != nil || !last.Complete || last.Entries[0].Path != "b" {
		t.Fatalf("%+v %v", last, err)
	}
	for _, q := range []TreeQuery{{Limit: 2, Cursor: first.Next}, {Limit: 1, Cursor: "bad"}, {Limit: -1}, {Path: "../x"}, {Limit: 1, Cursor: "eyJxdWVyeSI6IiIsIm9mZnNldCI6LTF9"}} {
		if _, err := r.Tree(ctx, testSHA, q); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v: %v", q, err)
		}
	}
	// A formerly valid offset must be rejected if a broken reader changes its snapshot.
	source.entries = nil
	if _, err := r.Tree(ctx, testSHA, TreeQuery{Limit: 1, Cursor: first.Next}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	source.failure = ErrStorage
	if _, err := r.Tree(ctx, testSHA, TreeQuery{}); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
}
func TestFileRangesAndRepresentations(t *testing.T) {
	r, source := fixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		selection  Selection
		want       string
		start, end int
		err        error
	}{
		{Selection{Path: "a"}, "one\ntwo\nthree", 1, 3, nil},
		{Selection{Path: "a", Start: 2, End: 2}, "two\n", 2, 2, nil},
		{Selection{Path: "a", Start: 3, End: 999}, "three", 3, 3, nil},
		{Selection{Path: "empty"}, "", 0, 0, nil},
		{Selection{Path: "a", Start: 4}, "", 0, 0, ErrInvalid},
		{Selection{Path: "a", Start: 3, End: 2}, "", 0, 0, ErrInvalid},
		{Selection{Path: "a", Start: -1}, "", 0, 0, ErrInvalid},
		{Selection{Path: "../a"}, "", 0, 0, ErrInvalid},
		{Selection{Path: "binary"}, "", 0, 0, ErrUnsupported},
		{Selection{Path: "missing"}, "", 0, 0, ErrNotFound},
	} {
		result, err := r.File(ctx, testSHA, tc.selection)
		if !errors.Is(err, tc.err) || (err == nil && (result.Content != tc.want || result.Start != tc.start || result.End != tc.end || result.Blob != testSHA)) {
			t.Fatalf("%+v: %+v %v", tc.selection, result, err)
		}
	}
	source.entries = append(source.entries, Entry{Path: "link", OID: testSHA, Mode: "120000", Type: "blob"})
	source.files["link"] = []byte("../../outside")
	source.files["lfs"] = []byte("version https://git-lfs.github.com/spec/v1\noid sha256:123\n")
	for name, kind := range map[string]string{"link": "symlink", "lfs": "lfs_pointer"} {
		result, err := r.File(ctx, testSHA, Selection{Path: name})
		if err != nil || result.Kind != kind {
			t.Fatalf("%+v %v", result, err)
		}
	}
	r.limits.FileBytes = 2
	if _, err := r.File(ctx, testSHA, Selection{Path: "a", Start: 1, End: 1}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
}
func TestBatchPartialErrorsAndBudget(t *testing.T) {
	r, _ := fixture(t)
	ctx := context.Background()
	r.limits.BatchBytes = 4
	result, err := r.Batch(ctx, testSHA, []Selection{{Path: "missing"}, {Path: "b"}, {Path: "a"}})
	if err != nil || result.Items[0].Error != "not_found" || result.Items[1].File.Content != "one\n" || result.Items[2].Error != "limit_exceeded" {
		t.Fatalf("%+v %v", result, err)
	}
	for _, items := range [][]Selection{nil, make([]Selection, 33)} {
		if _, err := r.Batch(ctx, testSHA, items); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := r.Batch(canceled, testSHA, []Selection{{Path: "a"}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestPathAndLimits(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../a", "/a", "a/../b", "a//b", "a/", "a\nx", "a\x00x", string([]byte{255})} {
		if ValidatePath(name, false) == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	if ValidatePath("a b/é.go", false) != nil {
		t.Fatal("valid literal path rejected")
	}
	if _, err := New(nil, "repo", Limits{FileBytes: -1}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	r, _ := fixture(t)
	if _, err := r.File(context.Background(), "HEAD", Selection{Path: "a"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	for _, err := range []error{ErrInvalid, ErrNotFound, ErrLimit, ErrUnsupported, ErrStorage, context.Canceled, context.DeadlineExceeded} {
		if ErrorCode(err) == "" {
			t.Fatal("empty error code")
		}
	}
}
func TestSearchLimitsAndFailures(t *testing.T) {
	r, source := fixture(t)
	ctx := context.Background()
	result, err := r.Search(ctx, SearchQuery{SHA: testSHA, Query: "one"})
	if err != nil || !result.Complete || len(result.Matches) != 2 || result.Matches[0].Line != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	result, err = r.Search(ctx, SearchQuery{SHA: testSHA, Query: "^one$", Regex: true, Limit: 1})
	if err != nil || result.Complete || result.Omitted["match_limit"] != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	for _, q := range []SearchQuery{{SHA: testSHA, Query: "[", Regex: true}, {SHA: testSHA}, {SHA: testSHA, Query: "x", Limit: -1}, {SHA: "HEAD", Query: "x"}} {
		if _, err := r.Search(ctx, q); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	source.entries = append(source.entries, Entry{Path: "binary", Type: "blob"}, Entry{Path: "dir", Type: "tree"}, Entry{Path: "link", Type: "blob", Mode: "120000"})
	result, err = r.Search(ctx, SearchQuery{SHA: testSHA, Query: "absent"})
	if err != nil || !result.Complete || result.Omitted["binary"] != 1 || result.Omitted["non_regular"] != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	r.limits.SearchFiles = 1
	result, _ = r.Search(ctx, SearchQuery{SHA: testSHA, Query: "one"})
	if result.Complete || result.Omitted["scan_budget"] == 0 {
		t.Fatal(result)
	}
	r.limits.SearchFiles = 100
	r.limits.FileBytes = 1
	result, _ = r.Search(ctx, SearchQuery{SHA: testSHA, Query: "one"})
	if result.Complete || result.Omitted["size_limit"] == 0 {
		t.Fatal(result)
	}
	r.limits.FileBytes = 1 << 20
	source.files["a"] = []byte(strings.Repeat("x", 5000))
	result, _ = r.Search(ctx, SearchQuery{SHA: testSHA, Query: "x"})
	if result.Complete || result.Omitted["long_line"] != 1 {
		t.Fatal(result)
	}
	source.failure = ErrStorage
	if _, err := r.Search(ctx, SearchQuery{SHA: testSHA, Query: "x"}); !errors.Is(err, ErrStorage) {
		t.Fatal(err)
	}
}

func (s *memorySource) Content(ctx context.Context, sha string, entry Entry, limit int) ([]byte, error) {
	_, data, err := s.File(ctx, sha, entry.Path, limit)
	return data, err
}
