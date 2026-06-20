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

// partialNameRE captures the partial name immediately after `{{>`.
var partialNameRE = regexp.MustCompile(`^{{>\s*([A-Za-z][\w-]*)`)

// argRE matches one key=value or key="value" pair inside a partial invocation.
var argRE = regexp.MustCompile(`([A-Za-z][\w-]*)=("([^"]*)"|(\S+))`)

// expandPartials substitutes every {{> ...}} invocation in body with
// the registered partial's output. The scan is brace-aware so a
// nested `{{name}}` inside an argument value (e.g.
// `legend="{{mode}}"`) does not prematurely close the invocation;
// the matching `}}` is the one that brings the depth back to zero.
// Unknown partial names produce an error.
func expandPartials(body string) (string, error) {
	var out strings.Builder
	i := 0
	for {
		start := strings.Index(body[i:], "{{>")
		if start < 0 {
			out.WriteString(body[i:])
			return out.String(), nil
		}
		out.WriteString(body[i : i+start])
		absStart := i + start
		end := findMatchingClose(body, absStart)
		if end < 0 {
			// Unbalanced — pass the rest through verbatim and stop
			// looking for more partials.
			out.WriteString(body[absStart:])
			return out.String(), nil
		}
		match := body[absStart : end+2]
		nameParts := partialNameRE.FindStringSubmatch(match)
		if nameParts == nil {
			out.WriteString(match)
			i = end + 2
			continue
		}
		name := nameParts[1]
		fn, ok := partialRegistry[name]
		if !ok {
			return "", fmt.Errorf("unknown partial %q", name)
		}
		// rawArgs is everything between the name and the closing }}.
		rawArgs := match[len(nameParts[0]) : len(match)-2]
		got, err := fn(parseArgs(rawArgs))
		if err != nil {
			return "", fmt.Errorf("partial %q: %w", name, err)
		}
		out.WriteString(got)
		i = end + 2
	}
}

// findMatchingClose returns the byte offset of the `}}` that closes
// the `{{>` at start. Returns -1 if no balanced close exists. Nested
// `{{...}}` pairs (e.g. inside quoted argument values) are skipped:
// every `{{` increments depth, every `}}` decrements; the close is
// the one that brings depth back to zero.
func findMatchingClose(s string, start int) int {
	depth := 1
	j := start + 3 // skip past `{{>`
	for j < len(s)-1 {
		switch {
		case s[j] == '{' && s[j+1] == '{':
			depth++
			j += 2
		case s[j] == '}' && s[j+1] == '}':
			depth--
			if depth == 0 {
				return j
			}
			j += 2
		default:
			j++
		}
	}
	return -1
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
