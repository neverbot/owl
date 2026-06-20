package events

import "testing"

// TestRender covers a happy-path template and a missing-key case
// which must render as the empty string (no error).
func TestRender(t *testing.T) {
	tpl, err := CompileTemplate("src", "{{.container}} {{.from}}→{{.to}}")
	if err != nil {
		t.Fatal(err)
	}
	got := Render(tpl, map[string]any{"container": "nginx", "from": "1.0", "to": "1.1"})
	if got != "nginx 1.0→1.1" {
		t.Fatalf("got %q", got)
	}
}

// TestRenderMissingKey verifies that missing keys render as empty
// string rather than "<no value>" or an error.
func TestRenderMissingKey(t *testing.T) {
	tpl, err := CompileTemplate("src", "{{.a}}-{{.b}}")
	if err != nil {
		t.Fatal(err)
	}
	got := Render(tpl, map[string]any{"a": "x"})
	if got != "x-" {
		t.Fatalf("got %q", got)
	}
}

// TestCompileTemplateEmpty returns nil template; Render handles nil
// by returning "".
func TestCompileTemplateEmpty(t *testing.T) {
	tpl, err := CompileTemplate("src", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := Render(tpl, nil); got != "" {
		t.Fatalf("got %q", got)
	}
}
