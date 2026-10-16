package gitstore

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codebase"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/model"
)

func (s *Store) Resolve(ctx context.Context, kind, value string) (codebase.Revision, error) {
	sha := value
	switch kind {
	case "sha":
		if err := s.Ensure(ctx, []string{sha}, ""); err != nil {
			return codebase.Revision{}, err
		}
	case "branch", "tag":
		prefix := "refs/heads/"
		if kind == "tag" {
			prefix = "refs/tags/"
		}
		ref := prefix + value
		if err := s.validateRef(ctx, ref); err != nil {
			return codebase.Revision{}, err
		}
		err := s.lock(ctx, func() error {
			if err := s.fetch(ctx, ref, false); err != nil {
				return err
			}
			output, err := s.run(ctx, "-C", s.path, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
			if err != nil {
				return err
			}
			sha = strings.TrimSpace(output)
			_, err = s.run(ctx, "-C", s.path, "update-ref", "refs/cache/commits/"+sha, sha)
			return err
		})
		if err != nil {
			return codebase.Revision{}, err
		}
	default:
		return codebase.Revision{}, codebase.ErrInvalid
	}
	root, err := s.root(ctx, sha)
	return codebase.Revision{SHA: sha, Tree: root}, err
}

func (s *Store) root(ctx context.Context, sha string) (string, error) {
	data, err := s.command(ctx, "", 4<<20, "-C", s.path, "cat-file", "commit", sha)
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(string(data), "\n")
	oid := strings.TrimPrefix(first, "tree ")
	if first == oid || model.ValidateSHA(oid) != nil {
		return "", codebase.ErrStorage
	}
	if _, err = s.object(ctx, sha, oid); err != nil {
		return "", err
	}
	return oid, nil
}

func (s *Store) object(ctx context.Context, sha, oid string) (objectInfo, error) {
	info, err := s.inspect(ctx, oid)
	if errors.Is(err, codebase.ErrNotFound) {
		if err = s.repair(ctx, sha, oid); err != nil {
			return objectInfo{}, err
		}
		return s.inspect(ctx, oid)
	}
	return info, err
}

func (s *Store) treeEntries(ctx context.Context, sha, oid, prefix string) ([]codebase.Entry, error) {
	info, err := s.object(ctx, sha, oid)
	if err != nil {
		return nil, err
	}
	if info.kind != "tree" {
		return nil, codebase.ErrInvalid
	}
	raw, err := s.command(ctx, "", 16<<20, "-C", s.path, "ls-tree", "-z", oid)
	if err != nil {
		return nil, err
	}
	entries := []codebase.Entry{}
	for _, record := range strings.Split(string(raw), "\x00") {
		if record == "" {
			continue
		}
		header, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 {
			return nil, codebase.ErrStorage
		}
		full := name
		if prefix != "" {
			full = prefix + "/" + name
		}
		if codebase.ValidatePath(full, false) != nil {
			return nil, codebase.ErrUnsupported
		}
		entries = append(entries, codebase.Entry{Path: full, OID: fields[2], Mode: fields[0], Type: fields[1]})
	}
	return entries, nil
}

// Walk each path component literally; never interpret it as a revision or pathspec.
func (s *Store) entry(ctx context.Context, sha, name string) (codebase.Entry, error) {
	if model.ValidateSHA(sha) != nil || codebase.ValidatePath(name, true) != nil {
		return codebase.Entry{}, codebase.ErrInvalid
	}
	if err := s.Ensure(ctx, []string{sha}, ""); err != nil {
		return codebase.Entry{}, err
	}
	root, err := s.root(ctx, sha)
	if err != nil {
		return codebase.Entry{}, err
	}
	current := codebase.Entry{OID: root, Type: "tree", Mode: "040000"}
	if name == "" {
		return current, nil
	}
	for _, part := range strings.Split(name, "/") {
		if current.Type != "tree" {
			return codebase.Entry{}, codebase.ErrNotFound
		}
		entries, err := s.treeEntries(ctx, sha, current.OID, current.Path)
		if err != nil {
			return codebase.Entry{}, err
		}
		found := false
		for _, entry := range entries {
			if path.Base(entry.Path) == part {
				current = entry
				found = true
				break
			}
		}
		if !found {
			return codebase.Entry{}, codebase.ErrNotFound
		}
	}
	return current, nil
}

func (s *Store) Tree(ctx context.Context, sha, name string, recursive bool) ([]codebase.Entry, error) {
	entry, err := s.entry(ctx, sha, name)
	if err != nil {
		return nil, err
	}
	if entry.Type != "tree" {
		return nil, codebase.ErrInvalid
	}
	entries := []codebase.Entry{}
	queue := []codebase.Entry{entry}
	total := 0
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		children, err := s.treeEntries(ctx, sha, next.OID, next.Path)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			total += len(child.Path) + len(child.OID) + 32
			if total > 16<<20 || len(entries) >= 100000 {
				return nil, codebase.ErrLimit
			}
			entries = append(entries, child)
			if recursive && child.Type == "tree" {
				queue = append(queue, child)
			}
		}
	}
	return entries, nil
}

func (s *Store) File(ctx context.Context, sha, name string, limit int) (codebase.Entry, []byte, error) {
	if name == "" || limit < 1 {
		return codebase.Entry{}, nil, codebase.ErrInvalid
	}
	entry, err := s.entry(ctx, sha, name)
	if err != nil {
		return entry, nil, err
	}
	if entry.Type != "blob" {
		return entry, nil, codebase.ErrUnsupported
	}
	data, err := s.Content(ctx, sha, entry, limit)
	return entry, data, err
}

// Content reads an entry returned by Tree without walking its path again.
func (s *Store) Content(ctx context.Context, sha string, entry codebase.Entry, limit int) ([]byte, error) {
	if model.ValidateSHA(sha) != nil || model.ValidateSHA(entry.OID) != nil || limit < 1 {
		return nil, codebase.ErrInvalid
	}
	if entry.Type != "blob" {
		return nil, codebase.ErrUnsupported
	}
	info, err := s.object(ctx, sha, entry.OID)
	if err != nil {
		return nil, err
	}
	if info.kind != "blob" {
		return nil, codebase.ErrStorage
	}
	if info.size > limit {
		return nil, codebase.ErrLimit
	}
	return s.command(ctx, "", limit, "-C", s.path, "cat-file", "blob", entry.OID)
}
