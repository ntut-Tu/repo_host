package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/api"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codebase"
	"net/http"
	"strconv"
	"time"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codehost"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

type Resources interface {
	Commit(context.Context, string) (model.Commit, error)
	PR(context.Context, int) (model.Change, error)
	Issue(context.Context, int) (model.Issue, error)
	Timeline(context.Context, int) (model.Timeline, error)
}

func Handler(resources Resources, source Source, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /openapi.json", api.Spec)
	mux.HandleFunc("GET /docs", api.Docs)
	if source != nil {
		registerSource(mux, resources, source, token)
	}
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/commits/{sha}", withAuth(token, func(w http.ResponseWriter, r *http.Request) {
		sha := r.PathValue("sha")
		if err := model.ValidateSHA(sha); err != nil {
			fail(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		result, err := resources.Commit(r.Context(), sha)
		resource(w, result, err)
	}))
	mux.HandleFunc("GET /v1/prs/{number}", withAuth(token, numberRoute(func(ctx context.Context, n int) (any, error) {
		return resources.PR(ctx, n)
	})))
	mux.HandleFunc("GET /v1/issues/{number}", withAuth(token, numberRoute(func(ctx context.Context, n int) (any, error) {
		return resources.Issue(ctx, n)
	})))
	mux.HandleFunc("GET /v1/prs/{number}/timeline", withAuth(token, numberRoute(func(ctx context.Context, n int) (any, error) {
		return resources.Timeline(ctx, n)
	})))
	return mux
}

func withAuth(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actual := []byte(r.Header.Get("Authorization"))
		expected := []byte("Bearer " + token)
		if subtle.ConstantTimeCompare(actual, expected) != 1 {
			fail(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}

func numberRoute(read func(context.Context, int) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		number, err := strconv.Atoi(r.PathValue("number"))
		if err != nil || codehost.ValidateNumber(number) != nil {
			fail(w, http.StatusUnprocessableEntity, "number must be positive")
			return
		}
		result, err := read(r.Context(), number)
		resource(w, result, err)
	}
}

func resource(w http.ResponseWriter, value any, err error) {
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, http.StatusOK, value)
}

func fail(w http.ResponseWriter, status int, detail string) {
	respond(w, status, map[string]string{"detail": detail})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func failure(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, codebase.ErrInvalid):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, codebase.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, codebase.ErrLimit):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, codebase.ErrUnsupported):
		status = http.StatusUnsupportedMediaType
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	case errors.Is(err, context.Canceled):
		status = http.StatusRequestTimeout
	}
	respond(w, status, map[string]string{"code": codebase.ErrorCode(err), "detail": http.StatusText(status)})
}
