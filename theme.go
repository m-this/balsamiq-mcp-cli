package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// A theme file (.bais.yaml at or above the working directory, or $BAIS_THEME)
// defines color tokens and parametrized partials so payloads stay small and
// consistent with the project's validated style.
//
//	colors:
//	  primary: "#009e0f"
//	partials:
//	  pill:
//	    params: {text: PILL, color: $primary}
//	    body:
//	      controlType: rectangle
//	      ...
//	      text: ${text}
//
// Payloads invoke a partial with {use: pill, with: {text: AJOUTÉE}, x: 10}:
// `with` fills ${params}, any sibling key overrides the expanded body's keys.
// A body may be a list of controls; it is spliced into the surrounding array.

type partial struct {
	Params map[string]any `yaml:"params"`
	Body   any            `yaml:"body"`
}

type theme struct {
	Colors   map[string]string  `yaml:"colors"`
	Partials map[string]partial `yaml:"partials"`
}

func loadTheme() (*theme, error) {
	path := os.Getenv("BAIS_THEME")
	if path == "" {
		dir, _ := os.Getwd()
		for dir != "/" && dir != "" {
			candidate := filepath.Join(dir, ".bais.yaml")
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			}
			dir = filepath.Dir(dir)
		}
	}
	if path == "" {
		return &theme{}, nil
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("theme %s: %w", path, err)
	}
	var th theme
	if err := yaml.Unmarshal(buf, &th); err != nil {
		return nil, fmt.Errorf("theme %s: %w", path, err)
	}
	return &th, nil
}

var paramRef = regexp.MustCompile(`\$\{([a-zA-Z0-9_-]+)\}`)

func (th *theme) expand(v any) (any, error) {
	return th.expandDepth(normalizeYAML(v), 0)
}

func (th *theme) expandDepth(v any, depth int) (any, error) {
	if depth > 10 {
		return nil, errors.New("partial expansion too deep (cycle?)")
	}
	switch t := v.(type) {
	case map[string]any:
		if name, ok := t["use"].(string); ok {
			return th.expandPartial(name, t, depth)
		}
		out := make(map[string]any, len(t))
		for k, val := range t {
			expanded, err := th.expandDepth(val, depth)
			if err != nil {
				return nil, err
			}
			_, wasList := val.([]any)
			if _, isList := expanded.([]any); isList && !wasList {
				return nil, fmt.Errorf("partial under key %q expands to a list; list-bodied partials can only be spliced into arrays", k)
			}
			out[k] = expanded
		}
		return out, nil
	case []any:
		out := make([]any, 0, len(t))
		for _, item := range t {
			expanded, err := th.expandDepth(item, depth)
			if err != nil {
				return nil, err
			}
			if list, ok := expanded.([]any); ok {
				if _, wasList := item.([]any); !wasList {
					out = append(out, list...)
					continue
				}
			}
			out = append(out, expanded)
		}
		return out, nil
	case string:
		return th.resolveString(t)
	default:
		return v, nil
	}
}

func (th *theme) expandPartial(name string, call map[string]any, depth int) (any, error) {
	p, ok := th.Partials[name]
	if !ok {
		available := make([]string, 0, len(th.Partials))
		for k := range th.Partials {
			available = append(available, k)
		}
		return nil, fmt.Errorf("unknown partial %q (theme defines: %s)", name, strings.Join(available, ", "))
	}
	params := map[string]any{}
	for k, v := range p.Params {
		params[k] = v
	}
	if with, ok := call["with"].(map[string]any); ok {
		for k, v := range with {
			if _, known := p.Params[k]; !known {
				return nil, fmt.Errorf("partial %q has no param %q", name, k)
			}
			params[k] = v
		}
	}
	body, err := substitute(deepCopy(normalizeYAML(p.Body)), params, name)
	if err != nil {
		return nil, err
	}
	if overridden, ok := body.(map[string]any); ok {
		for k, v := range call {
			if k != "use" && k != "with" {
				overridden[k] = v
			}
		}
	}
	return th.expandDepth(body, depth+1)
}

func substitute(v any, params map[string]any, name string) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			sub, err := substitute(val, params, name)
			if err != nil {
				return nil, err
			}
			t[k] = sub
		}
		return t, nil
	case []any:
		for i, val := range t {
			sub, err := substitute(val, params, name)
			if err != nil {
				return nil, err
			}
			t[i] = sub
		}
		return t, nil
	case string:
		if m := paramRef.FindStringSubmatch(t); m != nil && m[0] == t {
			val, ok := params[m[1]]
			if !ok {
				return nil, fmt.Errorf("partial %q: unbound param ${%s}", name, m[1])
			}
			return val, nil
		}
		var err error
		out := paramRef.ReplaceAllStringFunc(t, func(ref string) string {
			key := paramRef.FindStringSubmatch(ref)[1]
			val, ok := params[key]
			if !ok {
				err = fmt.Errorf("partial %q: unbound param ${%s}", name, key)
				return ref
			}
			return fmt.Sprint(val)
		})
		return out, err
	default:
		return v, nil
	}
}

func (th *theme) resolveString(s string) (string, error) {
	if strings.HasPrefix(s, "$") && !strings.HasPrefix(s, "${") {
		name := s[1:]
		if color, ok := th.Colors[name]; ok {
			return color, nil
		}
		if len(th.Colors) > 0 {
			return s, fmt.Errorf("unknown color token %q (theme defines: %s)", s, strings.Join(colorNames(th.Colors), ", "))
		}
	}
	return s, nil
}

func colorNames(colors map[string]string) []string {
	out := make([]string, 0, len(colors))
	for k := range colors {
		out = append(out, "$"+k)
	}
	return out
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = deepCopy(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = deepCopy(val)
		}
		return out
	default:
		return v
	}
}
