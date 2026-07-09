package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// board content cache: one JSON file per board, so follow-up reads
// (map, show, edit context) never re-fetch nor re-print the whole tree.

func cacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".cache")
	}
	return filepath.Join(dir, "bais")
}

func boardCachePath(boardURL string) string {
	slug := strings.Trim(strings.NewReplacer("https://", "", "http://", "", "/", "_").Replace(boardURL), "_")
	return filepath.Join(cacheDir(), slug+".json")
}

type board struct {
	Name     string           `json:"name"`
	Controls []map[string]any `json:"content"`
}

func fetchBoard(boardURL string) (*board, error) {
	c, err := dial()
	if err != nil {
		return nil, err
	}
	var b board
	cursor := ""
	for {
		params := map[string]any{"boardUrl": boardURL}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := c.call("tools/call", map[string]any{
			"name": "get_balsamiq_board_content", "arguments": params,
		})
		if err != nil {
			return nil, err
		}
		result, err := toolResult(raw)
		if err != nil {
			return nil, fmt.Errorf("get_balsamiq_board_content: %w (%v)", err, result)
		}
		var page struct {
			Name       string           `json:"name"`
			Controls   []map[string]any `json:"content"`
			NextCursor string           `json:"nextCursor"`
		}
		buf, _ := json.Marshal(result)
		if err := json.Unmarshal(buf, &page); err != nil {
			return nil, err
		}
		if page.Name != "" {
			b.Name = page.Name
		}
		b.Controls = append(b.Controls, page.Controls...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if err := os.MkdirAll(cacheDir(), 0o755); err == nil {
		buf, _ := json.Marshal(b)
		_ = os.WriteFile(boardCachePath(boardURL), buf, 0o644)
	}
	return &b, nil
}

func loadBoard(boardURL string, refresh bool) (*board, error) {
	if !refresh {
		if buf, err := os.ReadFile(boardCachePath(boardURL)); err == nil {
			var b board
			if json.Unmarshal(buf, &b) == nil {
				return &b, nil
			}
		}
	}
	return fetchBoard(boardURL)
}

func invalidateBoard(boardURL string) {
	_ = os.Remove(boardCachePath(boardURL))
}

// cmdProjects prints one line per project.
func cmdProjects() error {
	result, err := callTool("list_balsamiq_projects", map[string]any{})
	if err != nil {
		return err
	}
	return printYAML(result)
}

func cmdTOC(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: bmc toc <projectUrl>")
	}
	result, err := callTool("get_balsamiq_project_toc", map[string]any{"projectUrl": args[0]})
	if err != nil {
		return err
	}
	buf, _ := json.Marshal(result)
	var toc struct {
		ProjectName string `json:"projectName"`
		Boards      []struct {
			Name  string `json:"name"`
			URL   string `json:"url"`
			Level int    `json:"level"`
		} `json:"boards"`
	}
	if err := json.Unmarshal(buf, &toc); err != nil {
		return printYAML(result)
	}
	fmt.Println(toc.ProjectName)
	for _, b := range toc.Boards {
		fmt.Printf("%s%s  %s\n", strings.Repeat("  ", b.Level), b.URL, b.Name)
	}
	return nil
}

// cmdBoard prints the compact control map: one line per control with id,
// type, and text. Geometry is opt-in (--geo) or implied by a filter, since
// filtered lines are usually about to be patched.
func cmdBoard(args []string) error {
	var boardURL, find, typ string
	refresh, full, geo := false, false, false
	depth := -1
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--refresh":
			refresh = true
		case "--full":
			full = true
		case "--geo":
			geo = true
		case "--depth":
			i++
			if i >= len(args) {
				return errors.New("--depth needs a number")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return fmt.Errorf("--depth: %w", err)
			}
			depth = n
		case "--find":
			i++
			if i >= len(args) {
				return errors.New("--find needs a pattern")
			}
			find = strings.ToLower(args[i])
		case "--type":
			i++
			if i >= len(args) {
				return errors.New("--type needs a control type")
			}
			typ = normalizeType(args[i])
		default:
			boardURL = args[i]
		}
	}
	if boardURL == "" {
		return errors.New("usage: bmc board <boardUrl> [--refresh] [--full] [--geo] [--depth n] [--find text] [--type button]")
	}
	b, err := loadBoard(boardURL, refresh)
	if err != nil {
		return err
	}
	if full {
		return printYAML(map[string]any{"name": b.Name, "content": anySlice(b.Controls)})
	}
	opts := mapOpts{geo: geo || find != "" || typ != "", depth: depth, find: find, typ: typ}
	fmt.Printf("board: %s\n", b.Name)
	for _, ctrl := range b.Controls {
		printControlLine(ctrl, 0, opts)
	}
	return nil
}

type mapOpts struct {
	geo   bool
	depth int
	find  string
	typ   string
}

func (o mapOpts) filtered() bool { return o.find != "" || o.typ != "" }

