package config

import (
	"errors"
	"net/url"
	"os"
	"strings"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codebase"
	"gopkg.in/yaml.v3"
)

type Settings struct {
	Retrieval     codebase.Limits `yaml:"retrieval"`
	Provider      string          `yaml:"provider"`
	Repository    string          `yaml:"repository"`
	UpstreamToken string          `yaml:"upstream_token"`
	APIURL        string          `yaml:"api_url"`
	GitURL        string          `yaml:"git_url"`
	APIToken      string          `yaml:"api_token"`
	DataDirectory string          `yaml:"data_directory"`
}

func Load(path string) (Settings, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, err
	}
	var document struct {
		CodehostGo Settings `yaml:"codehost_go"`
	}
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return Settings{}, err
	}
	settings := document.CodehostGo
	settings.Retrieval = settings.Retrieval.Defaults()
	if err := settings.Retrieval.Validate(); err != nil {
		return Settings{}, errors.New("invalid codehost_go.retrieval limits")
	}
	if settings.Provider != "github" && settings.Provider != "gitlab" {
		return Settings{}, errors.New("codehost_go.provider must be github or gitlab")
	}
	if settings.Repository == "" || len(settings.APIToken) < 32 {
		return Settings{}, errors.New("codehost_go.repository and a 32-character api_token are required")
	}
	if settings.APIURL == "" {
		settings.APIURL = map[string]string{"github": "https://api.github.com", "gitlab": "https://gitlab.com/api/v4"}[settings.Provider]
	}
	if settings.GitURL == "" {
		settings.GitURL, err = defaultGitURL(settings)
		if err != nil {
			return Settings{}, err
		}
	}
	if value := os.Getenv("CODEHOST_GO_DATA"); value != "" {
		settings.DataDirectory = value
	} else if settings.DataDirectory == "" {
		settings.DataDirectory = "./data/codehost-go"
	}
	return settings, nil
}

func defaultGitURL(settings Settings) (string, error) {
	if settings.Provider == "github" && strings.TrimRight(settings.APIURL, "/") == "https://api.github.com" {
		return "https://github.com/" + settings.Repository + ".git", nil
	}
	parsed, err := url.Parse(settings.APIURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("codehost_go.api_url must be HTTPS")
	}
	want := "/api/v4"
	if settings.Provider == "github" {
		want = "/api/v3"
	}
	if strings.TrimRight(parsed.Path, "/") != want || strings.IndexFunc(settings.Repository, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
		return "", errors.New("codehost_go.git_url is required for this API URL or numeric project")
	}
	return parsed.Scheme + "://" + parsed.Host + "/" + settings.Repository + ".git", nil
}
