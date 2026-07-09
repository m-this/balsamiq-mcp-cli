# bais

Token-efficient CLI for Balsamiq wireframes. Talks directly to the Balsamiq MCP
endpoint (`https://bais.balsamiq.com/mcp`) over streamable HTTP, so no MCP server
needs to be loaded into an agent's context: no tool schemas in the prompt, and
responses come back as pruned YAML instead of raw JSON.

## Install

```sh
go install github.com/mathisfaivre/bais@latest
```

## Usage

```sh
bais login                                # OAuth (dynamic client registration + PKCE)
bais projects                             # projects (name, space, url)
bais toc <projectUrl>                     # boards of a project, one line each
bais board <boardUrl>                     # compact control map: id type "text"
     [--geo] [--depth n] [--find text] [--type button] [--refresh] [--full]
bais show <boardUrl> <id>                 # full props of one control, from local cache
bais edit <boardUrl> -f patch.yaml        # atomic edit: additions / patches / deletions
bais create <projectUrl> -f board.yaml    # new board from a flexbox node tree
bais preview <boardUrl> [--node <id>]     # render board (or crop one control) to an image
bais expand -f payload.yaml               # dry-run: expanded + linted payload, no send
bais tools [name]                         # list tools / one tool's input schema
bais call <tool> k=v k2:='{"j":1}' -f a.yaml [--raw] [--path a.b[0].c]
```

A patch file sends only what changes; the server applies it atomically (one bad
id rejects the whole edit) and records it in the project's version history:

```yaml
patches:
  - id: "45"
    props: {text: Souscrire, color: $primary}     # theme color token
additions:
  - {controlType: input, x: 1273, y: 700, width: 258, height: 32, zOrder: 40}
  - {controlType: sticky-note, after: "45", dy: 20, width: 140, height: 60, text: note}
  - {use: pill, with: {text: AJOUTÉE, x: 100, y: 200}}   # theme partial
deletions: ["12"]                                 # subtree deleted recursively
```

Before sending, the CLI lints the payload offline (unknown controlType, missing
geometry, CommonMark markup in text, unresolved color tokens), re-fetches the
board so manual editor changes are picked up, aborts if a targeted id no longer
exists, resolves relative `after:` positions, and expands deletions to whole
subtrees so no orphan children are left behind.

## Theme file

The nearest `.bais.yaml` above the working directory (or `$BAIS_THEME`) defines
color tokens and parametrized partials, so repeated structures (app chrome,
steppers, pills, label/value rows) are written once and invoked in one line:

```yaml
colors:
  primary: "#009e0f"
partials:
  pill:
    params: {text: PILL, color: $primary, x: 0, y: 0}
    body:
      controlType: rectangle
      backgroundColor: "${color}"
      x: "${x}"                 # whole-string ${param} keeps the param's type
      "y": "${y}"
      width: 90
      height: 24
      zOrder: 50
```

A partial body may be a list of controls; it splices into the surrounding
array. `bais expand -f payload.yaml` shows the resolved payload without
sending it.

Board content is cached as plain JSON files in the user cache dir (inspectable
with `jq`).

Credentials are stored in `~/Library/Application Support/bais/credentials.json`
(0600) and refreshed automatically.

## Why not MCP directly

The MCP server injects every tool schema into the context (the create/edit
schemas alone are ~53 KB each) and returns verbose JSON. `bais` keeps the same
backend but:

- keeps tool schemas out of the agent context entirely
- digests boards to one line per control instead of the full JSON tree
- prunes nulls, empty strings, and empty containers, and renders YAML
- caches boards locally so re-reads and single-control lookups are free
- writes previews to disk instead of streaming base64 through the context
