package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codebase"
)

type failingReader struct{ err error }

func (f failingReader) Resolve(context.Context, string, string) (codebase.Revision, error) {
	return codebase.Revision{}, f.err
}
func (f failingReader) Tree(context.Context, string, string, bool) ([]codebase.Entry, error) {
	return nil, f.err
}
func (f failingReader) File(context.Context, string, string, int) (codebase.Entry, []byte, error) {
	return codebase.Entry{}, nil, f.err
}

const fullSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestRetrievalValidationAndErrors(t *testing.T) {
	source, _ := codebase.New(failingReader{codebase.ErrStorage}, "repo", codebase.Limits{})
	handler := Handler(fakeResources{}, source, "secret")
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/v1/revisions/resolve", `{"kind":"pr","value":"0"}`, 422},
		{"POST", "/v1/revisions/resolve", `{"kind":"branch","value":"main","unexpected":1}`, 422},
		{"POST", "/v1/revisions/resolve", `{} {}`, 422},
		{"POST", "/v1/revisions/resolve", `{`, 422},
		{"POST", "/v1/revisions/resolve", `{"kind":"branch","value":"main"}`, 502},
		{"GET", "/v1/trees/" + fullSHA + "?limit=x&recursive=true", "", 422},
		{"GET", "/v1/trees/" + fullSHA + "?recursive=x", "", 422},
		{"GET", "/v1/files/" + fullSHA + "?path=a&start=x", "", 422},
		{"GET", "/v1/files/" + fullSHA + "?path=a&end=x", "", 422},
		{"POST", "/v1/files/batch", `{"sha":"` + fullSHA + `","files":[]}`, 422},
		{"POST", "/v1/search", `{"sha":"` + fullSHA + `","query":"[","regex":true}`, 422},
		{"POST", "/v1/search", `{"query":"` + strings.Repeat("x", 1<<20) + `"}`, 413},
	} {
		request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		request.Header.Set("Authorization", "Bearer secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, response.Code, response.Body.String())
		}
	}
	for _, tc := range []struct {
		err    error
		status int
	}{{codebase.ErrInvalid, 422}, {codebase.ErrNotFound, 404}, {codebase.ErrLimit, 413}, {codebase.ErrUnsupported, 415}, {context.Canceled, 408}, {context.DeadlineExceeded, 504}, {errors.New("sensitive detail"), 502}} {
		source, _ := codebase.New(failingReader{tc.err}, "repo", codebase.Limits{})
		handler := Handler(fakeResources{}, source, "secret")
		request := httptest.NewRequest("GET", "/v1/files/"+fullSHA+"?path=a", nil)
		request.Header.Set("Authorization", "Bearer secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status || strings.Contains(response.Body.String(), "sensitive") {
			t.Fatalf("%d %s", response.Code, response.Body.String())
		}
	}
}
func TestContractAndRouteAuthentication(t *testing.T) {
	source, _ := codebase.New(failingReader{codebase.ErrNotFound}, "repo", codebase.Limits{})
	handler := Handler(fakeResources{}, source, "secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/openapi.json", nil))
	var contract struct {
		Version    string                                `json:"openapi"`
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &contract); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || contract.Version != "3.1.0" || len(contract.Paths) != 12 {
		t.Fatalf("invalid contract: %d paths=%d", response.Code, len(contract.Paths))
	}
	for path, methods := range contract.Paths {
		for method, raw := range methods {
			var operation struct {
				Responses map[string]struct {
					Content map[string]struct{ Schema map[string]any }
				}
			}
			if err := json.Unmarshal(raw, &operation); err != nil {
				t.Fatal(err)
			}
			if _, ok := operation.Responses["200"]; !ok {
				t.Fatal("missing successful response", path)
			}
			target := strings.ReplaceAll(strings.ReplaceAll(path, "{sha}", fullSHA), "{number}", "1")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(strings.ToUpper(method), target, nil))
			expected := 401
			if path == "/health/live" || path == "/docs" || path == "/openapi.json" {
				expected = 200
			}
			if response.Code != expected {
				t.Fatalf("documented route %s %s: %d", method, path, response.Code)
			}
			if expected == 401 {
				for _, media := range operation.Responses["200"].Content {
					reference, ok := media.Schema["$ref"].(string)
					if !ok || contract.Components.Schemas[strings.TrimPrefix(reference, "#/components/schemas/")] == nil {
						t.Fatal("missing response schema", path)
					}
				}
			}
		}
	}
}

func (f failingReader) Content(context.Context, string, codebase.Entry, int) ([]byte, error) {
	return nil, f.err
}
