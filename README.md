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
bais board <boardUrl> [--refresh|--full]  # compact control map: id type x,y wxh "text"
bais show <boardUrl> <id>                 # full props of one control, from local cache
bais edit <boardUrl> -f patch.yaml        # atomic edit: additions / patches / deletions
bais create <projectUrl> -f board.yaml    # new board from a flexbox node tree
bais preview <boardUrl> [-o out.png]      # render board to PNG, prints the path
bais tools [name]                         # list tools / one tool's input schema
bais call <tool> k=v k2:='{"j":1}' -f a.yaml [--raw] [--path a.b[0].c]
```

A patch file sends only what changes; the server applies it atomically (one bad
id rejects the whole edit) and records it in the project's version history:

```yaml
patches:
  - id: "45"
    props: {text: Souscrire, color: "#2266aa"}
additions:
  - {controlType: input, x: 1273, y: 700, width: 258, height: 32, zOrder: 40}
deletions: ["12"]
```

Board content is cached as plain JSON files in the user cache dir (inspectable
with `jq`). `bais edit` re-fetches the board just before sending, so manual
edits made in the editor meanwhile are picked up, and aborts early if a
targeted control id no longer exists.

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
