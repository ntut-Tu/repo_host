package provider

import (
	"context"
	"strconv"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

func (g *GitLab) Issue(ctx context.Context, number int) (model.Issue, error) {
	query := `query($path:ID!,$iid:String!){project(fullPath:$path){id issue(iid:$iid){title description}}}`
	var result struct {
		Project *struct {
			ID    string `json:"id"`
			Issue *struct {
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"issue"`
		} `json:"project"`
	}
	err := g.transport.query(ctx, g.endpoint, query, map[string]any{"path": g.project,
		"iid": strconv.Itoa(number)}, &result)
	if err != nil {
		return model.Issue{}, err
	}
	if result.Project == nil || result.Project.Issue == nil {
		return model.Issue{}, ErrUnavailable
	}
	path := "/projects/" + escapeProject(g.project) + "/issues/" + strconv.Itoa(number) + "/closed_by"
	links, err := g.transport.pages(ctx, path)
	if err != nil {
		return model.Issue{}, err
	}
	linked, err := validateLinks(links, result.Project.ID)
	if err != nil {
		return model.Issue{}, err
	}
	return model.Issue{Number: number, Title: result.Project.Issue.Title,
		Description: result.Project.Issue.Description, LinkedPRs: linked}, nil
}
