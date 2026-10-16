package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

const githubCommitFields = `nodes { commit { oid message committedDate } }`
const githubIssueFields = `nodes { number title body repository { nameWithOwner } }`
const githubTimelineFields = `nodes { __typename
  ... on PullRequestCommit { commit { oid message committedDate } }
  ... on IssueComment { body createdAt author { login } }
  ... on PullRequestReview { body submittedAt author { login } }
  ... on PullRequestReviewThread { comments(first:100) { nodes { body createdAt author { login } } pageInfo { hasNextPage } } }
  ... on MergedEvent { createdAt } }`
const githubTimelineFilter = `itemTypes:[PULL_REQUEST_COMMIT,ISSUE_COMMENT,PULL_REQUEST_REVIEW,PULL_REQUEST_REVIEW_THREAD,MERGED_EVENT],`
const githubPageInfo = `pageInfo { hasNextPage endCursor }`

type GitHub struct {
	transport transport
	endpoint  string
	owner     string
	name      string
	repo      string
}

type ghPage struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type ghCommit struct {
	OID           string  `json:"oid"`
	Message       string  `json:"message"`
	CommittedDate *string `json:"committedDate"`
}

type ghNode struct {
	Type       string   `json:"__typename"`
	Commit     ghCommit `json:"commit"`
	Number     int      `json:"number"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	CreatedAt   string `json:"createdAt"`
	SubmittedAt string `json:"submittedAt"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
	Comments struct {
		Nodes []ghNode `json:"nodes"`
		Page  *ghPage  `json:"pageInfo"`
	} `json:"comments"`
}

type ghConnection struct {
	Nodes []ghNode `json:"nodes"`
	Page  *ghPage  `json:"pageInfo"`
}

type ghPR struct {
	Title    string       `json:"title"`
	Body     string       `json:"body"`
	HeadSHA  string       `json:"headRefOid"`
	State    string       `json:"state"`
	MergedAt *string      `json:"mergedAt"`
	Commits  ghConnection `json:"commits"`
	Timeline ghConnection `json:"timelineItems"`
	Issues   ghConnection `json:"closingIssuesReferences"`
}

func NewGitHub(repository, apiURL, token string) (*GitHub, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid GitHub repository")
	}
	endpoint := strings.TrimRight(apiURL, "/") + "/graphql"
	if strings.HasSuffix(strings.TrimRight(apiURL, "/"), "/api/v3") {
		endpoint = strings.TrimSuffix(strings.TrimRight(apiURL, "/"), "/api/v3") + "/api/graphql"
	}
	return &GitHub{newTransport(apiURL, token, "github"), endpoint, parts[0], parts[1], repository}, nil
}

func ghField(name, cursor string) string {
	fields := map[string]string{"commits": githubCommitFields, "timelineItems": githubTimelineFields,
		"closingIssuesReferences": githubIssueFields}
	filter := ""
	if name == "timelineItems" {
		filter = githubTimelineFilter
	}
	after := ""
	if cursor != "" {
		after = ",after:$after"
	}
	return name + "(" + filter + "first:100" + after + "){ " + fields[name] + " " + githubPageInfo + " }"
}

