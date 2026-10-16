package provider

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

const gitlabCommitFields = `nodes { sha fullTitle description committedDate } pageInfo { hasNextPage endCursor }`
const gitlabDiscussionFields = `nodes { notes(first:100) { nodes { body createdAt system author { username } } pageInfo { hasNextPage } } } pageInfo { hasNextPage endCursor }`

type GitLab struct {
	transport transport
	endpoint  string
	project   string
}

type glPage struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type glCommit struct {
	SHA           string  `json:"sha"`
	FullTitle     string  `json:"fullTitle"`
	Description   string  `json:"description"`
	CommittedDate *string `json:"committedDate"`
}

type glNote struct {
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"`
	System    bool   `json:"system"`
	Author    struct {
		Username string `json:"username"`
	} `json:"author"`
}

type glDiscussion struct {
	Notes struct {
		Nodes []glNote `json:"nodes"`
		Page  *glPage  `json:"pageInfo"`
	} `json:"notes"`
}

type glMR struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	HeadSHA     string  `json:"diffHeadSha"`
	State       string  `json:"state"`
	MergedAt    *string `json:"mergedAt"`
	Commits     struct {
		Nodes []glCommit `json:"nodes"`
		Page  *glPage    `json:"pageInfo"`
	} `json:"commits"`
	Discussions struct {
		Nodes []glDiscussion `json:"nodes"`
		Page  *glPage        `json:"pageInfo"`
	} `json:"discussions"`
}

func NewGitLab(repository, apiURL, token string) *GitLab {
	root := strings.TrimRight(apiURL, "/")
	endpoint := root + "/graphql"
	if strings.HasSuffix(root, "/v4") {
		endpoint = strings.TrimSuffix(root, "/v4") + "/graphql"
	}
	return &GitLab{newTransport(apiURL, token, "gitlab"), endpoint, repository}
}

func (g *GitLab) queryMR(ctx context.Context, number int, field, cursor string) (string, glMR, error) {
	commits, discussions := "", ""
	after, declaration := "", ""
	variables := map[string]any{"path": g.project, "iid": strconv.Itoa(number)}
	if cursor != "" {
		after, declaration = ",after:$after", ",$after:String!"
		variables["after"] = cursor
	}
	if field == "" || field == "commits" {
		commits = "commits(first:100" + after + "){ " + gitlabCommitFields + " }"
	}
	if field == "" || field == "discussions" {
		discussions = "discussions(first:100" + after + "){ " + gitlabDiscussionFields + " }"
	}
	query := "query($path:ID!,$iid:String!" + declaration + "){project(fullPath:$path){id mergeRequest(iid:$iid){"
	if field == "" {
		query += "title description diffHeadSha state mergedAt "
	}
	query += commits + " " + discussions + "}}}"
	var result struct {
		Project *struct {
			ID string `json:"id"`
			MR *glMR  `json:"mergeRequest"`
		} `json:"project"`
	}
	if err := g.transport.query(ctx, g.endpoint, query, variables, &result); err != nil {
		return "", glMR{}, err
	}
	if result.Project == nil || result.Project.MR == nil {
		return "", glMR{}, ErrUnavailable
	}
	return result.Project.ID, *result.Project.MR, nil
}

func (g *GitLab) commits(ctx context.Context, number int, first glMR) ([]glCommit, error) {
	if first.Commits.Nodes == nil || first.Commits.Page == nil {
		return nil, ErrIncomplete
	}
	all := append([]glCommit{}, first.Commits.Nodes...)
	page := *first.Commits.Page
	for count := 0; page.HasNextPage; count++ {
		if count > 1000 || page.EndCursor == "" {
			return nil, ErrIncomplete
		}
		_, next, err := g.queryMR(ctx, number, "commits", page.EndCursor)
		if err != nil {
			return nil, err
		}
		if next.Commits.Nodes == nil || next.Commits.Page == nil {
			return nil, ErrIncomplete
		}
		if next.Commits.Page.HasNextPage && next.Commits.Page.EndCursor == page.EndCursor {
			return nil, ErrIncomplete
		}
		all = append(all, next.Commits.Nodes...)
		page = *next.Commits.Page
	}
	return all, nil
}

func (g *GitLab) discussions(ctx context.Context, number int, first glMR) ([]glDiscussion, error) {
	if first.Discussions.Nodes == nil || first.Discussions.Page == nil {
		return nil, ErrIncomplete
	}
	all := append([]glDiscussion{}, first.Discussions.Nodes...)
	page := *first.Discussions.Page
	for count := 0; page.HasNextPage; count++ {
		if count > 1000 || page.EndCursor == "" {
			return nil, ErrIncomplete
		}
		_, next, err := g.queryMR(ctx, number, "discussions", page.EndCursor)
		if err != nil {
			return nil, err
		}
		if next.Discussions.Nodes == nil || next.Discussions.Page == nil {
			return nil, ErrIncomplete
		}
		if next.Discussions.Page.HasNextPage && next.Discussions.Page.EndCursor == page.EndCursor {
			return nil, ErrIncomplete
		}
		all = append(all, next.Discussions.Nodes...)
		page = *next.Discussions.Page
	}
	return all, nil
}

