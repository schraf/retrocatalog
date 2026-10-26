package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unsetEnvForTest(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		old, existed := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(key, old)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func TestLoadDotEnv(t *testing.T) {
	keys := []string{
		"RETROCATALOG_DOTENV_ID",
		"RETROCATALOG_DOTENV_SECRET",
		"RETROCATALOG_DOTENV_SINGLE",
		"RETROCATALOG_DOTENV_INLINE",
		"RETROCATALOG_DOTENV_ESCAPED",
		"RETROCATALOG_DOTENV_EQUALS",
		"RETROCATALOG_DOTENV_EMPTY",
		"RETROCATALOG_DOTENV_HASH",
		"RETROCATALOG_DOTENV_TRAILING",
	}
	unsetEnvForTest(t, keys...)

	path := filepath.Join(t.TempDir(), ".env")
	content := `# IGDB credentials

RETROCATALOG_DOTENV_ID=abc
export RETROCATALOG_DOTENV_SECRET="s3cret value"
RETROCATALOG_DOTENV_SINGLE='single # stays'
RETROCATALOG_DOTENV_INLINE=hello # trailing comment
RETROCATALOG_DOTENV_ESCAPED="line\nbreak\t\"quoted\"\\"
RETROCATALOG_DOTENV_EQUALS=a=b=c
RETROCATALOG_DOTENV_EMPTY=
RETROCATALOG_DOTENV_HASH=#comment
RETROCATALOG_DOTENV_TRAILING="quoted" # comment
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"RETROCATALOG_DOTENV_ID":       "abc",
		"RETROCATALOG_DOTENV_SECRET":   "s3cret value",
		"RETROCATALOG_DOTENV_SINGLE":   "single # stays",
		"RETROCATALOG_DOTENV_INLINE":   "hello",
		"RETROCATALOG_DOTENV_ESCAPED":  "line\nbreak\t\"quoted\"\\",
		"RETROCATALOG_DOTENV_EQUALS":   "a=b=c",
		"RETROCATALOG_DOTENV_EMPTY":    "",
		"RETROCATALOG_DOTENV_HASH":     "",
		"RETROCATALOG_DOTENV_TRAILING": "quoted",
	}
	for key, expected := range want {
		if got := os.Getenv(key); got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestLoadDotEnvPreservesExistingVariables(t *testing.T) {
	keys := []string{"RETROCATALOG_DOTENV_KEEP", "RETROCATALOG_DOTENV_NEW"}
	unsetEnvForTest(t, keys...)
	t.Setenv("RETROCATALOG_DOTENV_KEEP", "outer")

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("RETROCATALOG_DOTENV_KEEP=inner\nRETROCATALOG_DOTENV_NEW=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("RETROCATALOG_DOTENV_KEEP"); got != "outer" {
		t.Fatalf("existing variable = %q, want outer", got)
	}
	if got := os.Getenv("RETROCATALOG_DOTENV_NEW"); got != "value" {
		t.Fatalf("new variable = %q, want value", got)
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatalf("missing .env should not be an error: %v", err)
	}
}

func TestLoadDotEnvRejectsMalformedLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"no equals", "RETROCATALOG_DOTENV_BAD\n"},
		{"empty key", "=value\n"},
		{"invalid key", "1BAD=value\n"},
		{"unterminated quote", `RETROCATALOG_DOTENV_BAD="open` + "\n"},
		{"text after quote", `RETROCATALOG_DOTENV_BAD="closed" trailing` + "\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			err := loadDotEnv(path)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), ".env:1") {
				t.Fatalf("error = %q, want file and line number", err)
			}
		})
	}
}
