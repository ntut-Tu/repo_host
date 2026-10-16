package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

type fakeResources struct{}

func (fakeResources) Commit(_ context.Context, sha string) (model.Commit, error) {
	return model.Commit{SHA: sha}, nil
}
func (fakeResources) PR(_ context.Context, n int) (model.Change, error) {
	return model.Change{Number: n}, nil
}
func (fakeResources) Issue(_ context.Context, n int) (model.Issue, error) {
	return model.Issue{Number: n}, nil
}
func (fakeResources) Timeline(_ context.Context, n int) (model.Timeline, error) {
	return model.Timeline{}, nil
}

func TestRoutesAndAuth(t *testing.T) {
	server := httptest.NewServer(Handler(fakeResources{}, nil, "12345678901234567890123456789012"))
	defer server.Close()
	response, err := http.Get(server.URL + "/health/live")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("health: %d", response.StatusCode)
	}
	response, err = http.Get(server.URL + "/v1/prs/3")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatalf("auth: %d", response.StatusCode)
	}
	for _, path := range []string{"/v1/prs/3", "/v1/issues/7", "/v1/prs/3/timeline",
		"/v1/commits/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		request, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		request.Header.Set("Authorization", "Bearer 12345678901234567890123456789012")
		response, err = http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatalf("%s: %d", path, response.StatusCode)
		}
	}
}