func localProjectID(global string) string {
	index := strings.LastIndex(global, "/")
	if index < 0 {
		return ""
	}
	return global[index+1:]
}

func validateLinks(links []map[string]any, projectID string) ([]int, error) {
	local := localProjectID(projectID)
	if local == "" {
		return nil, ErrIncomplete
	}
	numbers := make([]int, 0, len(links))
	for _, link := range links {
		id, okID := asInt(link["project_id"])
		iid, okIID := asInt(link["iid"])
		if !okID || !okIID || strconv.Itoa(id) != local {
			return nil, ErrIncomplete
		}
		numbers = append(numbers, iid)
	}
	return numbers, nil
}

func (g *GitLab) Snapshot(ctx context.Context, number int) (model.Snapshot, error) {
	projectID, mr, err := g.queryMR(ctx, number, "", "")
	if err != nil {
		return model.Snapshot{}, err
	}
	if mr.HeadSHA == "" {
		return model.Snapshot{}, ErrIncomplete
	}
	commits, err := g.commits(ctx, number, mr)
	if err != nil {
		return model.Snapshot{}, err
	}
	discussions, err := g.discussions(ctx, number, mr)
	if err != nil {
		return model.Snapshot{}, err
	}
	path := "/projects/" + escapeProject(g.project) + "/merge_requests/" + strconv.Itoa(number) + "/closes_issues"
	links, err := g.transport.pages(ctx, path)
	if err != nil {
		return model.Snapshot{}, err
	}
	issueNumbers, err := validateLinks(links, projectID)
	if err != nil {
		return model.Snapshot{}, err
	}
	return buildGitLab(number, mr, commits, discussions, links, issueNumbers)
}

func buildGitLab(number int, mr glMR, commits []glCommit, discussions []glDiscussion,
	links []map[string]any, issueNumbers []int) (model.Snapshot, error) {
	snapshot := model.Snapshot{Change: model.Change{Number: number, Title: mr.Title,
		Description: mr.Description, HeadSHA: mr.HeadSHA, State: mr.State, LinkedIssues: issueNumbers},
		Commits: []model.Commit{}, Events: []model.Event{}, Issues: []model.Issue{}, MergedAt: mr.MergedAt}
	for _, commit := range commits {
		if commit.SHA == "" {
			return model.Snapshot{}, ErrIncomplete
		}
		message := commit.FullTitle
		if commit.Description != "" {
			message += "\n\n" + commit.Description
		}
		snapshot.Commits = append(snapshot.Commits, model.Commit{SHA: commit.SHA,
			Message: message, CommittedAt: commit.CommittedDate})
		date := ""
		if commit.CommittedDate != nil {
			date = *commit.CommittedDate
		}
		snapshot.Events = append(snapshot.Events, model.Event{Kind: "commit", OccurredAt: date,
			SHA: &commit.SHA, Message: &message})
	}
	for _, discussion := range discussions {
		if discussion.Notes.Nodes == nil || discussion.Notes.Page == nil || discussion.Notes.Page.HasNextPage {
			return model.Snapshot{}, ErrIncomplete
		}
		for _, note := range discussion.Notes.Nodes {
			if note.System || note.CreatedAt == "" {
				continue
			}
			var author *string
			if note.Author.Username != "" {
				author = &note.Author.Username
			}
			body := note.Body
			snapshot.Events = append(snapshot.Events, model.Event{Kind: "comment", OccurredAt: note.CreatedAt,
				Message: &body, Author: author})
		}
	}
	for _, link := range links {
		iid, _ := asInt(link["iid"])
		title, _ := link["title"].(string)
		description, _ := link["description"].(string)
		if title != "" {
			snapshot.Issues = append(snapshot.Issues, model.Issue{Number: iid,
				Title: title, Description: description, LinkedPRs: []int{}})
		}
	}
	if mr.MergedAt != nil {
		snapshot.Events = append(snapshot.Events, model.Event{Kind: "merged", OccurredAt: *mr.MergedAt})
	}
	sort.SliceStable(snapshot.Events, func(i, j int) bool {
		left, right := snapshot.Events[i], snapshot.Events[j]
		if left.OccurredAt == right.OccurredAt {
			return left.Kind == "commit" && right.Kind != "commit"
		}
		return left.OccurredAt < right.OccurredAt
	})
	return snapshot, nil
}

func (g *GitLab) FetchRef(number int) string {
	return "refs/merge-requests/" + strconv.Itoa(number) + "/head"
}
