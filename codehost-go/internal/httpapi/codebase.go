package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codebase"
)

type Source interface {
	Resolve(context.Context, string, string) (codebase.Revision, error)
	Tree(context.Context, string, codebase.TreeQuery) (codebase.TreeResult, error)
	File(context.Context, string, codebase.Selection) (codebase.FileResult, error)
	Batch(context.Context, string, []codebase.Selection) (codebase.BatchResult, error)
	Search(context.Context, codebase.SearchQuery) (codebase.SearchResult, error)
}

func registerSource(mux *http.ServeMux, resources Resources, source Source, token string) {
	mux.HandleFunc("POST /v1/revisions/resolve", withAuth(token, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.Kind == "pr" {
			number, err := strconv.Atoi(input.Value)
			if err != nil || number < 1 {
				failure(w, codebase.ErrInvalid)
				return
			}
			change, err := resources.PR(r.Context(), number)
			if err != nil {
				failure(w, err)
				return
			}
			input.Kind, input.Value = "sha", change.HeadSHA
		}
		result, err := source.Resolve(r.Context(), input.Kind, input.Value)
		resource(w, result, err)
	}))
	mux.HandleFunc("GET /v1/trees/{sha}", withAuth(token, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		limit, err := optionalInt(query.Get("limit"))
		if err != nil {
			failure(w, codebase.ErrInvalid)
			return
		}
		recursive := false
		if query.Has("recursive") {
			recursive, err = strconv.ParseBool(query.Get("recursive"))
			if err != nil {
				failure(w, codebase.ErrInvalid)
				return
			}
		}
		result, err := source.Tree(r.Context(), r.PathValue("sha"), codebase.TreeQuery{Path: query.Get("path"), Recursive: recursive, Limit: limit, Cursor: query.Get("cursor")})
		resource(w, result, err)
	}))
	mux.HandleFunc("GET /v1/files/{sha}", withAuth(token, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		start, err := optionalInt(query.Get("start"))
		if err != nil {
			failure(w, codebase.ErrInvalid)
			return
		}
		end, err := optionalInt(query.Get("end"))
		if err != nil {
			failure(w, codebase.ErrInvalid)
			return
		}
		result, err := source.File(r.Context(), r.PathValue("sha"), codebase.Selection{Path: query.Get("path"), Start: start, End: end})
		resource(w, result, err)
	}))
	mux.HandleFunc("POST /v1/files/batch", withAuth(token, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			SHA   string               `json:"sha"`
			Files []codebase.Selection `json:"files"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := source.Batch(r.Context(), input.SHA, input.Files)
		resource(w, result, err)
	}))
	mux.HandleFunc("POST /v1/search", withAuth(token, func(w http.ResponseWriter, r *http.Request) {
		var input codebase.SearchQuery
		if !decode(w, r, &input) {
			return
		}
		result, err := source.Search(r.Context(), input)
		resource(w, result, err)
	}))
}

func optionalInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			failure(w, codebase.ErrLimit)
		} else {
			failure(w, codebase.ErrInvalid)
		}
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		failure(w, codebase.ErrInvalid)
		return false
	}
	return true
}
