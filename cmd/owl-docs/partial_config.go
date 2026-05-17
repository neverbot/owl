package main

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/neverbot/owl/internal/config"
)

func init() {
	registerPartial("config-schema", configSchemaPartial)
}

// configSchemaPartial reflects over [config.Config] and every struct it
// transitively contains, emitting one Markdown table per struct. Each
// row shows the YAML name, type, default (from [config.Default]) and
// the `doc:"…"` description attached to that field.
func configSchemaPartial(args map[string]string) (string, error) {
	root := reflect.TypeOf(config.Config{})
	defaults := reflect.ValueOf(config.Default())
	var b strings.Builder
	visited := map[string]bool{}
	walkConfigStruct(&b, root, defaults, "Config", visited)
	return b.String(), nil
}

// nestedField captures a struct-typed field discovered while walking
// the config tree, so its own table can be rendered after the parent's.
type nestedField struct {
	t           reflect.Type
	defaults    reflect.Value
	displayName string
}

// walkConfigStruct emits a Markdown table for t and then recurses into
// every nested struct field (or struct-element slice) it contains.
// visited prevents infinite recursion on self-referential types.
func walkConfigStruct(b *strings.Builder, t reflect.Type, defaults reflect.Value,
	displayName string, visited map[string]bool) {
	if visited[t.String()] {
		return
	}
	visited[t.String()] = true

	fmt.Fprintf(b, "\n### `%s`\n\n", displayName)
	fmt.Fprintf(b, "| Field | Type | Default | Description |\n|---|---|---|---|\n")
	var nested []nestedField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		yamlName := strings.SplitN(f.Tag.Get("yaml"), ",", 2)[0]
		if yamlName == "" || yamlName == "-" {
			continue
		}
		doc := f.Tag.Get("doc")
		def := defaultLiteral(defaults, i)
		fmt.Fprintf(b, "| `%s` | `%s` | %s | %s |\n",
			yamlName, prettyType(f.Type), def, escapePipe(doc))
		if isNestedStruct(f.Type) {
			child := reflect.Value{}
			if defaults.IsValid() && defaults.Kind() == reflect.Struct {
				child = defaults.Field(i)
			}
			nested = append(nested, nestedField{
				t:           dereferenceStruct(f.Type),
				defaults:    child,
				displayName: typeDisplayName(f.Type),
			})
		}
	}
	// Render nested structs sorted by name for deterministic output.
	sort.Slice(nested, func(i, j int) bool { return nested[i].displayName < nested[j].displayName })
	for _, n := range nested {
		// For slice-of-struct fields, the defaults Value is a slice;
		// fall back to a zero struct for column rendering.
		defs := n.defaults
		if defs.IsValid() && defs.Kind() == reflect.Slice {
			defs = reflect.New(n.t).Elem()
		}
		walkConfigStruct(b, n.t, defs, n.displayName, visited)
	}
}

// isNestedStruct reports whether t is a struct or a slice/array whose
// element is a struct, i.e. a type worth rendering as its own table.
func isNestedStruct(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Struct:
		return true
	case reflect.Slice, reflect.Array:
		return t.Elem().Kind() == reflect.Struct
	default:
		return false
	}
}

// dereferenceStruct returns the underlying struct type for a slice or
// array of struct; otherwise it returns t unchanged.
func dereferenceStruct(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return t.Elem()
	}
	return t
}

// typeDisplayName returns the header label used for a nested struct's
// table, decorated with `[]` for slice-of-struct fields.
func typeDisplayName(t reflect.Type) string {
	if t.Kind() == reflect.Slice {
		return "[]" + t.Elem().Name()
	}
	return t.Name()
}

// prettyType renders a Go type as the docs table sees it: composite
// types are expanded ("[]string", "map[string]string") and named
// structs collapse to their short name.
func prettyType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Slice:
		return "[]" + prettyType(t.Elem())
	case reflect.Map:
		return fmt.Sprintf("map[%s]%s", prettyType(t.Key()), prettyType(t.Elem()))
	case reflect.Struct:
		return t.Name()
	default:
		return t.Kind().String()
	}
}

// defaultLiteral pretty-prints the i-th field of defaults as a
// backtick-quoted literal, or "—" when no usable default exists.
func defaultLiteral(defaults reflect.Value, idx int) string {
	if !defaults.IsValid() || defaults.Kind() != reflect.Struct {
		return "—"
	}
	v := defaults.Field(idx)
	switch v.Kind() {
	case reflect.String:
		s := v.String()
		if s == "" {
			return "—"
		}
		return fmt.Sprintf("`%q`", s)
	case reflect.Bool:
		return fmt.Sprintf("`%t`", v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("`%d`", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("`%d`", v.Uint())
	case reflect.Float32, reflect.Float64:
		return fmt.Sprintf("`%g`", v.Float())
	default:
		return "—"
	}
}

// escapePipe escapes "|" characters so doc text never breaks the
// surrounding Markdown table row.
func escapePipe(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
