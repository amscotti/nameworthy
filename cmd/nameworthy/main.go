// Command nameworthy serves the one-page WoW: Forever name auditor.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/amscotti/nameworthy/internal/jev"
	"github.com/amscotti/nameworthy/internal/server"
)

const version = "0.1.0"

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("nameworthy", version)
		return
	}

	apiKey, err := resolveAPIKey(envFilePaths())
	if err != nil {
		log.Fatalf("nameworthy: loading config: %v", err)
	}
	if apiKey == "" {
		log.Print("nameworthy: TYPESAFE_API_KEY is unset — the page serves; auditing requires a key")
	}

	handler := server.New(server.JevJudge{Client: jev.NewClient(apiKey)}).Routes()
	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("nameworthy %s listening on %s", version, *addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("nameworthy: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("nameworthy: shutdown: %v", err)
	}
}

func envFilePaths() []string {
	paths := []string{".env"}
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), ".env"))
	}
	return paths
}

// resolveAPIKey returns TYPESAFE_API_KEY from the environment, falling back to
// the first .env file in paths when the variable is not already set.
func resolveAPIKey(paths []string) (string, error) {
	if _, configured := os.LookupEnv("TYPESAFE_API_KEY"); !configured {
		for _, path := range paths {
			if err := loadEnvFile(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			if _, found := os.LookupEnv("TYPESAFE_API_KEY"); found {
				break
			}
		}
	}
	return strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY")), nil
}

// loadEnvFile reads simple KEY=VALUE lines into the environment without
// overriding variables that are already set. Comment lines and blanks are
// skipped, an export prefix is accepted, and one matching pair of quotes is
// stripped. Read and invalid-key errors are returned to the caller.
func loadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') ||
			(value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		if key == "" {
			continue
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("invalid environment variable: %w", err)
		}
	}
	return nil
}