func (g *GitHub) queryPR(ctx context.Context, number int, field, cursor string) (ghPR, error) {
	selection := ghField("commits", "") + ghField("timelineItems", "") + ghField("closingIssuesReferences", "")
	variables := map[string]any{"owner": g.owner, "name": g.name, "number": number}
	varDecl := "$owner:String!,$name:String!,$number:Int!"
	if field != "" {
		selection = ghField(field, cursor)
		if cursor != "" {
			varDecl += ",$after:String!"
			variables["after"] = cursor
		}
	}
	query := "query(" + varDecl + "){repository(owner:$owner,name:$name){pullRequest(number:$number){"
	if field == "" {
		query += "number title body headRefOid state mergedAt "
	}
	query += selection + "}}}"
	var result struct {
		Repository struct {
			PR *ghPR `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := g.transport.query(ctx, g.endpoint, query, variables, &result); err != nil {
		return ghPR{}, err
	}
	if result.Repository.PR == nil {
		return ghPR{}, ErrUnavailable
	}
	return *result.Repository.PR, nil
}

func (g *GitHub) all(ctx context.Context, number int, field string, first ghConnection) ([]ghNode, error) {
	if first.Nodes == nil || first.Page == nil {
		return nil, ErrIncomplete
	}
	nodes := append([]ghNode{}, first.Nodes...)
	page := *first.Page
	for count := 0; page.HasNextPage; count++ {
		if count > 1000 || page.EndCursor == "" {
			return nil, ErrIncomplete
		}
		pr, err := g.queryPR(ctx, number, field, page.EndCursor)
		if err != nil {
			return nil, err
		}
		var next ghConnection
		switch field {
		case "commits":
			next = pr.Commits
		case "timelineItems":
			next = pr.Timeline
		default:
			next = pr.Issues
		}
		if next.Nodes == nil || next.Page == nil {
			return nil, ErrIncomplete
		}
		if next.Page.HasNextPage && next.Page.EndCursor == page.EndCursor {
			return nil, ErrIncomplete
		}
		nodes = append(nodes, next.Nodes...)
		page = *next.Page
	}
	return nodes, nil
}

func (g *GitHub) Snapshot(ctx context.Context, number int) (model.Snapshot, error) {
	pr, err := g.queryPR(ctx, number, "", "")
	if err != nil {
		return model.Snapshot{}, err
	}
	if pr.HeadSHA == "" {
		return model.Snapshot{}, ErrIncomplete
	}
	commits, err := g.all(ctx, number, "commits", pr.Commits)
	if err != nil {
		return model.Snapshot{}, err
	}
	events, err := g.all(ctx, number, "timelineItems", pr.Timeline)
	if err != nil {
		return model.Snapshot{}, err
	}
	issues, err := g.all(ctx, number, "closingIssuesReferences", pr.Issues)
	if err != nil {
		return model.Snapshot{}, err
	}
	return g.build(number, pr, commits, events, issues)
}

func (g *GitHub) build(number int, pr ghPR, commits, nodes, issues []ghNode) (model.Snapshot, error) {
	snapshot := model.Snapshot{Change: model.Change{Number: number, Title: pr.Title, Description: pr.Body,
		HeadSHA: pr.HeadSHA, State: pr.State, LinkedIssues: []int{}}, Commits: []model.Commit{},
		Events: []model.Event{}, Issues: []model.Issue{}, MergedAt: pr.MergedAt}
	for _, issue := range issues {
		if issue.Repository.NameWithOwner != g.repo || issue.Number == 0 {
			return model.Snapshot{}, ErrIncomplete
		}
		snapshot.Change.LinkedIssues = append(snapshot.Change.LinkedIssues, issue.Number)
		snapshot.Issues = append(snapshot.Issues, model.Issue{Number: issue.Number, Title: issue.Title,
			Description: issue.Body, LinkedPRs: []int{}})
	}
	for _, node := range commits {
		if node.Commit.OID == "" {
			return model.Snapshot{}, ErrIncomplete
		}
		snapshot.Commits = append(snapshot.Commits, model.Commit{SHA: node.Commit.OID,
			Message: node.Commit.Message, CommittedAt: node.Commit.CommittedDate})
	}
	for _, node := range nodes {
		newEvents, err := ghEvents(node)
		if err != nil {
			return model.Snapshot{}, err
		}
		snapshot.Events = append(snapshot.Events, newEvents...)
	}
	if pr.MergedAt != nil && !hasMerge(snapshot.Events) {
		snapshot.Events = append(snapshot.Events, model.Event{Kind: "merged", OccurredAt: *pr.MergedAt})
	}
	return snapshot, nil
}

func ghEvents(node ghNode) ([]model.Event, error) {
	switch node.Type {
	case "PullRequestCommit":
		if node.Commit.OID == "" || node.Commit.CommittedDate == nil {
			return nil, ErrIncomplete
		}
		return []model.Event{{Kind: "commit", OccurredAt: *node.Commit.CommittedDate,
			SHA: &node.Commit.OID, Message: &node.Commit.Message}}, nil
	case "MergedEvent":
		return []model.Event{{Kind: "merged", OccurredAt: node.CreatedAt}}, nil
	case "IssueComment", "PullRequestReview":
		if node.Body == "" {
			return nil, nil
		}
		date := node.CreatedAt
		if node.Type == "PullRequestReview" {
			date = node.SubmittedAt
		}
		if date == "" {
			return nil, nil
		}
		return []model.Event{commentEvent(node, date)}, nil
	case "PullRequestReviewThread":
		if node.Comments.Nodes == nil || node.Comments.Page == nil || node.Comments.Page.HasNextPage {
			return nil, ErrIncomplete
		}
		result := make([]model.Event, 0, len(node.Comments.Nodes))
		for _, item := range node.Comments.Nodes {
			result = append(result, commentEvent(item, item.CreatedAt))
		}
		return result, nil
	default:
		return nil, nil
	}
}

func commentEvent(node ghNode, date string) model.Event {
	var author *string
	if node.Author.Login != "" {
		author = &node.Author.Login
	}
	return model.Event{Kind: "comment", OccurredAt: date, Message: &node.Body, Author: author}
}

func hasMerge(events []model.Event) bool {
	for _, event := range events {
		if event.Kind == "merged" {
			return true
		}
	}
	return false
}

func (g *GitHub) FetchRef(number int) string { return fmt.Sprintf("refs/pull/%d/head", number) }
