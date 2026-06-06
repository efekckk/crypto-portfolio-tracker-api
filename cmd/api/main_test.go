package main

import (
	"testing"
)

func TestEnvOr_returnsFallbackWhenUnset(t *testing.T) {
	t.Setenv("FOO_TEST_UNSET", "")
	if got := envOr("FOO_TEST_UNSET", "default"); got != "default" {
		t.Fatalf("expected default, got %q", got)
	}
}

func TestEnvOr_returnsValueWhenSet(t *testing.T) {
	t.Setenv("FOO_TEST_SET", "from-env")
	if got := envOr("FOO_TEST_SET", "default"); got != "from-env" {
		t.Fatalf("expected from-env, got %q", got)
	}
}
