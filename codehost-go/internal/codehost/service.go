package codehost

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

const mutableTTL = 30 * time.Second

type Git interface {
	Ensure(context.Context, []string, string) error
	Read(context.Context, string, string) (model.Commit, error)
}

type Service struct {
	reader   Reader
	git      Git
	cache    Cache
	provider string
}

type Reader interface {
	Snapshot(context.Context, int) (model.Snapshot, error)
	Issue(context.Context, int) (model.Issue, error)
	FetchRef(int) string
}

type Cache interface {
	Get(string, string, any) bool
	Put(string, string, any, time.Duration) error
}

func New(reader Reader, git Git, cache Cache, providerName string) *Service {
	return &Service{reader, git, cache, providerName}
}

func (s *Service) snapshot(ctx context.Context, number int) (model.Snapshot, error) {
	key := strconv.Itoa(number)
	var snapshot model.Snapshot
	if s.cache.Get("snapshot", key, &snapshot) {
		return snapshot, nil
	}
	snapshot, err := s.reader.Snapshot(ctx, number)
	if err != nil {
		return model.Snapshot{}, err
	}
	if err := s.cache.Put("snapshot", key, snapshot, mutableTTL); err != nil {
		return model.Snapshot{}, err
	}
	return snapshot, nil
}

func (s *Service) Commit(ctx context.Context, sha string) (model.Commit, error) {
	if err := model.ValidateSHA(sha); err != nil {
		return model.Commit{}, err
	}
	return s.git.Read(ctx, sha, "")
}

func (s *Service) PR(ctx context.Context, number int) (model.Change, error) {
	snapshot, err := s.snapshot(ctx, number)
	if err != nil {
		return model.Change{}, err
	}
	if err := s.git.Ensure(ctx, []string{snapshot.Change.HeadSHA}, s.reader.FetchRef(number)); err != nil {
		return model.Change{}, err
	}
	return snapshot.Change, nil
}

func (s *Service) Issue(ctx context.Context, number int) (model.Issue, error) {
	key := strconv.Itoa(number)
	var issue model.Issue
	if s.cache.Get("issue", key, &issue) {
		return issue, nil
	}
	issue, err := s.reader.Issue(ctx, number)
	if err != nil {
		return model.Issue{}, err
	}
	if err := s.cache.Put("issue", key, issue, mutableTTL); err != nil {
		return model.Issue{}, err
	}
	return issue, nil
}

func (s *Service) Timeline(ctx context.Context, number int) (model.Timeline, error) {
	snapshot, err := s.snapshot(ctx, number)
	if err != nil {
		return model.Timeline{}, err
	}
	shas := make([]string, 0)
	for _, event := range snapshot.Events {
		if event.Kind == "commit" && event.SHA != nil {
			shas = append(shas, *event.SHA)
		}
	}
	if err := s.git.Ensure(ctx, shas, s.reader.FetchRef(number)); err != nil {
		return model.Timeline{}, err
	}
	for index := range snapshot.Events {
		event := &snapshot.Events[index]
		if event.Kind != "commit" || event.SHA == nil {
			continue
		}
		commit, err := s.git.Read(ctx, *event.SHA, s.reader.FetchRef(number))
		if err != nil {
			return model.Timeline{}, err
		}
		event.Message = &commit.Message
	}
	ordering := "GitHub PR timeline order"
	if s.provider == "gitlab" {
		ordering = "commit time and comment creation time; push time is unavailable"
	}
	if snapshot.Events == nil {
		snapshot.Events = []model.Event{}
	}
	return model.Timeline{Change: snapshot.Change, Events: snapshot.Events, OrderingBasis: ordering}, nil
}

func ValidateNumber(number int) error {
	if number <= 0 {
		return errors.New("number must be positive")
	}
	return nil
}
