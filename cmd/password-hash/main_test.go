package main

import (
	"bytes"
	"strings"
	"testing"

	assistantauth "github.com/EurekaMXZ/assistant/internal/auth"
)

func TestResolvePasswordPrefersFlag(t *testing.T) {
	password, err := resolvePassword(options{password: "secret123", envVar: defaultPasswordEnvVar}, nil, strings.NewReader("ignored\n"), &bytes.Buffer{}, func(string) (string, bool) {
		return "from-env", true
	}, true)
	if err != nil {
		t.Fatalf("resolve password: %v", err)
	}
	if password != "secret123" {
		t.Fatalf("password = %q, want %q", password, "secret123")
	}
}

func TestResolvePasswordUsesPositionalArgument(t *testing.T) {
	password, err := resolvePassword(options{envVar: defaultPasswordEnvVar}, []string{"secret123"}, strings.NewReader("ignored\n"), &bytes.Buffer{}, func(string) (string, bool) {
		return "from-env", true
	}, true)
	if err != nil {
		t.Fatalf("resolve password: %v", err)
	}
	if password != "secret123" {
		t.Fatalf("password = %q, want %q", password, "secret123")
	}
}

func TestResolvePasswordUsesEnvironmentFallback(t *testing.T) {
	password, err := resolvePassword(options{envVar: defaultPasswordEnvVar}, nil, strings.NewReader("ignored\n"), &bytes.Buffer{}, func(key string) (string, bool) {
		if key != defaultPasswordEnvVar {
			t.Fatalf("lookupEnv key = %q, want %q", key, defaultPasswordEnvVar)
		}
		return "secret123", true
	}, true)
	if err != nil {
		t.Fatalf("resolve password: %v", err)
	}
	if password != "secret123" {
		t.Fatalf("password = %q, want %q", password, "secret123")
	}
}

func TestResolvePasswordUsesPipedInput(t *testing.T) {
	password, err := resolvePassword(options{envVar: defaultPasswordEnvVar}, nil, strings.NewReader("secret123\n"), &bytes.Buffer{}, func(string) (string, bool) {
		return "", false
	}, false)
	if err != nil {
		t.Fatalf("resolve password: %v", err)
	}
	if password != "secret123" {
		t.Fatalf("password = %q, want %q", password, "secret123")
	}
}

func TestRunWritesProjectCompatibleHash(t *testing.T) {
	var stdout bytes.Buffer
	if err := run([]string{"-password", "secret123"}, strings.NewReader(""), &stdout, &bytes.Buffer{}, func(string) (string, bool) {
		return "", false
	}, false); err != nil {
		t.Fatalf("run: %v", err)
	}

	hash := strings.TrimSpace(stdout.String())
	if hash == "" {
		t.Fatal("expected hash output")
	}
	if err := assistantauth.ComparePasswordHash(hash, "secret123"); err != nil {
		t.Fatalf("compare password hash: %v", err)
	}
}

func TestResolvePasswordRejectsMultipleExplicitSources(t *testing.T) {
	_, err := resolvePassword(options{password: "secret123", envVar: defaultPasswordEnvVar}, []string{"another-secret"}, strings.NewReader(""), &bytes.Buffer{}, func(string) (string, bool) {
		return "", false
	}, false)
	if err == nil {
		t.Fatal("expected an error")
	}
}
