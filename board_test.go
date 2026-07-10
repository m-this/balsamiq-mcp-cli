package main

import (
	"math"
	"testing"
)

func box(left, top, w, h float64) map[string]any {
	return map[string]any{"left": left, "top": top, "w": w, "h": h}
}

func TestEdgePointHorizontal(t *testing.T) {
	// 100x50 box at (0,0), target straight to the right: exit at the right
	// border's middle, pushed by the gap.
	x, y := edgePoint(box(0, 0, 100, 50), 300, 25, 8)
	if x != 108 || y != 25 {
		t.Fatalf("got (%g, %g), want (108, 25)", x, y)
	}
}

func TestEdgePointVertical(t *testing.T) {
	// Target straight below: exit through the bottom border (y = 50) plus the gap.
	x, y := edgePoint(box(0, 0, 100, 50), 50, 500, 10)
	if x != 50 || y != 60 {
		t.Fatalf("got (%g, %g), want (50, 60)", x, y)
	}
}

func TestEdgePointDiagonalStaysOutsideBox(t *testing.T) {
	b := box(0, 0, 100, 50)
	x, y := edgePoint(b, 400, 300, 8)
	if x < 0 || x > 100+8 {
		t.Fatalf("x = %g escapes the border+gap band", x)
	}
	if y <= 50 {
		t.Fatalf("y = %g should exit below the box", y)
	}
}

func TestEdgePointDegenerateSameCenter(t *testing.T) {
	x, y := edgePoint(box(0, 0, 100, 50), 50, 25, 8)
	if x != 50 || y != 25 {
		t.Fatalf("got (%g, %g), want the center back", x, y)
	}
}

func TestResolveConnectEdgeToEdge(t *testing.T) {
	b := &board{Controls: []map[string]any{
		{"id": "a", "bbox": box(0, 0, 100, 50)},
		{"id": "b", "bbox": box(300, 0, 100, 50)},
	}}
	ctrl := map[string]any{"controlType": "arrow", "from": "a", "to": "b"}
	var missing []string
	if err := resolveConnect(b, ctrl, "a", 0, &missing); err != nil {
		t.Fatal(err)
	}
	if len(missing) > 0 {
		t.Fatalf("unexpected missing: %v", missing)
	}
	p0 := ctrl["p0"].(map[string]any)
	p2 := ctrl["p2"].(map[string]any)
	if p0["x"].(float64) != 108 || p2["x"].(float64) != 292 {
		t.Fatalf("endpoints not edge-to-edge: p0.x=%v p2.x=%v", p0["x"], p2["x"])
	}
	if num(ctrl["height"]) < 1 {
		t.Fatalf("height %v below server minimum of 1", ctrl["height"])
	}
	for _, gone := range []string{"from", "to", "gap"} {
		if _, ok := ctrl[gone]; ok {
			t.Fatalf("helper key %q leaked into the payload", gone)
		}
	}
	if ctrl["shape"] != "straight" {
		t.Fatalf("shape = %v, want straight default", ctrl["shape"])
	}
	mid := ctrl["p1"].(map[string]any)
	wantMid := (p0["x"].(float64) + p2["x"].(float64)) / 2
	if math.Abs(mid["x"].(float64)-wantMid) > 1e-9 {
		t.Fatalf("p1.x = %v, want midpoint %g", mid["x"], wantMid)
	}
}

func TestResolvePositionParent(t *testing.T) {
	b := &board{Controls: []map[string]any{
		{"id": "39", "bbox": box(1233, 0, 356, 1104)},
	}}
	ctrl := map[string]any{"controlType": "icon", "parent": "39", "rx": 20, "ry": 30, "width": 32, "height": 32}
	var missing []string
	if err := resolvePosition(b, ctrl, 0, &missing); err != nil {
		t.Fatal(err)
	}
	if num(ctrl["x"]) != 1253 || num(ctrl["y"]) != 30 {
		t.Fatalf("got (%v, %v), want (1253, 30)", ctrl["x"], ctrl["y"])
	}
	for _, gone := range []string{"parent", "rx", "ry"} {
		if _, ok := ctrl[gone]; ok {
			t.Fatalf("helper key %q leaked into the payload", gone)
		}
	}
	if _, ok := ctrl["zOrder"]; !ok {
		t.Fatal("zOrder default missing")
	}
}

