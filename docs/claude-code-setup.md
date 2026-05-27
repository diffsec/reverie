# Claude Code Setup

Step-by-step guide for wiring reverie into Claude Code.

## 1. Build reverie

Option A -- install to `$GOPATH/bin`:

```bash
cd /path/to/reverie
go install ./cmd/reverie
```

The binary lands at `$(go env GOPATH)/bin/reverie`. Make sure that directory is on your `$PATH`.

Option B -- build in place:

```bash
cd /path/to/reverie
go build -o reverie ./cmd/reverie
```

Use the absolute path to the binary in the config below.

## 2. Ensure Ollama is running

Reverie uses Ollama for local embeddings by default. Pull the model if you haven't already:

```bash
ollama pull nomic-embed-text
```

Verify Ollama is running:

```bash
curl -s http://localhost:11434/v1/models | head -5
```

If Ollama isn't running when reverie starts, `memory_write` and `memory_recall` return clean errors -- no data is corrupted.

## 3. Register the MCP server

Use the `claude mcp` CLI -- this writes the entry to `~/.claude.json` (the file Claude Code actually reads MCP servers from). Do NOT hand-edit `~/.claude/settings.json` for MCP config; it is the wrong file and is ignored.

```bash
claude mcp add --scope user reverie "$(command -v reverie || echo $(go env GOPATH)/bin/reverie)" serve
```

Or with an absolute path:

```bash
claude mcp add --scope user reverie /Users/you/Code/reverie/reverie serve
```

No API keys or env vars are needed with Ollama. If using Voyage instead, pass the key with `-e`:

```bash
claude mcp add --scope user -e VOYAGE_API_KEY="$VOYAGE_API_KEY" reverie reverie serve
```

Verify it landed:

```bash
claude mcp list
```

You should see `reverie: ... - ✓ Connected`.

## 4. Add the reverie preamble to CLAUDE.md

The canonical preamble lives at [`scripts/reverie-preamble.md`](../scripts/reverie-preamble.md) -- single source of truth for the agent-facing memory instructions.

If you ran `./scripts/install.sh`, it's already injected into `~/.claude/CLAUDE.md` between managed markers (`<!-- BEGIN reverie-preamble -->` ... `<!-- END reverie-preamble -->`) and will be refreshed in place on subsequent runs. Pass `--skip-preamble` to opt out.

For a manual install, append the file's contents to your `~/.claude/CLAUDE.md`:

```bash
{ printf '\n'; cat scripts/reverie-preamble.md; } >> ~/.claude/CLAUDE.md
```

This teaches Claude Code how and when to use reverie's tools in every session.

## 5. Restart Claude Code

Quit and relaunch the `claude` CLI. MCP servers are discovered at startup.

## 6. Verify

Start a Claude Code session and run `/mcp`. You should see `reverie` listed as a connected server with 7+ tools:

- `memory_recall`
- `memory_write`
- `memory_apply_judgment`
- `memory_reinforce`
- `memory_forget`
- `memory_list`
- `memory_decay_tick`

If reverie is not listed, check:
- Is Ollama running? (`ollama list` should show `nomic-embed-text`)
- Is the binary path correct? (`claude mcp get reverie` to inspect)
- Check stderr output: `reverie serve 2>reverie.log` and inspect `reverie.log`.

## 7. Test

Ask Claude to write a test memory:

> "Write a memory that my preferred language is Go."

Then recall it:

> "Recall memories about my language preferences."

You should see the fact returned with a similarity score and gate pass flags. If you see results, reverie is working end-to-end.
