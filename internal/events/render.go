package events

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// CompileTemplate parses src as a text/template. Absent payload keys
// render as "" rather than "<no value>" thanks to Render's
// post-processing step. Returns (nil, nil) for empty src — Render
// maps nil to "".
func CompileTemplate(name, src string) (*template.Template, error) {
	if src == "" {
		return nil, nil
	}
	tpl, err := template.New(name).Option("missingkey=zero").Parse(src)
	if err != nil {
		return nil, fmt.Errorf("compile template %s: %w", name, err)
	}
	return tpl, nil
}

// Render executes tpl against payload. A nil template returns "".
// Missing payload keys are reduced to "" (text/template emits
// "<no value>" for nil interfaces even with missingkey=zero on
// map[string]any; this post-processing pass is the standard
// workaround). Execution errors degrade to "" so a single
// malformed payload doesn't break ingestion.
func Render(tpl *template.Template, payload map[string]any) string {
	if tpl == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, payload); err != nil {
		return ""
	}
	return strings.ReplaceAll(buf.String(), "<no value>", "")
}