func TestResolvePositionAfterAndParentConflict(t *testing.T) {
	b := &board{Controls: []map[string]any{{"id": "1", "bbox": box(0, 0, 10, 10)}}}
	ctrl := map[string]any{"after": "1", "parent": "1"}
	var missing []string
	if err := resolvePosition(b, ctrl, 0, &missing); err == nil {
		t.Fatal("after+parent should be rejected")
	}
}

func TestExpandFrameIphone(t *testing.T) {
	controls := expandFrame(map[string]any{"controlType": "iphone", "x": 100, "y": 200}, 0)
	if len(controls) != 3 {
		t.Fatalf("got %d controls, want 3", len(controls))
	}
	body := controls[0].(map[string]any)
	if num(body["width"]) != 380 || num(body["height"]) != 780 {
		t.Fatalf("default size not applied: %vx%v", body["width"], body["height"])
	}
	notch := controls[1].(map[string]any)
	wantNotchX := 100 + (380-0.32*380)/2
	if num(notch["x"]) != wantNotchX {
		t.Fatalf("notch not centered: x=%v want %g", notch["x"], wantNotchX)
	}
	for _, c := range controls {
		if errs := lintControl("frame", c.(map[string]any)); len(errs) != 0 {
			t.Fatalf("generated control fails lint: %v", errs)
		}
	}
}

func TestExpandFrameBrowser(t *testing.T) {
	controls := expandFrame(map[string]any{"controlType": "browser", "x": 0, "y": 0, "width": 800, "height": 600, "text": "https://stello.fr"}, 2)
	if len(controls) != 6 {
		t.Fatalf("got %d controls, want 6", len(controls))
	}
	var url map[string]any
	for _, c := range controls {
		if m := c.(map[string]any); m["controlType"] == "input" {
			url = m
		}
		if errs := lintControl("frame", c.(map[string]any)); len(errs) != 0 {
			t.Fatalf("generated control fails lint: %v", errs)
		}
	}
	if url == nil || url["text"] != "https://stello.fr" {
		t.Fatalf("url input missing or wrong text: %v", url)
	}
}

func TestLintEditMoves(t *testing.T) {
	errs := lintEdit(map[string]any{"moves": []any{
		map[string]any{"id": "12", "dx": 0, "dy": 100},
	}})
	if len(errs) != 0 {
		t.Fatalf("valid move rejected: %v", errs)
	}
	errs = lintEdit(map[string]any{"moves": []any{
		map[string]any{"id": "12"},
	}})
	if len(errs) == 0 {
		t.Fatal("move without dx/dy should be rejected")
	}
	errs = lintEdit(map[string]any{"moves": []any{
		map[string]any{"id": "12", "dx": 5, "recursive": false, "typo": true},
	}})
	if len(errs) == 0 {
		t.Fatal("unknown move key should be rejected")
	}
}

func TestDeletionEntry(t *testing.T) {
	if id, sub := deletionEntry("12"); id != "12" || sub {
		t.Fatalf("plain string must not sweep the subtree: got (%q, %v)", id, sub)
	}
	if id, sub := deletionEntry(map[string]any{"id": "12", "subtree": true}); id != "12" || !sub {
		t.Fatalf("subtree form not honored: got (%q, %v)", id, sub)
	}
	if id, _ := deletionEntry(42); id != "" {
		t.Fatalf("junk entry should resolve to empty id, got %q", id)
	}
}

func TestLintEditDeletions(t *testing.T) {
	errs := lintEdit(map[string]any{"deletions": []any{"12", map[string]any{"id": "13", "subtree": true}}})
	if len(errs) != 0 {
		t.Fatalf("valid deletions rejected: %v", errs)
	}
	errs = lintEdit(map[string]any{"deletions": []any{map[string]any{"subtree": true}}})
	if len(errs) == 0 {
		t.Fatal("deletion mapping without id should be rejected")
	}
}

func TestLintControlFromTo(t *testing.T) {
	errs := lintControl("additions[0]", map[string]any{
		"controlType": "arrow", "from": "a", "to": "b",
	})
	if len(errs) != 0 {
		t.Fatalf("from/to arrow should not require geometry: %v", errs)
	}
	errs = lintControl("additions[0]", map[string]any{
		"controlType": "rectangle", "from": "a", "to": "b",
		"x": 0, "y": 0, "width": 10, "height": 10,
	})
	if len(errs) == 0 {
		t.Fatal("from/to on a rectangle should be rejected")
	}
	errs = lintControl("additions[0]", map[string]any{
		"controlType": "arrow", "from": "a",
	})
	if len(errs) == 0 {
		t.Fatal("from without to should be rejected")
	}
}
