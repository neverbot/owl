package events

import (
	"bytes"
	"fmt"
	"text/template"
	"text/template/parse"
)

// compiledTemplate holds a parsed template together with the set of
// top-level field names it references so Render can pre-fill missing
// keys with "" before execution.
type compiledTemplate struct {
	tpl    *template.Template
	fields map[string]struct{}
}

// CompileTemplate parses src as a text/template. Absent payload keys
// render as the empty string because Render pre-fills missing keys
// before execution. Returns (nil, nil) for empty src — Render maps
// nil to "".
func CompileTemplate(name, src string) (*compiledTemplate, error) {
	if src == "" {
		return nil, nil
	}
	tpl, err := template.New(name).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("compile template %s: %w", name, err)
	}
	fields := collectFields(tpl.Root)
	return &compiledTemplate{tpl: tpl, fields: fields}, nil
}

// Render executes ct against payload. A nil ct returns "".
// Missing keys in payload render as "" rather than "<no value>".
// Execution errors degrade to "" so a single malformed payload
// doesn't break ingestion.
func Render(ct *compiledTemplate, payload map[string]any) string {
	if ct == nil {
		return ""
	}
	// Copy payload and pre-fill any referenced but absent keys with "".
	filled := make(map[string]any, len(payload)+len(ct.fields))
	for k, v := range payload {
		filled[k] = v
	}
	for k := range ct.fields {
		if _, ok := filled[k]; !ok {
			filled[k] = ""
		}
	}
	var buf bytes.Buffer
	if err := ct.tpl.Execute(&buf, filled); err != nil {
		return ""
	}
	return buf.String()
}

// collectFields walks a parse.ListNode and returns the set of
// single-element field names accessed as "{{.name}}" in the template.
func collectFields(list *parse.ListNode) map[string]struct{} {
	out := make(map[string]struct{})
	if list == nil {
		return out
	}
	walkNodes(list.Nodes, out)
	return out
}

// walkNodes recursively visits template AST nodes to collect field
// names from FieldNode leaves that are direct children of a dot
// (i.e. {{.name}} — single-identifier paths only).
func walkNodes(nodes []parse.Node, out map[string]struct{}) {
	for _, n := range nodes {
		switch v := n.(type) {
		case *parse.ActionNode:
			if v.Pipe != nil {
				for _, cmd := range v.Pipe.Cmds {
					for _, arg := range cmd.Args {
						if f, ok := arg.(*parse.FieldNode); ok && len(f.Ident) == 1 {
							out[f.Ident[0]] = struct{}{}
						}
					}
				}
			}
		case *parse.ListNode:
			walkNodes(v.Nodes, out)
		case *parse.IfNode:
			walkNodes(v.List.Nodes, out)
			if v.ElseList != nil {
				walkNodes(v.ElseList.Nodes, out)
			}
		case *parse.RangeNode:
			walkNodes(v.List.Nodes, out)
		case *parse.WithNode:
			walkNodes(v.List.Nodes, out)
		}
	}
}
