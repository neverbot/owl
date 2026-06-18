package config

import (
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

func mustExpand(t *testing.T, in string, env map[string]string, files fstest.MapFS) string {
	t.Helper()
	out, err := expandString(in, envFunc(env), fileFunc(files))
	if err != nil {
		t.Fatalf("expandString(%q): unexpected error: %v", in, err)
	}
	return out
}

func envFunc(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func fileFunc(fs fstest.MapFS) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		return fs.ReadFile(strings.TrimPrefix(p, "/"))
	}
}

func TestExpandString_Literal(t *testing.T) {
	got := mustExpand(t, "plain text", nil, nil)
	if got != "plain text" {
		t.Fatalf("want %q, got %q", "plain text", got)
	}
}

func TestExpandString_EnvSet(t *testing.T) {
	got := mustExpand(t, "${FOO}", map[string]string{"FOO": "bar"}, nil)
	if got != "bar" {
		t.Fatalf("want %q, got %q", "bar", got)
	}
}

func TestExpandString_EnvUnset(t *testing.T) {
	_, err := expandString("${MISSING}", envFunc(nil), nil)
	if err == nil {
		t.Fatal("expected error for unset variable")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("error %q should name the missing variable", err)
	}
}

func TestExpandString_EnvDefault(t *testing.T) {
	got := mustExpand(t, "${FOO:-fallback}", nil, nil)
	if got != "fallback" {
		t.Fatalf("want %q, got %q", "fallback", got)
	}
	got = mustExpand(t, "${FOO:-fallback}", map[string]string{"FOO": "real"}, nil)
	if got != "real" {
		t.Fatalf("want %q, got %q", "real", got)
	}
	got = mustExpand(t, "${FOO:-}", nil, nil)
	if got != "" {
		t.Fatalf("want empty string, got %q", got)
	}
}

func TestExpandString_Escape(t *testing.T) {
	got := mustExpand(t, "$${FOO}", map[string]string{"FOO": "leak"}, nil)
	if got != "${FOO}" {
		t.Fatalf("want %q, got %q", "${FOO}", got)
	}
}

func TestExpandString_Mixed(t *testing.T) {
	got := mustExpand(t, "Bearer ${TOK}", map[string]string{"TOK": "abc"}, nil)
	if got != "Bearer abc" {
		t.Fatalf("want %q, got %q", "Bearer abc", got)
	}
}

func TestExpandString_File(t *testing.T) {
	fs := fstest.MapFS{"run/secrets/x": {Data: []byte("hunter2\n")}}
	got := mustExpand(t, "file:/run/secrets/x", nil, fs)
	if got != "hunter2" {
		t.Fatalf("want %q, got %q", "hunter2", got)
	}
}

func TestExpandString_FileMissing(t *testing.T) {
	fs := fstest.MapFS{}
	_, err := expandString("file:/nope", nil, fileFunc(fs))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestExpandString_FileRelativeRejected(t *testing.T) {
	_, err := expandString("file:relative/path", nil, fileFunc(fstest.MapFS{}))
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute-path error, got %v", err)
	}
}

func TestExpandString_UnterminatedBrace(t *testing.T) {
	_, err := expandString("${FOO", envFunc(map[string]string{"FOO": "x"}), nil)
	if err == nil {
		t.Fatal("expected unterminated-brace error")
	}
}

func TestExpandNode_NestedMap(t *testing.T) {
	src := []byte(`
targets:
  - name: t
    auth:
      bearer_token: ${TOK}
      headers:
        X-API-Key: ${KEY}
`)
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	env := envFunc(map[string]string{"TOK": "abc", "KEY": "k1"})
	if err := expandNode(&doc, env, nil); err != nil {
		t.Fatalf("expandNode: %v", err)
	}
	out, _ := yaml.Marshal(&doc)
	if !strings.Contains(string(out), "bearer_token: abc") {
		t.Fatalf("expected expanded bearer_token, got:\n%s", out)
	}
	if !strings.Contains(string(out), "X-API-Key: k1") {
		t.Fatalf("expected expanded X-API-Key, got:\n%s", out)
	}
}

func TestExpandNode_ErrorBubblesUp(t *testing.T) {
	src := []byte(`
targets:
  - auth:
      bearer_token: ${MISSING}
`)
	var doc yaml.Node
	_ = yaml.Unmarshal(src, &doc)
	err := expandNode(&doc, envFunc(nil), nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("error %q should name the variable", err)
	}
}
