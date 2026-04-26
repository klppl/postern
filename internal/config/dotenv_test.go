package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	contents := `
# a comment
FOO=bar
QUOTED="hello world"
SINGLE='one two'
export EXPORTED=yes
WITH_HASH=value # trailing
EMPTY=
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"FOO", "QUOTED", "SINGLE", "EXPORTED", "WITH_HASH", "EMPTY"} {
		os.Unsetenv(k)
	}
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"FOO":       "bar",
		"QUOTED":    "hello world",
		"SINGLE":    "one two",
		"EXPORTED":  "yes",
		"WITH_HASH": "value",
		"EMPTY":     "",
	}
	for k, want := range cases {
		got := os.Getenv(k)
		if got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadDotEnvDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	os.WriteFile(path, []byte("ALREADY=fromfile\n"), 0o600)
	os.Setenv("ALREADY", "fromshell")
	t.Cleanup(func() { os.Unsetenv("ALREADY") })
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("ALREADY"); got != "fromshell" {
		t.Fatalf("expected shell value to win, got %q", got)
	}
}

func TestLoadDotEnvMissing(t *testing.T) {
	if err := LoadDotEnv("/nonexistent/.env"); err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
}
