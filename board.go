package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
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
		return errors.New("usage: bais toc <projectUrl>")
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

// cmdBoard prints the compact control map: id, type, geometry, text.
func cmdBoard(args []string) error {
	var boardURL string
	refresh := false
	full := false
	for _, a := range args {
		switch a {
		case "--refresh":
			refresh = true
		case "--full":
			full = true
		default:
			boardURL = a
		}
	}
	if boardURL == "" {
		return errors.New("usage: bais board <boardUrl> [--refresh] [--full]")
	}
	b, err := loadBoard(boardURL, refresh)
	if err != nil {
		return err
	}
	if full {
		return printYAML(map[string]any{"name": b.Name, "content": anySlice(b.Controls)})
	}
	fmt.Printf("board: %s\n", b.Name)
	fmt.Println("id type x,y wxh text (indent = nesting)")
	for _, ctrl := range b.Controls {
		printControlLine(ctrl, 0)
	}
	return nil
}

func anySlice(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func printControlLine(ctrl map[string]any, depth int) {
	id, _ := ctrl["id"].(string)
	if id == "" {
		id = "-"
	}
	typ, _ := ctrl["type"].(string)
	geo := ""
	if bbox, ok := ctrl["bbox"].(map[string]any); ok {
		geo = fmt.Sprintf("%v,%v %vx%v", bbox["left"], bbox["top"], bbox["w"], bbox["h"])
	}
	line := fmt.Sprintf("%s%s %s %s", strings.Repeat("  ", depth), id, typ, geo)
	if text, ok := ctrl["text"].(string); ok && text != "" {
		if len(text) > 60 {
			text = text[:57] + "..."
		}
		line += " " + fmt.Sprintf("%q", text)
	}
	fmt.Println(line)
	if children, ok := ctrl["children"].([]any); ok {
		for _, child := range children {
			if m, ok := child.(map[string]any); ok {
				printControlLine(m, depth+1)
			}
		}
	}
}

// cmdShow prints the full properties of one control from the cached board,
// with children collapsed to their ids.
func cmdShow(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: bais show <boardUrl> <controlId>")
	}
	b, err := loadBoard(args[0], false)
	if err != nil {
		return err
	}
	ctrl := findControl(b.Controls, args[1])
	if ctrl == nil {
		return fmt.Errorf("control %q not found (run: bais board %s --refresh)", args[1], args[0])
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
	for i := 0; i < len(args); i++ {
		if args[i] == "-f" && i+1 < len(args) {
			i++
			file = args[i]
		} else {
			boardURL = args[i]
		}
	}
	if boardURL == "" || file == "" {
		return errors.New("usage: bais edit <boardUrl> -f patch.yaml (keys: additions, patches, deletions)")
	}
	params := map[string]any{}
	if err := mergeFile(params, file); err != nil {
		return err
	}
	params["boardUrl"] = boardURL
	if err := syncAndCheckIDs(boardURL, params); err != nil {
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
	return printYAML(result)
}

// syncAndCheckIDs re-fetches the board right before an edit, so the cache
// reflects any manual changes made in the editor meanwhile, and rejects the
// edit early if a targeted control no longer exists.
func syncAndCheckIDs(boardURL string, params map[string]any) error {
	targeted := make([]string, 0, 8)
	if patches, ok := params["patches"].([]any); ok {
		for _, p := range patches {
			if m, ok := p.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					targeted = append(targeted, id)
				}
			}
		}
	}
	if deletions, ok := params["deletions"].([]any); ok {
		for _, d := range deletions {
			if id, ok := d.(string); ok {
				targeted = append(targeted, id)
			}
		}
	}
	b, err := fetchBoard(boardURL)
	if err != nil {
		return fmt.Errorf("pre-edit sync: %w", err)
	}
	var missing []string
	for _, id := range targeted {
		if findControl(b.Controls, id) == nil {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("board changed since you built this patch: controls %s no longer exist; run 'bais board %s' and rebuild the patch",
			strings.Join(missing, ", "), boardURL)
	}
	return nil
}

// cmdCreate creates a board from a YAML description (flexbox node tree).
func cmdCreate(args []string) error {
	var projectURL, file string
	for i := 0; i < len(args); i++ {
		if args[i] == "-f" && i+1 < len(args) {
			i++
			file = args[i]
		} else {
			projectURL = args[i]
		}
	}
	if projectURL == "" || file == "" {
		return errors.New("usage: bais create <projectUrl> -f board.yaml (keys: board, insertAfterBoardUrl)")
	}
	params := map[string]any{}
	if err := mergeFile(params, file); err != nil {
		return err
	}
	params["projectUrl"] = projectURL
	result, err := callTool("create_balsamiq_board", params)
	if err != nil {
		if result != nil {
			_ = printYAML(result)
		}
		return err
	}
	return printYAML(result)
}

// cmdPreview renders the board and writes the PNG to disk, printing only the path.
func cmdPreview(args []string) error {
	var boardURL, out string
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			i++
			out = args[i]
		} else {
			boardURL = args[i]
		}
	}
	if boardURL == "" {
		return errors.New("usage: bais preview <boardUrl> [-o out.png]")
	}
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
			Type     string `json:"type"`
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
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
		if out == "" {
			out = boardCachePath(boardURL) + ".png"
		}
		if err := os.WriteFile(out, img, 0o644); err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	}
	return errors.New("no image in preview response")
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
