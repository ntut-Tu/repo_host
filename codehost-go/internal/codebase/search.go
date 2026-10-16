package codebase

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
)

type SearchQuery struct {
	SHA   string `json:"sha"`
	Path  string `json:"path,omitempty"`
	Query string `json:"query"`
	Regex bool   `json:"regex,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type Match struct {
	Path string `json:"path"`
	Blob string `json:"blob"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type SearchResult struct {
	Repository string         `json:"repository"`
	SHA        string         `json:"sha"`
	Matches    []Match        `json:"matches"`
	Complete   bool           `json:"complete"`
	Omitted    map[string]int `json:"omitted"`
}

func (r *Retriever) Search(ctx context.Context, q SearchQuery) (SearchResult, error) {
	result := SearchResult{Repository: r.repository, SHA: q.SHA, Matches: []Match{}, Complete: true, Omitted: map[string]int{}}
	if validateSource(q.SHA, q.Path, true) != nil || q.Query == "" || len(q.Query) > 4096 {
		return result, ErrInvalid
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 1000 {
		return result, ErrInvalid
	}
	pattern := q.Query
	if !q.Regex {
		pattern = regexp.QuoteMeta(pattern)
	}
	matcher, err := regexp.Compile(pattern)
	if err != nil {
		return result, ErrInvalid
	}
	entries, err := r.reader.Tree(ctx, q.SHA, q.Path, true)
	if err != nil {
		return result, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	scanned, remaining := 0, r.limits.SearchBytes
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if entry.Type == "tree" {
			continue
		}
		if entry.Type != "blob" || entry.Mode == "120000" {
			result.Omitted["non_regular"]++
			continue
		}
		if scanned >= r.limits.SearchFiles || remaining <= 0 {
			result.Complete = false
			result.Omitted["scan_budget"]++
			continue
		}
		scanned++
		data, err := r.reader.Content(ctx, q.SHA, entry, min(r.limits.FileBytes, remaining))
		if errors.Is(err, ErrLimit) {
			result.Complete = false
			result.Omitted["size_limit"]++
			continue
		}
		if err != nil {
			return result, err
		}
		remaining -= len(data)
		if !text(data) {
			result.Omitted["binary"]++
			continue
		}
		for i, line := range sourceLines(string(data)) {
			if !matcher.MatchString(strings.TrimSuffix(line, "\n")) {
				continue
			}
			if len(result.Matches) == q.Limit {
				result.Complete = false
				result.Omitted["match_limit"]++
				return result, nil
			}
			if len(line) > 4096 {
				result.Complete = false
				result.Omitted["long_line"]++
				continue
			}
			result.Matches = append(result.Matches, Match{entry.Path, entry.OID, i + 1, strings.TrimSuffix(line, "\n")})
		}
	}
	return result, nil
}
