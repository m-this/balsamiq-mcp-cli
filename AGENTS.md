# Agent instructions

This repo is `bmc`, a token-efficient CLI for Balsamiq wireframes. Build and
test with the Makefile: `make build`, `make test`, `make install`.

## Working with Balsamiq boards

The complete playbook for reading, editing, and creating boards through `bmc`
lives in [.claude/agents/balsamiq-wireframer.md](.claude/agents/balsamiq-wireframer.md).
Before any wireframe task, read that file (skip its YAML frontmatter) and
follow it. It covers the read workflow (map, filters, show, preview), the edit
payload format (patches, moves, relative additions, arrow connectors, device
frames, safe deletions), the theme file, and the server limits you should not
fight.
