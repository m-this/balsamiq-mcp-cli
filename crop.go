package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"

	_ "golang.org/x/image/webp"
)

// cropToNode cuts the board preview (WebP or PNG) down to one control's
// bounding box and returns it as PNG. The preview has no documented coordinate
// mapping, so the content bounding box of the cached board is mapped linearly
// onto the image; a generous padding absorbs the approximation.
func cropToNode(boardURL, id string, imgBytes []byte) ([]byte, error) {
	b, err := loadBoard(boardURL, false)
	if err != nil {
		return nil, err
	}
	ctrl := findControl(b.Controls, id)
	if ctrl == nil {
		return nil, fmt.Errorf("control %q not found on the board", id)
	}
	bbox, ok := ctrl["bbox"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("control %q has no bounding box", id)
	}

	var minX, minY, maxX, maxY float64
	first := true
	for _, root := range b.Controls {
		rb, ok := root["bbox"].(map[string]any)
		if !ok {
			continue
		}
		left, top := num(rb["left"]), num(rb["top"])
		right, bottom := left+num(rb["w"]), top+num(rb["h"])
		if first || left < minX {
			minX = left
		}
		if first || top < minY {
			minY = top
		}
		if first || right > maxX {
			maxX = right
		}
		if first || bottom > maxY {
			maxY = bottom
		}
		first = false
	}
	if first || maxX <= minX || maxY <= minY {
		return nil, fmt.Errorf("cannot compute board bounds from cache")
	}

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	scaleX := float64(bounds.Dx()) / (maxX - minX)
	scaleY := float64(bounds.Dy()) / (maxY - minY)

	const pad = 40.0
	left := (num(bbox["left"])-minX)*scaleX - pad
	top := (num(bbox["top"])-minY)*scaleY - pad
	right := (num(bbox["left"])+num(bbox["w"])-minX)*scaleX + pad
	bottom := (num(bbox["top"])+num(bbox["h"])-minY)*scaleY + pad
	rect := image.Rect(int(left), int(top), int(right), int(bottom)).Intersect(bounds)
	if rect.Empty() {
		return nil, fmt.Errorf("control %q maps outside the preview image", id)
	}

	out := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(out, out.Bounds(), img, rect.Min, draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
