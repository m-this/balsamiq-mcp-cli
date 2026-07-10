package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var knownControlTypes = map[string]bool{
	"arrow": true, "auto": true, "button": true, "button-bar": true,
	"calendar": true, "chart": true, "checkbox": true, "combobox": true,
	"data-grid": true, "date-chooser": true, "date-picker": true,
	"horizontal-line": true, "horizontal-slider": true, "icon": true,
	"image": true, "input": true, "numeric-stepper": true,
	"progress-bar": true, "radioButton": true, "rectangle": true,
	"search-box": true, "shape": true, "sticky-note": true, "switch": true,
	"text": true, "time-picker": true, "vertical-line": true,
	"vertical-slider": true,
}

var (
	commonMark = regexp.MustCompile(`\*\*[^*]+\*\*|__[^_]+__|\[[^\]]+\]\([^)]+\)|^#{1,3} `)
	hexColor   = regexp.MustCompile(`^#([A-Fa-f0-9]{6}|[A-Fa-f0-9]{3})$`)
)

// lintEdit validates an edit payload offline, before any network call.
// Returned errors block the edit; warnings are printed to stderr.
func lintEdit(params map[string]any) []string {
	var errs []string
	for key := range params {
		switch key {
		case "boardUrl", "additions", "patches", "moves", "deletions":
		default:
			errs = append(errs, fmt.Sprintf("unknown top-level key %q (expected additions, patches, moves, deletions)", key))
		}
	}
	additions, _ := params["additions"].([]any)
	for i, a := range additions {
		ctrl, ok := a.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Sprintf("additions[%d]: not a mapping", i))
			continue
		}
		errs = append(errs, lintControl(fmt.Sprintf("additions[%d]", i), ctrl)...)
	}
	patches, _ := params["patches"].([]any)
	for i, p := range patches {
		patch, ok := p.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Sprintf("patches[%d]: not a mapping", i))
			continue
		}
		if id, _ := patch["id"].(string); id == "" {
			errs = append(errs, fmt.Sprintf("patches[%d]: missing string id", i))
		}
		props, ok := patch["props"].(map[string]any)
		if !ok || len(props) == 0 {
			errs = append(errs, fmt.Sprintf("patches[%d]: props must be a non-empty mapping", i))
			continue
		}
		lintStrings(fmt.Sprintf("patches[%d].props", i), props, &errs)
	}
	moves, _ := params["moves"].([]any)
	for i, mv := range moves {
		m, ok := mv.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Sprintf("moves[%d]: not a mapping", i))
			continue
		}
		if id, _ := m["id"].(string); id == "" {
			errs = append(errs, fmt.Sprintf("moves[%d]: missing string id", i))
		}
		if _, dx := m["dx"]; !dx {
			if _, dy := m["dy"]; !dy {
				errs = append(errs, fmt.Sprintf("moves[%d]: needs dx and/or dy", i))
			}
		}
		for key := range m {
			switch key {
			case "id", "dx", "dy", "recursive":
			default:
				errs = append(errs, fmt.Sprintf("moves[%d]: unknown key %q (expected id, dx, dy, recursive)", i, key))
			}
		}
	}
	deletions, _ := params["deletions"].([]any)
	for i, d := range deletions {
		switch t := d.(type) {
		case string:
		case map[string]any:
			if id, _ := t["id"].(string); id == "" {
				errs = append(errs, fmt.Sprintf("deletions[%d]: missing string id", i))
			}
			for key := range t {
				switch key {
				case "id", "subtree":
				default:
					errs = append(errs, fmt.Sprintf("deletions[%d]: unknown key %q (expected id, subtree)", i, key))
				}
			}
		default:
			errs = append(errs, fmt.Sprintf("deletions[%d]: must be an id string or {id, subtree}", i))
		}
	}
	if len(additions)+len(patches)+len(moves)+len(deletions) == 0 {
		errs = append(errs, "empty edit: provide at least one of additions, patches, moves, deletions")
	}
	return errs
}

func lintCreate(params map[string]any) []string {
	var errs []string
	for key := range params {
		switch key {
		case "projectUrl", "board", "insertAfterBoardUrl":
		default:
			errs = append(errs, fmt.Sprintf("unknown top-level key %q (expected board, insertAfterBoardUrl)", key))
		}
	}
	board, ok := params["board"].(map[string]any)
	if !ok {
		errs = append(errs, "missing board mapping")
		return errs
	}
	lintStrings("board", board, &errs)
	return errs
}

func lintControl(at string, ctrl map[string]any) []string {
	var errs []string
	ct, _ := ctrl["controlType"].(string)
	if ct == "" {
		errs = append(errs, at+": missing controlType")
	} else if !knownControlTypes[ct] && !frameControlTypes[ct] {
		errs = append(errs, fmt.Sprintf("%s: unknown controlType %q (see: bmc tools edit_balsamiq_board)", at, ct))
	}
	_, from := ctrl["from"]
	_, to := ctrl["to"]
	if from || to {
		if ct != "arrow" {
			errs = append(errs, fmt.Sprintf("%s: from/to only applies to controlType arrow", at))
		}
		if !from || !to {
			errs = append(errs, fmt.Sprintf("%s: from and to must be given together", at))
		}
	}
	_, after := ctrl["after"]
	_, parent := ctrl["parent"]
	if after && parent {
		errs = append(errs, fmt.Sprintf("%s: after and parent are mutually exclusive", at))
	}
	for _, req := range []string{"x", "y", "width", "height"} {
		if _, ok := ctrl[req]; !ok {
			if from && to {
				continue
			}
			if frameControlTypes[ct] && (req == "width" || req == "height") {
				continue
			}
			if (after || parent) && (req == "x" || req == "y") {
				continue
			}
			errs = append(errs, fmt.Sprintf("%s: missing %s", at, req))
		}
	}
	lintStrings(at, ctrl, &errs)
	return errs
}

// lintStrings walks values looking for CommonMark markup in text and broken
// color values; markup issues are warnings, bad colors are errors.
func lintStrings(at string, m map[string]any, errs *[]string) {
	for k, v := range m {
		switch t := v.(type) {
		case string:
			if strings.Contains(strings.ToLower(k), "color") && !hexColor.MatchString(t) {
				*errs = append(*errs, fmt.Sprintf("%s.%s: %q is not a HEX color (unresolved $token?)", at, k, t))
			}
			if k == "text" && commonMark.MatchString(t) {
				warn("%s.%s: looks like CommonMark; Balsamiq uses its own markup (e.g. no **bold**)", at, k)
			}
		case map[string]any:
			lintStrings(at+"."+k, t, errs)
		case []any:
			for i, item := range t {
				if sub, ok := item.(map[string]any); ok {
					lintStrings(fmt.Sprintf("%s.%s[%d]", at, k, i), sub, errs)
				}
			}
		}
	}
}

func warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: "+format+"\n", args...)
}

func lintErrors(errs []string) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("payload rejected by offline lint:\n  - %s", strings.Join(errs, "\n  - "))
}
