package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	for _, key := range []string{"PLAIN", "QUOTED", "EMPTYKEY", "EXPORTED", "LITERAL", "UNMATCHED"} {
		t.Setenv(key, "") // register restoration, then simulate an unset variable
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	content := "# comment line\n\nPLAIN=hello\nQUOTED='world'\nEXISTING=fromfile\nBAD LINE\nEMPTYKEY=\nexport EXPORTED=\"with spaces\"\nLITERAL=don't-strip\nUNMATCHED='keep\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("EXISTING", "keepme")

	if err := loadEnvFile(path); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if got := os.Getenv("PLAIN"); got != "hello" {
		t.Errorf("PLAIN = %q, want hello", got)
	}
	if got := os.Getenv("QUOTED"); got != "world" {
		t.Errorf("QUOTED = %q, want world (quotes stripped)", got)
	}
	if got := os.Getenv("EXISTING"); got != "keepme" {
		t.Errorf("EXISTING = %q, want keepme (no override)", got)
	}
	for key, want := range map[string]string{
		"EXPORTED": "with spaces", "LITERAL": "don't-strip", "UNMATCHED": "'keep",
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadEnvFileMissing(t *testing.T) {
	if err := loadEnvFile(filepath.Join(t.TempDir(), "nope.env")); err == nil {
		t.Error("missing file must return an error")
	}
}

func TestResolveAPIKeyPrecedence(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "cwd.env")
	fallback := filepath.Join(dir, "bin.env")
	if err := os.WriteFile(primary, []byte("TYPESAFE_API_KEY=  from-cwd  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallback, []byte("TYPESAFE_API_KEY=from-bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Explicit value in the environment wins over .env files.
	t.Setenv("TYPESAFE_API_KEY", "from-env")
	if got, err := resolveAPIKey([]string{primary, fallback}); err != nil || got != "from-env" {
		t.Fatalf("explicit env: got %q, err %v; want from-env", got, err)
	}

	// Explicitly empty TYPESAFE_API_KEY="" disables .env fallback (used for keyless smoke runs).
	t.Setenv("TYPESAFE_API_KEY", "")
	if got, err := resolveAPIKey([]string{primary, fallback}); err != nil || got != "" {
		t.Fatalf("empty env: got %q, err %v; want empty key", got, err)
	}

	// When unset, working-directory .env precedes binary-directory .env.
	if err := os.Unsetenv("TYPESAFE_API_KEY"); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveAPIKey([]string{primary, fallback}); err != nil || got != "from-cwd" {
		t.Fatalf("cwd fallback: got %q, err %v; want from-cwd", got, err)
	}

	// Missing primary .env falls through cleanly to binary-directory .env.
	if err := os.Unsetenv("TYPESAFE_API_KEY"); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.env")
	if got, err := resolveAPIKey([]string{missing, fallback}); err != nil || got != "from-bin" {
		t.Fatalf("bin fallback: got %q, err %v; want from-bin", got, err)
	}

	paths := envFilePaths()
	if len(paths) == 0 || paths[0] != ".env" {
		t.Fatalf("envFilePaths() = %v, want .env first", paths)
	}
}
