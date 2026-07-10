package main

// Pseudo-controlTypes expanded client-side: the BAIS additions schema has no
// device frames, so `iphone` and `browser` splice into the plain rectangles,
// shapes, and inputs the reference boards emulate them with. Geometry needs
// arithmetic (centered notch, bar layout), which theme partials cannot do.

var frameControlTypes = map[string]bool{"iphone": true, "browser": true}

var frameDefaults = map[string][2]float64{
	"iphone":  {380, 780},
	"browser": {1024, 768},
}

func isFrame(ctrl map[string]any) bool {
	ct, _ := ctrl["controlType"].(string)
	return frameControlTypes[ct]
}

func expandFrame(ctrl map[string]any, i int) []any {
	ct, _ := ctrl["controlType"].(string)
	x, y := num(ctrl["x"]), num(ctrl["y"])
	w, h := num(ctrl["width"]), num(ctrl["height"])
	if w == 0 {
		w = frameDefaults[ct][0]
	}
	if h == 0 {
		h = frameDefaults[ct][1]
	}
	z := float64(100 + i)
	if v, ok := ctrl["zOrder"]; ok {
		z = num(v)
	}
	if ct == "iphone" {
		return iphoneFrame(x, y, w, h, z)
	}
	url, _ := ctrl["text"].(string)
	if url == "" {
		url = "https://"
	}
	return browserFrame(x, y, w, h, z, url)
}

func iphoneFrame(x, y, w, h, z float64) []any {
	notchW := 0.32 * w
	homeW := 0.35 * w
	return []any{
		map[string]any{
			"controlType": "rectangle", "borderStyle": "roundedSolid",
			"borderColor": "#333333",
			"x":           x, "y": y, "width": w, "height": h, "zOrder": z,
		},
		map[string]any{
			"controlType": "rectangle", "borderStyle": "roundedSolid",
			"backgroundColor": "#333333",
			"x":               x + (w-notchW)/2, "y": y + 10, "width": notchW, "height": 24, "zOrder": z + 1,
		},
		map[string]any{
			"controlType": "rectangle", "borderStyle": "roundedSolid",
			"backgroundColor": "#333333",
			"x":               x + (w-homeW)/2, "y": y + h - 20, "width": homeW, "height": 10, "zOrder": z + 1,
		},
	}
}

func browserFrame(x, y, w, h, z float64, url string) []any {
	const barH = 40.0
	controls := []any{
		map[string]any{
			"controlType": "rectangle", "borderColor": "#333333",
			"x": x, "y": y, "width": w, "height": h, "zOrder": z,
		},
		map[string]any{
			"controlType": "horizontal-line", "color": "#333333",
			"x": x, "y": y + barH - 5, "width": w, "height": 10, "zOrder": z + 1,
		},
		map[string]any{
			"controlType": "input", "text": url,
			"x": x + 68, "y": y + 7, "width": w - 80, "height": 26, "zOrder": z + 1,
		},
	}
	for i := range 3 {
		controls = append(controls, map[string]any{
			"controlType": "shape", "shape": "circle", "borderColor": "#333333",
			"x": x + 12 + float64(i)*18, "y": y + 14, "width": 12, "height": 12, "zOrder": z + 1,
		})
	}
	return controls
}
