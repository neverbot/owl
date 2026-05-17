package main

import (
	"strings"
	"testing"
)

func TestConfigSchemaPartialMentionsKnownFields(t *testing.T) {
	out, err := configSchemaPartial(nil)
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		"`listen`",        // top-level scalar
		"`storage`",       // nested struct field reference
		"`StorageConfig`", // sub-section header
		"`retention`",     // nested field in storage
		"### `Config`",    // header for the root
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("config-schema output missing %q", w)
		}
	}
}

func TestConfigSchemaPartialEmitsDocTagText(t *testing.T) {
	out, err := configSchemaPartial(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "HTTP listen address") {
		t.Error("doc tag text for 'listen' field not rendered")
	}
}
