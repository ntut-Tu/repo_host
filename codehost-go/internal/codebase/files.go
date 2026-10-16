package codebase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

type TreeQuery struct {
	Path      string
	Recursive bool
	Limit     int
	Cursor    string
}

type TreeResult struct {
	Repository string  `json:"repository"`
	SHA        string  `json:"sha"`
	Entries    []Entry `json:"entries"`
	Next       string  `json:"next,omitempty"`
	Complete   bool    `json:"complete"`
}

type cursor struct {
	Query  string `json:"query"`
	Offset int    `json:"offset"`
}

func (r *Retriever) Tree(ctx context.Context, sha string, q TreeQuery) (TreeResult, error) {
	result := TreeResult{Repository: r.repository, SHA: sha, Entries: []Entry{}}
	if err := validateSource(sha, q.Path, true); err != nil {
		return result, err
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 1000 {
		return result, ErrInvalid
	}
	identity, _ := json.Marshal([]any{r.repository, sha, q.Path, q.Recursive, q.Limit})
	digest := sha256.Sum256(identity)
	key := hex.EncodeToString(digest[:])
	position := cursor{Query: key}
	if q.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(raw, &position) != nil || position.Query != key || position.Offset < 0 {
			return result, ErrInvalid
		}
	}
	entries, err := r.reader.Tree(ctx, sha, q.Path, q.Recursive)
	if err != nil {
		return result, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	if position.Offset > len(entries) {
		return result, ErrInvalid
	}
	end := position.Offset + min(q.Limit, len(entries)-position.Offset)
	result.Entries = append(result.Entries, entries[position.Offset:end]...)
	result.Complete = end == len(entries)
	if !result.Complete {
		encoded, _ := json.Marshal(cursor{key, end})
		result.Next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return result, nil
}

type Selection struct {
	Path  string `json:"path"`
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
}

type FileResult struct {
	Repository string `json:"repository"`
	SHA        string `json:"sha"`
	Path       string `json:"path"`
	Blob       string `json:"blob"`
	Mode       string `json:"mode"`
	Kind       string `json:"kind"`
	Encoding   string `json:"encoding"`
	Content    string `json:"content"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	TotalLines int    `json:"total_lines"`
}

func (r *Retriever) File(ctx context.Context, sha string, selection Selection) (FileResult, error) {
	return r.file(ctx, sha, selection, r.limits.FileBytes)
}

func (r *Retriever) file(ctx context.Context, sha string, selection Selection, limit int) (FileResult, error) {
	result := FileResult{Repository: r.repository, SHA: sha, Path: selection.Path, Encoding: "utf-8", Kind: "file"}
	if err := validateSource(sha, selection.Path, false); err != nil {
		return result, err
	}
	if selection.Start < 0 || selection.End < 0 || (selection.End > 0 && selection.Start > selection.End) {
		return result, ErrInvalid
	}
	entry, data, err := r.reader.File(ctx, sha, selection.Path, limit)
	if err != nil {
		return result, err
	}
	if !text(data) {
		return result, ErrUnsupported
	}
	result.Blob, result.Mode = entry.OID, entry.Mode
	if entry.Mode == "120000" {
		result.Kind = "symlink"
	}
	if bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/v1\n")) {
		result.Kind = "lfs_pointer"
	}
	lines := sourceLines(string(data))
	result.TotalLines = len(lines)
	start, end := selection.Start, selection.End
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = len(lines)
	}
	if len(lines) == 0 && selection.Start == 0 && selection.End == 0 {
		return result, nil
	}
	if start > len(lines) {
		return result, ErrInvalid
	}
	end = min(end, len(lines))
	result.Start, result.End = start, end
	result.Content = strings.Join(lines[start-1:end], "")
	return result, nil
}

func text(data []byte) bool { return utf8.Valid(data) && !bytes.ContainsRune(data, 0) }
func sourceLines(data string) []string {
	if data == "" {
		return nil
	}
	lines := strings.SplitAfter(data, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type BatchItem struct {
	Selection Selection   `json:"selection"`
	File      *FileResult `json:"file,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type BatchResult struct {
	Repository string      `json:"repository"`
	SHA        string      `json:"sha"`
	Items      []BatchItem `json:"items"`
}

func (r *Retriever) Batch(ctx context.Context, sha string, selections []Selection) (BatchResult, error) {
	result := BatchResult{Repository: r.repository, SHA: sha, Items: []BatchItem{}}
	if validateSource(sha, "", true) != nil || len(selections) == 0 || len(selections) > r.limits.BatchFiles {
		return result, ErrInvalid
	}
	remaining := r.limits.BatchBytes
	for _, selection := range selections {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		item := BatchItem{Selection: selection}
		if remaining == 0 {
			item.Error = ErrorCode(ErrLimit)
		} else {
			file, err := r.file(ctx, sha, selection, min(r.limits.FileBytes, remaining))
			if err != nil {
				item.Error = ErrorCode(err)
			} else {
				remaining -= len(file.Content)
				item.File = &file
			}
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}
