package codebase

import (
	"context"
	"errors"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

var (
	ErrInvalid     = errors.New("invalid retrieval request")
	ErrNotFound    = errors.New("source object or path not found")
	ErrLimit       = errors.New("retrieval limit exceeded")
	ErrUnsupported = errors.New("unsupported source representation")
	ErrStorage     = errors.New("Git storage operation failed")
)

type Revision struct {
	Repository string `json:"repository"`
	SHA        string `json:"sha"`
	Tree       string `json:"tree"`
}

type Entry struct {
	Path string `json:"path"`
	OID  string `json:"oid"`
	Mode string `json:"mode"`
	Type string `json:"type"`
}

type Reader interface {
	Resolve(context.Context, string, string) (Revision, error)
	Tree(context.Context, string, string, bool) ([]Entry, error)
	File(context.Context, string, string, int) (Entry, []byte, error)
	Content(context.Context, string, Entry, int) ([]byte, error)
}

// Limits bound work as well as response size. Zero fields use defaults.
type Limits struct {
	FileBytes   int `yaml:"file_bytes"`
	BatchFiles  int `yaml:"batch_files"`
	BatchBytes  int `yaml:"batch_bytes"`
	SearchFiles int `yaml:"search_files"`
	SearchBytes int `yaml:"search_bytes"`
}

func (l Limits) Defaults() Limits {
	if l.FileBytes == 0 {
		l.FileBytes = 1 << 20
	}
	if l.BatchFiles == 0 {
		l.BatchFiles = 32
	}
	if l.BatchBytes == 0 {
		l.BatchBytes = 4 << 20
	}
	if l.SearchFiles == 0 {
		l.SearchFiles = 1000
	}
	if l.SearchBytes == 0 {
		l.SearchBytes = 16 << 20
	}
	return l
}

func (l Limits) Validate() error {
	if l.FileBytes < 1 || l.FileBytes > 16<<20 || l.BatchFiles < 1 || l.BatchFiles > 100 ||
		l.BatchBytes < l.FileBytes || l.BatchBytes > 64<<20 || l.SearchFiles < 1 || l.SearchFiles > 100000 ||
		l.SearchBytes < l.FileBytes || l.SearchBytes > 256<<20 {
		return ErrInvalid
	}
	return nil
}

type Retriever struct {
	reader     Reader
	repository string
	limits     Limits
}

func New(reader Reader, repository string, limits Limits) (*Retriever, error) {
	limits = limits.Defaults()
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &Retriever{reader, repository, limits}, nil
}

func (r *Retriever) Resolve(ctx context.Context, kind, value string) (Revision, error) {
	if kind != "sha" && kind != "branch" && kind != "tag" {
		return Revision{}, ErrInvalid
	}
	revision, err := r.reader.Resolve(ctx, kind, value)
	revision.Repository = r.repository
	return revision, err
}

func ValidatePath(value string, root bool) error {
	if value == "" && root {
		return nil
	}
	if value == "" || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") ||
		strings.HasPrefix(value, "/") || value == "." || path.Clean(value) != value ||
		value == ".." || strings.HasPrefix(value, "../") {
		return ErrInvalid
	}
	return nil
}

func validateSource(sha, name string, root bool) error {
	if model.ValidateSHA(sha) != nil {
		return ErrInvalid
	}
	return ValidatePath(name, root)
}

// ErrorCode is shared by HTTP errors and individual batch failures.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalid):
		return "invalid_request"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrLimit):
		return "limit_exceeded"
	case errors.Is(err, ErrUnsupported):
		return "unsupported_content"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "upstream_failure"
	}
}
