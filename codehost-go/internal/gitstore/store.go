package gitstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codebase"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

type Store struct {
	path     string
	lockPath string
	remote   string
	provider string
	token    string
	origin   string
}

func New(root, remote, provider, token string) (*Store, error) {
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Git remote must be a credential-free HTTPS URL")
	}
	sum := sha256.Sum256([]byte(provider + "\x00" + remote + "\x00" + token))
	identity := hex.EncodeToString(sum[:12])
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	store := &Store{filepath.Join(root, identity), filepath.Join(root, identity+".lock"), remote, provider, token, parsed.Scheme + "://" + parsed.Host + "/"}
	err = store.lock(context.Background(), func() error {
		if _, err := os.Stat(filepath.Join(store.path, "HEAD")); err == nil {
			return nil
		}
		_, err := store.run(context.Background(), "init", "--bare", store.path)
		return err
	})
	return store, err
}

func (s *Store) lock(ctx context.Context, action func() error) error {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	file, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return codebase.ErrStorage
	}
	defer file.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK {
			return codebase.ErrStorage
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return action()
}

func (s *Store) fetch(ctx context.Context, ref string, repair bool) error {
	sum := sha256.Sum256([]byte(ref))
	destination := "refs/cache/fetched/" + hex.EncodeToString(sum[:12])
	args := []string{"-C", s.path, "fetch", "--no-tags", "--no-auto-maintenance"}
	if repair {
		args = append(args, "--refetch")
	}
	args = append(args, s.remote, "+"+ref+":"+destination)
	_, err := s.run(ctx, args...)
	return err
}

func (s *Store) Ensure(ctx context.Context, shas []string, hint string) error {
	for _, sha := range shas {
		if model.ValidateSHA(sha) != nil {
			return codebase.ErrInvalid
		}
	}
	if len(shas) == 0 {
		return nil
	}
	if hint != "" {
		if err := s.validateRef(ctx, hint); err != nil {
			return err
		}
	}
	return s.lock(ctx, func() error {
		missing := []string{}
		for _, sha := range shas {
			info, err := s.inspect(ctx, sha)
			if errors.Is(err, codebase.ErrNotFound) {
				missing = append(missing, sha)
				continue
			}
			if err != nil {
				return err
			}
			if info.kind != "commit" {
				return codebase.ErrInvalid
			}
		}
		if len(missing) == 0 {
			return nil
		}
		if hint != "" {
			if err := s.fetch(ctx, hint, false); err != nil && (ctx.Err() != nil || len(missing) > 1) {
				return err
			}
		}
		for _, sha := range missing {
			info, err := s.inspect(ctx, sha)
			if errors.Is(err, codebase.ErrNotFound) {
				if err = s.fetch(ctx, sha, false); err != nil {
					return err
				}
				info, err = s.inspect(ctx, sha)
			}
			if err != nil {
				return err
			}
			if info.kind != "commit" {
				return codebase.ErrInvalid
			}
			if _, err = s.run(ctx, "-C", s.path, "update-ref", "refs/cache/commits/"+sha, sha); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) validateRef(ctx context.Context, ref string) error {
	if !strings.HasPrefix(ref, "refs/") || strings.ContainsAny(ref, "\x00\r\n") {
		return codebase.ErrInvalid
	}
	_, err := s.run(ctx, "check-ref-format", ref)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return codebase.ErrInvalid
	}
	return nil
}

func (s *Store) Read(ctx context.Context, sha, hint string) (model.Commit, error) {
	if err := s.Ensure(ctx, []string{sha}, hint); err != nil {
		return model.Commit{}, err
	}
	output, err := s.run(ctx, "-C", s.path, "show", "-s", "--format=%cI%n%B", sha)
	if err != nil {
		return model.Commit{}, err
	}
	date, message, _ := strings.Cut(output, "\n")
	return model.Commit{SHA: sha, Message: strings.TrimRight(message, "\n"), CommittedAt: &date}, nil
}