func (o mapOpts) matches(ctrl map[string]any) bool {
	if !o.filtered() {
		return true
	}
	id, _ := ctrl["id"].(string)
	typ, _ := ctrl["type"].(string)
	text, _ := ctrl["text"].(string)
	if o.typ != "" && !strings.Contains(normalizeType(typ), o.typ) {
		return false
	}
	if o.find != "" && !strings.Contains(strings.ToLower(text), o.find) && id != o.find {
		return false
	}
	return true
}

func normalizeType(t string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(t))
}

func anySlice(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func printControlLine(ctrl map[string]any, depth int, opts mapOpts) {
	if opts.depth >= 0 && depth > opts.depth {
		return
	}
	if opts.matches(ctrl) {
		id, _ := ctrl["id"].(string)
		if id == "" {
			id = "-"
		}
		typ, _ := ctrl["type"].(string)
		indent := strings.Repeat("  ", depth)
		if opts.filtered() {
			indent = ""
		}
		line := indent + id + " " + typ
		if opts.geo {
			if bbox, ok := ctrl["bbox"].(map[string]any); ok {
				line += fmt.Sprintf(" %v,%v %vx%v", bbox["left"], bbox["top"], bbox["w"], bbox["h"])
			}
		}
		if text, ok := ctrl["text"].(string); ok && text != "" {
			if len(text) > 60 {
				text = text[:57] + "..."
			}
			line += " " + fmt.Sprintf("%q", text)
		}
		fmt.Println(line)
	}
	if children, ok := ctrl["children"].([]any); ok {
		for _, child := range children {
			if m, ok := child.(map[string]any); ok {
				printControlLine(m, depth+1, opts)
			}
		}
	}
}

// cmdShow prints the full properties of one control from the cached board,
// with children collapsed to their ids.
func cmdShow(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: bmc show <boardUrl> <controlId>")
	}
	b, err := loadBoard(args[0], false)
	if err != nil {
		return err
	}
	ctrl := findControl(b.Controls, args[1])
	if ctrl == nil {
		return fmt.Errorf("control %q not found (run: bmc board %s --refresh)", args[1], args[0])
	}
	out := make(map[string]any, len(ctrl))
	maps.Copy(out, ctrl)
	if children, ok := ctrl["children"].([]any); ok {
		ids := make([]any, 0, len(children))
		for _, child := range children {
			if m, ok := child.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					ids = append(ids, id)
				} else {
					ids = append(ids, fmt.Sprintf("(%v)", m["type"]))
				}
			}
		}
		out["children"] = ids
	}
	return printYAML(out)
}

func findControl(controls []map[string]any, id string) map[string]any {
	for _, ctrl := range controls {
		if cid, _ := ctrl["id"].(string); cid == id {
			return ctrl
		}
		if children, ok := ctrl["children"].([]any); ok {
			var sub []map[string]any
			for _, child := range children {
				if m, ok := child.(map[string]any); ok {
					sub = append(sub, m)
				}
			}
			if found := findControl(sub, id); found != nil {
				return found
			}
		}
	}
	return nil
}

// cmdEdit applies a YAML patch file (additions/patches/deletions) atomically.
func cmdEdit(args []string) error {
	var boardURL, file string
	preview := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-f" && i+1 < len(args):
			i++
			file = args[i]
		case args[i] == "--preview":
			preview = true
		default:
			boardURL = args[i]
		}
	}
	if boardURL == "" || file == "" {
		return errors.New("usage: bmc edit <boardUrl> -f patch.yaml [--preview] (keys: additions, patches, deletions)")
	}
	params := map[string]any{}
	if err := mergeFile(params, file); err != nil {
		return err
	}
	params["boardUrl"] = boardURL
	if err := lintErrors(lintEdit(params)); err != nil {
		return err
	}
	if err := prepareEdit(boardURL, params); err != nil {
		return err
	}
	result, err := callTool("edit_balsamiq_board", params)
	invalidateBoard(boardURL)
	if err != nil {
		if result != nil {
			_ = printYAML(result)
		}
		return err
	}
	if err := printYAML(result); err != nil {
		return err
	}
	if preview {
		return writePreview(boardURL, "", "")
	}
	return nil
}

