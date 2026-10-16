package gitstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codebase"
)

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buffer.Len() {
		b.exceeded = true
		return len(data), nil
	}
	return b.buffer.Write(data)
}

func (s *Store) environment() []string {
	env := []string{}
	for _, value := range os.Environ() {
		// Do not inherit repository overrides or injected Git configuration.
		if !strings.HasPrefix(value, "GIT_") {
			env = append(env, value)
		}
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_LAZY_FETCH=1")
	settings := [][2]string{{"http.followRedirects", "false"}, {"credential.helper", ""}, {"gc.auto", "0"}}
	if s.token != "" {
		user := "oauth2"
		if s.provider == "github" {
			user = "x-access-token"
		}
		header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+s.token))
		settings = append(settings, [2]string{"http." + s.origin + ".extraheader", header})
	}
	env = append(env, "GIT_CONFIG_COUNT="+strconv.Itoa(len(settings)))
	for i, item := range settings {
		env = append(env, "GIT_CONFIG_KEY_"+strconv.Itoa(i)+"="+item[0], "GIT_CONFIG_VALUE_"+strconv.Itoa(i)+"="+item[1])
	}
	return env
}

func (s *Store) command(parent context.Context, input string, limit int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.Env = s.environment()
	command.Stdin = strings.NewReader(input)
	output := &boundedBuffer{limit: limit}
	command.Stdout = output
	diagnostics := &boundedBuffer{limit: 4096}
	command.Stderr = diagnostics
	err := command.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if output.exceeded {
		return nil, codebase.ErrLimit
	}
	if err != nil {
		return nil, codebase.ErrStorage
	}
	if strings.HasPrefix(args[len(args)-1], "--batch-check") && (diagnostics.buffer.Len() > 0 || diagnostics.exceeded) {
		return nil, codebase.ErrStorage
	}
	return output.buffer.Bytes(), nil
}

func (s *Store) run(ctx context.Context, args ...string) (string, error) {
	data, err := s.command(ctx, "", 16<<20, args...)
	return string(data), err
}

type objectInfo struct {
	oid  string
	kind string
	size int
}

func (s *Store) inspect(ctx context.Context, expression string) (objectInfo, error) {
	data, err := s.command(ctx, expression+"\n", 1024, "-C", s.path, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if err != nil {
		return objectInfo{}, err
	}
	value := strings.TrimSpace(string(data))
	if strings.HasSuffix(value, " missing") {
		return objectInfo{}, codebase.ErrNotFound
	}
	fields := strings.Fields(value)
	if len(fields) != 3 {
		return objectInfo{}, codebase.ErrStorage
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size < 0 {
		return objectInfo{}, codebase.ErrStorage
	}
	return objectInfo{fields[0], fields[1], size}, nil
}

func (s *Store) repair(ctx context.Context, sha, oid string) error {
	return s.lock(ctx, func() error {
		if _, err := s.inspect(ctx, oid); !errors.Is(err, codebase.ErrNotFound) {
			return err
		}
		if err := s.fetch(ctx, sha, true); err != nil {
			return err
		}
		_, err := s.inspect(ctx, oid)
		return err
	})
}
