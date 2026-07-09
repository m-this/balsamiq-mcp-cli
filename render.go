package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// printYAML renders v as YAML after pruning nulls and empty containers,
// which is where most of the token savings over raw MCP JSON come from.
func printYAML(v any) error {
	v = prune(v)
	if v == nil {
		fmt.Println("(empty)")
		return nil
	}
	enc := yaml.NewEncoder(os.Stdout)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(v)
}

func prune(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if p := prune(val); p != nil {
				out[k] = p
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, val := range t {
			if p := prune(val); p != nil {
				out = append(out, p)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return t
	case nil:
		return nil
	default:
		return v
	}
}

// toolResult unwraps an MCP tools/call result: prefers structuredContent,
// then parses each text content block as JSON when possible.
func toolResult(raw json.RawMessage) (any, error) {
	var res struct {
		IsError           bool `json:"isError"`
		StructuredContent any  `json:"structuredContent"`
		Content           []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	if res.StructuredContent != nil && !res.IsError {
		return res.StructuredContent, nil
	}
	var parts []any
	for _, c := range res.Content {
		if c.Type != "text" {
			parts = append(parts, fmt.Sprintf("(%s content omitted)", c.Type))
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(c.Text), &v); err == nil {
			parts = append(parts, v)
		} else {
			parts = append(parts, c.Text)
		}
	}
	var out any
	switch len(parts) {
	case 0:
		out = nil
	case 1:
		out = parts[0]
	default:
		out = parts
	}
	if res.IsError {
		return out, fmt.Errorf("tool returned an error")
	}
	return out, nil
}

var pathSegment = regexp.MustCompile(`([^.\[\]]+)|\[(\d+)\]`)

// extractPath walks v following a jq-like path: a.b[0].c
func extractPath(v any, path string) (any, error) {
	for _, m := range pathSegment.FindAllStringSubmatch(path, -1) {
		if m[1] != "" {
			obj, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("path %q: %q is not an object", path, m[1])
			}
			v, ok = obj[m[1]]
			if !ok {
				keys := make([]string, 0, len(obj))
				for k := range obj {
					keys = append(keys, k)
				}
				return nil, fmt.Errorf("path %q: key %q not found (available: %s)", path, m[1], strings.Join(keys, ", "))
			}
		} else {
			arr, ok := v.([]any)
			if !ok {
				return nil, fmt.Errorf("path %q: not an array at [%s]", path, m[2])
			}
			i, _ := strconv.Atoi(m[2])
			if i >= len(arr) {
				return nil, fmt.Errorf("path %q: index %d out of range (len %d)", path, i, len(arr))
			}
			v = arr[i]
		}
	}
	return v, nil
}