// prepareEdit re-fetches the board right before an edit, so the cache reflects
// any manual changes made in the editor meanwhile, then:
//   - rejects the edit early if a targeted control no longer exists
//   - resolves relative additions (after: siblingId [+dx/dy]) to absolute x/y
//   - expands each deletion to the control's whole subtree, since the server
//     otherwise leaves spatially-nested children behind as ghosts
func prepareEdit(boardURL string, params map[string]any) error {
	b, err := fetchBoard(boardURL)
	if err != nil {
		return fmt.Errorf("pre-edit sync: %w", err)
	}
	var missing []string

	if patches, ok := params["patches"].([]any); ok {
		for _, p := range patches {
			if m, ok := p.(map[string]any); ok {
				if id, ok := m["id"].(string); ok && findControl(b.Controls, id) == nil {
					missing = append(missing, id)
				}
			}
		}
	}

	if additions, ok := params["additions"].([]any); ok {
		for i, a := range additions {
			ctrl, ok := a.(map[string]any)
			if !ok {
				continue
			}
			after, ok := ctrl["after"].(string)
			if !ok {
				continue
			}
			sibling := findControl(b.Controls, after)
			if sibling == nil {
				missing = append(missing, after)
				continue
			}
			bbox, _ := sibling["bbox"].(map[string]any)
			dx, dy := num(ctrl["dx"]), 8.0
			if v, ok := ctrl["dy"]; ok {
				dy = num(v)
			}
			ctrl["x"] = num(bbox["left"]) + dx
			ctrl["y"] = num(bbox["top"]) + num(bbox["h"]) + dy
			delete(ctrl, "after")
			delete(ctrl, "dx")
			delete(ctrl, "dy")
			if _, ok := ctrl["zOrder"]; !ok {
				ctrl["zOrder"] = 100 + i
			}
		}
	}

	if deletions, ok := params["deletions"].([]any); ok {
		seen := map[string]bool{}
		expanded := make([]any, 0, len(deletions))
		add := func(id string) {
			if !seen[id] {
				seen[id] = true
				expanded = append(expanded, id)
			}
		}
		for _, d := range deletions {
			id, ok := d.(string)
			if !ok {
				continue
			}
			ctrl := findControl(b.Controls, id)
			if ctrl == nil {
				missing = append(missing, id)
				continue
			}
			add(id)
			for _, desc := range descendantIDs(ctrl) {
				add(desc)
			}
		}
		params["deletions"] = expanded
	}

	if len(missing) > 0 {
		return fmt.Errorf("board changed since you built this patch: controls %s no longer exist; run 'bmc board %s' and rebuild the patch",
			strings.Join(missing, ", "), boardURL)
	}
	return nil
}

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	default:
		return 0
	}
}

func descendantIDs(ctrl map[string]any) []string {
	var ids []string
	if children, ok := ctrl["children"].([]any); ok {
		for _, child := range children {
			if m, ok := child.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					ids = append(ids, id)
				}
				ids = append(ids, descendantIDs(m)...)
			}
		}
	}
	return ids
}

// cmdCreate creates a board from a YAML description (flexbox node tree).
func cmdCreate(args []string) error {
	var projectURL, file string
	preview := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-f" && i+1 < len(args):
			i++
			file = args[i]
		case args[i] == "--preview":
			preview = true
		default:
			projectURL = args[i]
		}
	}
	if projectURL == "" || file == "" {
		return errors.New("usage: bmc create <projectUrl> -f board.yaml [--preview] (keys: board, insertAfterBoardUrl)")
	}
	params := map[string]any{}
	if err := mergeFile(params, file); err != nil {
		return err
	}
	params["projectUrl"] = projectURL
	if err := lintErrors(lintCreate(params)); err != nil {
		return err
	}
	result, err := callTool("create_balsamiq_board", params)
	if err != nil {
		if result != nil {
			_ = printYAML(result)
		}
		return err
	}
	if err := printYAML(result); err != nil {
		return err
	}
	if preview {
		if url := boardURLFromResult(result); url != "" {
			return writePreview(url, "", "")
		}
	}
	return nil
}

func boardURLFromResult(result any) string {
	m, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"boardUrl", "url"} {
		if url, ok := m[key].(string); ok {
			return url
		}
	}
	return ""
}

// cmdPreview renders the board and writes the PNG to disk, printing only the
// path. --node crops the image to one control's bounding box so a small
// retouch can be verified without re-reading a whole multi-screen render.
func cmdPreview(args []string) error {
	var boardURL, out, node string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o" && i+1 < len(args):
			i++
			out = args[i]
		case args[i] == "--node" && i+1 < len(args):
			i++
			node = args[i]
		default:
			boardURL = args[i]
		}
	}
	if boardURL == "" {
		return errors.New("usage: bmc preview <boardUrl> [--node <controlId>] [-o out.png]")
	}
	return writePreview(boardURL, node, out)
}

func writePreview(boardURL, node, out string) error {
	c, err := dial()
	if err != nil {
		return err
	}
	raw, err := c.call("tools/call", map[string]any{
		"name": "get_balsamiq_board_preview", "arguments": map[string]any{"boardUrl": boardURL},
	})
	if err != nil {
		return err
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Data string `json:"data"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	for _, blk := range res.Content {
		if blk.Type != "image" {
			continue
		}
		img, err := base64.StdEncoding.DecodeString(blk.Data)
		if err != nil {
			return err
		}
		ext := ".png"
		if node != "" {
			if img, err = cropToNode(boardURL, node, img); err != nil {
				return err
			}
		} else if bytes.HasPrefix(img, []byte("RIFF")) {
			ext = ".webp"
		}
		if out == "" {
			out = boardCachePath(boardURL) + suffixFor(node) + ext
		}
		if err := os.WriteFile(out, img, 0o644); err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	}
	return errors.New("no image in preview response")
}

func suffixFor(node string) string {
	if node == "" {
		return ""
	}
	return "_" + node
}

func callTool(name string, params map[string]any) (any, error) {
	c, err := dial()
	if err != nil {
		return nil, err
	}
	raw, err := c.call("tools/call", map[string]any{"name": name, "arguments": params})
	if err != nil {
		return nil, err
	}
	return toolResult(raw)
}
