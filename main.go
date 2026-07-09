package main

import (
	"fmt"
	"os"
)

const usage = `bais - token-efficient Balsamiq CLI (MCP client)

Usage:
  bais login | logout                       OAuth against balsamiq.cloud
  bais projects                             list projects (name, space, url)
  bais toc <projectUrl>                     boards of a project, one line each
  bais board <boardUrl>                     compact control map (id type "text")
       [--geo] [--depth n] [--find text] [--type button] [--refresh] [--full]
  bais show <boardUrl> <controlId>          full props of one control (from local cache)
  bais edit <boardUrl> -f patch.yaml        atomic edit: additions / patches / deletions
       [--preview]                          (lint offline, pre-edit sync, recursive delete)
  bais create <projectUrl> -f board.yaml    new board from a flexbox node tree [--preview]
  bais preview <boardUrl> [--node <id>]     render board (or one control) to a PNG
       [-o out.png]
  bais expand -f payload.yaml               dry-run: expanded + linted payload, no send
  bais tools [name]                         list tools / show one input schema
  bais call <tool> [k=v] [k:=json] [-f f]   raw tool call (--raw, --path a.b[0].c)

Board content is cached in ~/Library/Caches/bais; board --refresh refetches,
edit re-syncs and invalidates automatically.

Theme: nearest .bais.yaml above the cwd (or $BAIS_THEME) defines color tokens
($primary -> #009e0f) and parametrized partials, invoked in payloads with
{use: pill, with: {text: AJOUTÉE}}. See bais expand to check the result.

Env:
  BAIS_URL     MCP endpoint (default https://bais.balsamiq.com/mcp)
  BAIS_THEME   theme file path (overrides .bais.yaml discovery)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "login":
		err = cmdLogin()
	case "logout":
		err = cmdLogout()
	case "projects":
		err = cmdProjects()
	case "toc":
		err = cmdTOC(os.Args[2:])
	case "board":
		err = cmdBoard(os.Args[2:])
	case "show":
		err = cmdShow(os.Args[2:])
	case "edit":
		err = cmdEdit(os.Args[2:])
	case "create":
		err = cmdCreate(os.Args[2:])
	case "preview":
		err = cmdPreview(os.Args[2:])
	case "expand":
		err = cmdExpand(os.Args[2:])
	case "tools":
		err = cmdTools(os.Args[2:])
	case "call":
		err = cmdCall(os.Args[2:])
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func mcpURL() string {
	if u := os.Getenv("BAIS_URL"); u != "" {
		return u
	}
	return "https://bais.balsamiq.com/mcp"
}
