package main

import (
	"fmt"
	"regexp"
	"strings"
)

// PartialFunc is the signature a partial implementation must satisfy.
// args is the parsed key/value map from the partial invocation;
// returned text is spliced in verbatim (as Markdown or raw HTML).
type PartialFunc func(args map[string]string) (string, error)

// partialRegistry holds every partial by name. New partials register
// themselves in init() in the file that implements them.
var partialRegistry = map[string]PartialFunc{}

// registerPartial adds a partial to the registry. Names must be
// unique; duplicate registration panics during binary init, which
// surfaces the mistake immediately on `go build`.
func registerPartial(name string, fn PartialFunc) {
	if _, ok := partialRegistry[name]; ok {
		panic("duplicate partial registration: " + name)
	}
	partialRegistry[name] = fn
}

// partialRE matches `{{> name key1=value1 key2="value with spaces"}}`
// on a single line.
var partialRE = regexp.MustCompile(`{{>\s*([A-Za-z][\w-]*)([^}]*)}}`)

// argRE matches one key=value or key="value" pair inside a partial invocation.
var argRE = regexp.MustCompile(`([A-Za-z][\w-]*)=("([^"]*)"|(\S+))`)

// expandPartials substitutes every {{> ...}} invocation in body with
// the registered partial's output. Unknown partial names produce an
// error.
func expandPartials(body string) (string, error) {
	var firstErr error
	out := partialRE.ReplaceAllStringFunc(body, func(match string) string {
		if firstErr != nil {
			return match
		}
		parts := partialRE.FindStringSubmatch(match)
		name, rawArgs := parts[1], parts[2]
		fn, ok := partialRegistry[name]
		if !ok {
			firstErr = fmt.Errorf("unknown partial %q", name)
			return match
		}
		args := parseArgs(rawArgs)
		got, err := fn(args)
		if err != nil {
			firstErr = fmt.Errorf("partial %q: %w", name, err)
			return match
		}
		return got
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

// parseArgs extracts key=value pairs from a partial invocation tail,
// supporting both bare and double-quoted values.
func parseArgs(raw string) map[string]string {
	out := map[string]string{}
	for _, m := range argRE.FindAllStringSubmatch(raw, -1) {
		k := m[1]
		v := m[3]
		if v == "" {
			v = m[4]
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}
