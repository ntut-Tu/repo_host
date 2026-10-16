package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/cache"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codebase"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/codehost"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/config"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/gitstore"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/httpapi"
	"github.com/ntut-Tu/test_Agent/services/codehost-go/internal/provider"
)

func handler(settings config.Settings) (http.Handler, error) {
	scope := sha256.Sum256([]byte(settings.Provider + "\x00" + settings.Repository + "\x00" + settings.APIURL + "\x00" + settings.UpstreamToken))
	files, err := cache.New(filepath.Join(settings.DataDirectory, "resources-"+hex.EncodeToString(scope[:12])))
	if err != nil {
		return nil, err
	}
	objects, err := gitstore.New(filepath.Join(settings.DataDirectory, "objects"), settings.GitURL, settings.Provider, settings.UpstreamToken)
	if err != nil {
		return nil, err
	}
	var reader codehost.Reader
	if settings.Provider == "github" {
		reader, err = provider.NewGitHub(settings.Repository, settings.APIURL, settings.UpstreamToken)
		if err != nil {
			return nil, err
		}
	} else {
		reader = provider.NewGitLab(settings.Repository, settings.APIURL, settings.UpstreamToken)
	}
	hosting := codehost.New(reader, objects, files, settings.Provider)
	source, err := codebase.New(objects, settings.Repository, settings.Retrieval)
	if err != nil {
		return nil, err
	}
	return httpapi.Handler(hosting, source, settings.APIToken), nil
}

func run() error {
	path := os.Getenv("CODEHOST_GO_VALUES")
	if path == "" {
		path = "values.yaml"
	}
	settings, err := config.Load(path)
	if err != nil {
		return errors.New("invalid codehost_go configuration")
	}
	routes, err := handler(settings)
	if err != nil {
		return errors.New("unable to initialize code-host service")
	}
	address := os.Getenv("CODEHOST_GO_ADDR")
	if address == "" {
		address = ":8082"
	}
	server := &http.Server{Addr: address, Handler: routes, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 65 * time.Second, WriteTimeout: 65 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	err = server.ListenAndServe()
	stop()
	<-stopped
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
