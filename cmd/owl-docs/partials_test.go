package main

import (
	"strings"
	"testing"
)

func TestExpandPartialsHappy(t *testing.T) {
	registerPartial("echo", func(args map[string]string) (string, error) {
		return "ECHO[" + args["text"] + "]", nil
	})
	t.Cleanup(func() { delete(partialRegistry, "echo") })

	got, err := expandPartials(`hello {{> echo text="world"}} done`)
	if err != nil {
		t.Fatal(err)
	}
	want := `hello ECHO[world] done`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestExpandPartialsUnknown(t *testing.T) {
	_, err := expandPartials(`hi {{> nope}}`)
	if err == nil || !strings.Contains(err.Error(), `unknown partial "nope"`) {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestExpandPartialsPropagatesError(t *testing.T) {
	registerPartial("bad", func(map[string]string) (string, error) {
		return "", errSomething
	})
	t.Cleanup(func() { delete(partialRegistry, "bad") })
	_, err := expandPartials(`x {{> bad}} y`)
	if err == nil {
		t.Fatal("expected error")
	}
}

var errSomething = errString("boom")

type errString string

func (e errString) Error() string { return string(e) }
