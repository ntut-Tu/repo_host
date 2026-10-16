package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIsolatesGoSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "values.yaml")
	contents := "code_host:\n  provider: invalid\ncodehost_service:\n  provider: invalid\n" +
		"codehost_go:\n  provider: github\n  repository: owner/repo\n" +
		"  api_token: 12345678901234567890123456789012\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Provider != "github" || settings.GitURL != "https://github.com/owner/repo.git" {
		t.Fatalf("wrong settings: provider=%s git_url=%s", settings.Provider, settings.GitURL)
	}
}

func TestRetrievalLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "values.yaml")
	prefix := "codehost_go:\n  provider: github\n  repository: owner/repo\n  api_token: 12345678901234567890123456789012\n  retrieval:\n"
	if err := os.WriteFile(path, []byte(prefix+"    file_bytes: 1024\n    search_files: 12\n"), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := Load(path)
	if err != nil || settings.Retrieval.FileBytes != 1024 || settings.Retrieval.SearchFiles != 12 {
		t.Fatalf("limits: %v", err)
	}
	if err := os.WriteFile(path, []byte(prefix+"    file_bytes: -1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("negative retrieval budget accepted")
	}
}
