package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type toolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func listTools(c *mcpClient) ([]toolInfo, error) {
	raw, err := c.call("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var res struct {
		Tools []toolInfo `json:"tools"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	sort.Slice(res.Tools, func(i, j int) bool { return res.Tools[i].Name < res.Tools[j].Name })
	return res.Tools, nil
}

func cmdTools(args []string) error {
	c, err := dial()
	if err != nil {
		return err
	}
	tools, err := listTools(c)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		for _, t := range tools {
			fmt.Printf("%-32s %s\n", t.Name, firstSentence(t.Description))
		}
		return nil
	}
	for _, t := range tools {
		if t.Name == args[0] {
			return printYAML(map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"input":       t.InputSchema,
			})
		}
	}
	return fmt.Errorf("unknown tool %q", args[0])
}

func cmdCall(args []string) error {
	var (
		raw      bool
		path     string
		toolName string
	)
	params := map[string]any{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--raw":
			raw = true
		case a == "--path":
			i++
			if i >= len(args) {
				return fmt.Errorf("--path needs a value")
			}
			path = args[i]
		case a == "-f":
			i++
			if i >= len(args) {
				return fmt.Errorf("-f needs a file")
			}
			if err := mergeFile(params, args[i]); err != nil {
				return err
			}
		case strings.Contains(a, ":="):
			k, v, _ := strings.Cut(a, ":=")
			var parsed any
			if err := json.Unmarshal([]byte(v), &parsed); err != nil {
				return fmt.Errorf("arg %q: invalid JSON: %w", k, err)
			}
			params[k] = parsed
		case strings.Contains(a, "="):
			k, v, _ := strings.Cut(a, "=")
			params[k] = v
		case toolName == "":
			toolName = a
		default:
			return fmt.Errorf("unexpected argument %q", a)
		}
	}
	if toolName == "" {
		return fmt.Errorf("usage: bais call <tool> [key=value] [key:=json] [-f file] [--raw] [--path p]")
	}

	c, err := dial()
	if err != nil {
		return err
	}
	rawRes, err := c.call("tools/call", map[string]any{
		"name":      toolName,
		"arguments": params,
	})
	if err != nil {
		return err
	}
	result, terr := toolResult(rawRes)
	if raw {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
		return terr
	}
	if path != "" {
		if result, err = extractPath(result, path); err != nil {
			return err
		}
	}
	if perr := printYAML(result); perr != nil {
		return perr
	}
	return terr
}

func mergeFile(params map[string]any, file string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var v map[string]any
	if err := yaml.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	th, err := loadTheme()
	if err != nil {
		return err
	}
	expanded, err := th.expand(v)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	m, ok := expanded.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: top level must be a mapping", file)
	}
	for k, val := range m {
		params[k] = val
	}
	return nil
}

// normalizeYAML converts map[any]any (yaml.v3 edge cases) into map[string]any
// so params marshal cleanly to JSON.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeYAML(val)
		}
		return t
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprint(k)] = normalizeYAML(val)
		}
		return out
	case []any:
		for i, val := range t {
			t[i] = normalizeYAML(val)
		}
		return t
	default:
		return v
	}
}

// cmdExpand prints a payload after theme/partial expansion and lint, without
// sending anything - the dry-run for building patch and board files.
func cmdExpand(args []string) error {
	var file string
	for i := 0; i < len(args); i++ {
		if args[i] == "-f" && i+1 < len(args) {
			i++
			file = args[i]
		} else {
			file = args[i]
		}
	}
	if file == "" {
		return fmt.Errorf("usage: bais expand -f payload.yaml")
	}
	params := map[string]any{}
	if err := mergeFile(params, file); err != nil {
		return err
	}
	var lintErrs []string
	if _, isEdit := params["additions"]; isEdit || params["patches"] != nil || params["deletions"] != nil {
		lintErrs = lintEdit(params)
	} else if params["board"] != nil {
		lintErrs = lintCreate(params)
	}
	for _, e := range lintErrs {
		warn("%s", e)
	}
	return printYAML(params)
}

func firstSentence(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if i := strings.Index(s, ". "); i > 0 && i < 120 {
		return s[:i+1]
	}
	if len(s) > 120 {
		return s[:117] + "..."
	}
	return s
}
