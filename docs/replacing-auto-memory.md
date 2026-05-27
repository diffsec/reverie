# Replacing Claude Code's Auto-Memory with Reverie

## Background

Claude Code has a built-in auto-memory system that stores memories as markdown files in `~/.claude/projects/<project>/memory/`. A single "conversation file" per conversation accumulates notes that later sessions re-read wholesale. It works but has limitations:

- **Code-only**: memories are not accessible from Claude Desktop or custom API harnesses.
- **No recall logic**: all memories are injected into context every session (no search, no ranking).
- **No decay**: memories accumulate forever with no staleness filtering.
- **No structured types**: no distinction between facts and episodes, no cross-references.
- **Opaque per-conversation state**: the "conversation file" is a single flat blob — no notion of which memories were actually in play, no resume semantics, no way to inspect prior session buffers from another harness.

Reverie replaces this with a proper memory system: vector search, Ebbinghaus decay, three memory layers, MCP-based access from any client, and **persistent sessions** (Phase 6) that replace the per-conversation auto-memory file with a structured, resumable working-memory buffer.

### Sessions replace the conversation file

Where auto-memory kept per-conversation notes in a markdown file, reverie models a session as first-class state:

- `memory_session_init(session_id, project_hint, tags)` at the start of a conversation creates (or resumes) a named session. If the session already exists, its prior working-memory buffer — the L2/L3 memories that were in play — is returned so the agent can resume with context intact.
- `memory_recall`, `memory_write`, `memory_reinforce`, and `memory_apply_judgment` all accept an optional `session_id`. When supplied, the session buffer auto-updates on each call (bounded by the configured budget, evicting lowest-score entries first).
- `memory_session_end(session_id, episode?)` closes the session. It runs a scoped decay tick limited to clusters touched this session (instead of bumping every cluster globally), optionally writes an L3 episode summarizing the conversation, and marks the session read-only.
- `reverie://sessions/{id}` reads back a session's buffer, metadata, and budget state from any MCP client — active or closed sessions are both readable.

Sessions are opt-in: all existing tools keep working without a `session_id`. But when adopted, they give you what the auto-memory conversation file never did — a structured, recall-aware, resumable working memory.

## Step 1: Add the CLAUDE.md preamble

Append the canonical preamble ([`scripts/reverie-preamble.md`](../scripts/reverie-preamble.md)) to `~/.claude/CLAUDE.md`. It covers the full session lifecycle (`session_start` / `memory_session_init` / `memory_session_end`) -- that lifecycle is what replaces the per-conversation auto-memory file.

If you ran `./scripts/install.sh`, this is already done -- the installer injects the preamble between managed markers in `~/.claude/CLAUDE.md`. For a manual install:

```bash
{ printf '\n'; cat scripts/reverie-preamble.md; } >> ~/.claude/CLAUDE.md
```

This teaches Claude Code to use reverie's tools instead of the built-in memory system.

## Step 2: Disable auto-memory

This is an open question. The auto-memory preamble is injected by Claude Code's built-in subsystem at the harness level -- it is not controlled by CLAUDE.md. Until a proper off-switch is available, **both** instruction blocks will appear in the system prompt and may compete.

Known options being investigated:
- A `settings.json` flag to disable auto-memory (not confirmed to exist yet).
- Removing or disabling the auto-memory skill/subsystem.
- Filing a feature request with Anthropic for an explicit opt-out.

In practice, the CLAUDE.md preamble's explicit "Do not write to ~/.claude/projects/*/memory/ files" instruction is usually sufficient to redirect the agent. The auto-memory preamble will still appear but the agent should follow the more specific reverie instructions.

## Step 3: Migrate existing memories (Phase 5)

Phase 5 will add `reverie import` to migrate existing auto-memory files into reverie:

```bash
# Import all projects
reverie import --all-projects

# Import a specific project
reverie import --project-dir ~/.claude/projects/-Users-you-Code-project/memory
```

The importer will:
- Walk `~/.claude/projects/*/memory/*.md` files.
- Parse YAML frontmatter (`name`, `description`, `type`).
- Map to reverie subtypes (user, feedback, project, reference).
- Embed via the configured provider.
- Write as L2 facts (or L3 episodes if the body has situation/action/outcome structure).
- Deduplicate via content hash.

**This is not yet implemented.** Until it ships:

- Existing memories in `~/.claude/projects/*/memory/` remain accessible to Claude Code through the built-in system.
- They will not be indexed by reverie.
- You can manually re-create important memories via `memory_write` in a Claude Code session.
- No data is lost -- the old files stay on disk untouched.

## Step 4: Verify

After setup, start a new Claude Code session and test:

1. Ask: "Write a memory that I prefer local-first tools." -- Should call `memory_write`.
2. Ask: "What are my preferences?" -- Should call `memory_recall`, not read from disk.
3. Check: `reverie status` -- Should show the new fact.

If Claude Code is still writing to `~/.claude/projects/*/memory/`, the auto-memory subsystem is overriding the preamble. Escalate to the disable-auto-memory investigation.
