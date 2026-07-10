# balsamiq-mcp-cli (bmc)

Token-efficient CLI for Balsamiq wireframes. Talks directly to the Balsamiq MCP
endpoint (`https://bais.balsamiq.com/mcp`) over streamable HTTP, so no MCP server
needs to be loaded into an agent's context: no tool schemas in the prompt, and
responses come back as pruned YAML instead of raw JSON.

## Install

```sh
git clone https://github.com/m-this/balsamiq-mcp-cli && cd balsamiq-mcp-cli
make install
```

`make install` builds the `bmc` binary into `go env GOBIN` (falling back to
`GOPATH/bin`) on any OS. `go install github.com/m-this/balsamiq-mcp-cli@latest`
also works but names the binary after the module.

## Usage

```sh
bmc login                                # OAuth (dynamic client registration + PKCE)
bmc projects                             # projects (name, space, url)
bmc toc <projectUrl>                     # boards of a project, one line each
bmc board <boardUrl>                     # compact control map: id type "text"
     [--geo] [--depth n] [--find text] [--type button] [--refresh] [--full]
bmc show <boardUrl> <id> [id...]         # full props of one or more controls, from local cache
bmc edit <boardUrl> -f patch.yaml        # atomic edit: additions / patches / moves / deletions
bmc create <projectUrl> -f board.yaml    # new board from a flexbox node tree
bmc preview <boardUrl> [--node <id>]     # render board (or crop one control) to an image
     [--scale f]                         #   upscale the result so small details stay legible
bmc expand -f payload.yaml               # dry-run: expanded + linted payload, no send
bmc tools [name]                         # list tools / one tool's input schema
bmc call <tool> k=v k2:='{"j":1}' -f a.yaml [--raw] [--path a.b[0].c]
```

A patch file sends only what changes; the server applies it atomically (one bad
id rejects the whole edit) and records it in the project's version history:

```yaml
patches:
  - id: "45"
    props: {text: Souscrire, color: $primary}     # theme color token
moves:
  - {id: "39", dy: 100}                           # shifts "39" AND its whole subtree
additions:
  - {controlType: input, x: 1273, y: 700, width: 258, height: 32, zOrder: 40}
  - {controlType: sticky-note, after: "45", dy: 20, width: 140, height: 60, text: note}
  - {controlType: arrow, from: "46", to: "81", text: puis}   # edge-to-edge connector
  - {controlType: iphone, x: 620, y: 1300}        # CLI-expanded device frame
  - {controlType: icon, parent: "39", rx: 20, ry: 30, width: 32, height: 32}
  - {use: pill, with: {text: AJOUTÉE, x: 100, y: 200}}   # theme partial
deletions:
  - "12"                                          # deletes exactly this control
  - {id: "27", subtree: true}                     # also sweeps everything nested under it
```

Before sending, the CLI lints the payload offline (unknown controlType, missing
geometry, CommonMark markup in text, unresolved color tokens), re-fetches the
board so manual editor changes are picked up, aborts if a targeted id no longer
exists, and resolves relative positions (`after:` sibling, `parent:` container
with `rx`/`ry` offsets).

Deleting a container on the server leaves its nested controls behind, so
`subtree: true` expands the deletion to everything under it - but the read
hierarchy is *spatial*, so a control overlapping unrelated content captures it
as children. Subtree sweeps are therefore opt-in and each swept control is
printed to stderr before sending.

A `move` shifts a control and, by default, every control nested under it
(`recursive: false` moves just the one), expanding to one x/y patch per control
from the freshly-synced geometry - a section reflow is one line instead of
dozens of hand-computed patches. An arrow addition with `from`/`to` control ids
computes its endpoints on each control's border along the center-to-center line
(`gap: n` tunes the 8 px standoff), avoiding the center-to-center trap where
both arrowheads land inside the boxes.

`parent: id` positions an addition at `rx`/`ry` inside a container's bbox, so
it reads back as that control's spatial child and follows it in later `moves`.
The pseudo-controlTypes `iphone` and `browser` (absent from the BAIS schema)
expand client-side into the rectangles, shapes, and inputs that emulate them:
body + notch + home indicator, or window + toolbar + traffic lights + URL bar
(`text` sets the URL). All additions still land at the board root and stack
above existing content - the server ignores `zOrder` against existing controls,
so a background behind existing content is not achievable.

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
array. `bmc expand -f payload.yaml` shows the resolved payload without
sending it.

Board content is cached as plain JSON files in the user cache dir (inspectable
with `jq`).

Credentials are stored in `~/Library/Application Support/bais/credentials.json`
(0600) and refreshed automatically.

## Coding agents

The wireframing playbook ships with the repo, one entry point per tool, all
pointing at the same canonical guide:

- **Claude Code**: `.claude/agents/balsamiq-wireframer.md` is picked up as a
  project agent (the canonical guide).
- **Codex**: `AGENTS.md` at the root is loaded automatically and defers to the
  guide.
- **opencode**: `AGENTS.md` plus a dedicated subagent in
  `.opencode/agent/balsamiq-wireframer.md`.

## Why not MCP directly

The MCP server injects every tool schema into the context (the create/edit
schemas alone are ~53 KB each) and returns verbose JSON. `bmc` keeps the same
backend but:

- keeps tool schemas out of the agent context entirely
- digests boards to one line per control instead of the full JSON tree
- prunes nulls, empty strings, and empty containers, and renders YAML
- caches boards locally so re-reads and single-control lookups are free
- writes previews to disk instead of streaming base64 through the context
