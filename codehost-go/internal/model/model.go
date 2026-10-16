package model

import (
	"errors"
	"regexp"
)

type Commit struct {
	SHA         string  `json:"sha"`
	Message     string  `json:"message"`
	CommittedAt *string `json:"committed_at"`
}

type Change struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	HeadSHA      string `json:"head_sha"`
	State        string `json:"state"`
	LinkedIssues []int  `json:"linked_issues"`
}

type Issue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Description string `json:"description"`
	LinkedPRs   []int  `json:"linked_prs"`
}

type Event struct {
	Kind       string  `json:"kind"`
	OccurredAt string  `json:"occurred_at"`
	SHA        *string `json:"sha"`
	Message    *string `json:"message"`
	Author     *string `json:"author"`
}

type Timeline struct {
	Change        Change  `json:"change"`
	Events        []Event `json:"events"`
	OrderingBasis string  `json:"ordering_basis"`
}

type Snapshot struct {
	Change   Change   `json:"change"`
	Commits  []Commit `json:"commits"`
	Events   []Event  `json:"events"`
	Issues   []Issue  `json:"issues"`
	MergedAt *string  `json:"merged_at"`
}

var fullSHA = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

func ValidateSHA(sha string) error {
	if !fullSHA.MatchString(sha) {
		return errors.New("a full commit SHA is required")
	}
	return nil
}
