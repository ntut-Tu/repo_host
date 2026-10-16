package provider

import (
	"context"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

func (g *GitHub) Issue(ctx context.Context, number int) (model.Issue, error) {
	query := `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){issue(number:$number){number title body closedByPullRequestsReferences(first:100,includeClosedPrs:true){nodes{number repository{nameWithOwner}} pageInfo{hasNextPage}}}}}`
	var result struct {
		Repository struct {
			Issue *struct {
				Title      string       `json:"title"`
				Body       string       `json:"body"`
				References ghConnection `json:"closedByPullRequestsReferences"`
			} `json:"issue"`
		} `json:"repository"`
	}
	err := g.transport.query(ctx, g.endpoint, query, map[string]any{"owner": g.owner,
		"name": g.name, "number": number}, &result)
	if err != nil {
		return model.Issue{}, err
	}
	issue := result.Repository.Issue
	if issue == nil {
		return model.Issue{}, ErrUnavailable
	}
	if issue.References.Nodes == nil || issue.References.Page == nil || issue.References.Page.HasNextPage {
		return model.Issue{}, ErrIncomplete
	}
	output := model.Issue{Number: number, Title: issue.Title, Description: issue.Body, LinkedPRs: []int{}}
	for _, ref := range issue.References.Nodes {
		if ref.Repository.NameWithOwner != g.repo || ref.Number == 0 {
			return model.Issue{}, ErrIncomplete
		}
		output.LinkedPRs = append(output.LinkedPRs, ref.Number)
	}
	return output, nil
}
