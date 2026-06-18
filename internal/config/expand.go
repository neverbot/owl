package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// expandString resolves the secret reference syntax inside one string
// scalar coming from the config YAML.
//
// Supported forms:
//
//   - ${VAR}             — environment variable; missing → error
//   - ${VAR:-default}    — environment variable with literal fallback
//   - file:/abs/path     — file contents, trailing whitespace trimmed
//   - $${anything}       — literal ${anything} (escape)
//
// env and read are injected so the function is unit-testable without
// touching the real process environment or filesystem. The defaults
// used by Load() are os.LookupEnv and os.ReadFile.
func expandString(s string, env func(string) (string, bool), read func(string) ([]byte, error)) (string, error) {
	if strings.HasPrefix(s, "file:") {
		path := strings.TrimPrefix(s, "file:")
		if !strings.HasPrefix(path, "/") {
			return "", fmt.Errorf("file: reference must be absolute, got %q", path)
		}
		data, err := read(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
		return strings.TrimRight(string(data), " \t\r\n"), nil
	}
	return expandEnv(s, env)
}

// expandEnv walks s replacing ${VAR}, ${VAR:-default} and $$ escapes.
// Anything else is left literal. Errors when a referenced variable is
// not set and no default is provided.
func expandEnv(s string, env func(string) (string, bool)) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		// $$  → literal $
		if i+1 < len(s) && s[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		// ${...}
		if i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("unterminated ${ in %q", s)
			}
			name := s[i+2 : i+2+end]
			value, err := lookup(name, env)
			if err != nil {
				return "", err
			}
			b.WriteString(value)
			i += 2 + end + 1
			continue
		}
		// Bare $ — keep literal.
		b.WriteByte('$')
		i++
	}
	return b.String(), nil
}

func lookup(spec string, env func(string) (string, bool)) (string, error) {
	if idx := strings.Index(spec, ":-"); idx >= 0 {
		name := spec[:idx]
		def := spec[idx+2:]
		if v, ok := env(name); ok {
			return v, nil
		}
		return def, nil
	}
	if v, ok := env(spec); ok {
		return v, nil
	}
	return "", fmt.Errorf("config: %s not set", spec)
}

// expandNode walks a yaml.Node tree expanding every string scalar in
// place. Mapping/sequence/alias/document nodes are recursed into.
// Returns the first error encountered (with an accurate YAML line).
func expandNode(n *yaml.Node, env func(string) (string, bool), read func(string) ([]byte, error)) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode && (n.Tag == "" || n.Tag == "!!str") {
		out, err := expandString(n.Value, env, read)
		if err != nil {
			return fmt.Errorf("line %d: %w", n.Line, err)
		}
		n.Value = out
		return nil
	}
	for _, c := range n.Content {
		if err := expandNode(c, env, read); err != nil {
			return err
		}
	}
	return nil
}

// defaultEnv is the os-backed lookup used in production. Wrapped
// because expandString takes the function form to keep tests pure.
func defaultEnv() func(string) (string, bool) {
	return os.LookupEnv
}

// defaultRead is the os-backed file reader used in production.
func defaultRead() func(string) ([]byte, error) {
	return os.ReadFile
}
