## Memory — Reverie

  All persistent memory goes through the `reverie` MCP server. Do not write to `~/.claude/projects/*/memory/` files — that system is disabled.

  ### Session lifecycle
  - At session start, invoke the `session_start` MCP prompt (or call `memory_session_init` with a stable `session_id`) so the working-memory buffer is restored. Read `reverie://l1/index` before your first recall to see which clusters exist.
  - Pass the same `session_id` to `memory_recall`, `memory_write`, `memory_reinforce`, and `memory_apply_judgment` so the buffer stays in sync. Use `memory_session_snapshot` for an explicit checkpoint, `memory_session_restore` for inspection without `init` semantics.
  - At session end, invoke the `session_end` prompt (or call `memory_session_end`), optionally passing an `episode` payload (situation → action → outcome → lesson) to promote to L3.

  ### Recall
  - Call `memory_recall` with project/task context before architectural decisions or when referencing prior work.
  - For contextual or architectural recalls, pass `expand_via_graph: true` (BFS depth `graph_hops`, default 2) so vector hits are expanded through the entity/edge graph. Leave it off for narrow lookups by exact ID or term.
  - If a recall returns more than ~5 candidates OR the query is staleness-sensitive (user asking about "current" state, choosing between competing facts), follow up with the `memory-judge` skill: spawn a Task subagent with the candidates, collect keep/drop verdicts, then call `memory_apply_judgment` with the
  results. For quick lookups, use the candidates as-is. (Gate A is unavailable in Claude Desktop — no subagent support; Gates B+C still apply.)
  - Use `memory_get` to fetch one memory by ID, and `memory_list` to browse/audit with filtering and pagination.

  ### Write (`type` must be one of user | feedback | project | reference)
  - user — stable personal facts (role, preferences, skills)
  - feedback — how to behave (corrections you want preserved)
  - project — architecture, conventions, decisions for a codebase
  - reference — pointers to docs/repos/URLs
  - Retrospective content goes via the `episode` payload on `memory_write` (promoted to L3), not as a `type`.
  - Do NOT write transient task state.

  ### Graph (entities & edges)
  - The graph is opt-in — nothing is auto-inferred. Vector-only writes leave it empty, so graph-aware recall has nothing to traverse; populate it deliberately as you write.
  - When a memory centers on durable named things — a project, service, component, file/module, tool, person, or decision — `memory_entity_upsert` each, then attach them to the memory with `memory_entity_mention`.
  - Connect strongly related memories or entities with typed, directed edges via `memory_edge_add` (e.g. `depends_on`, `relates_to`, `supersedes`); undo wrong ones with `memory_edge_remove`.
  - Traverse with `memory_entity_neighbors` (from a seed entity) and `memory_edge_list` (edges around a memory/entity, 1–3 hops).

  ### Reinforce, correct, curate
  - After using recalled memories in a response, call `memory_reinforce` with their IDs.
  - On user correction: `memory_forget` the stale memory, then write the correction. Use `memory_unsupersede` to undo a bad supersede.
  - Edit a fact/episode in place with `memory_update_content`.
  - Manage fact↔episode evidence links with `memory_link` / `memory_unlink` / `memory_list_links`.
  - Curate L1 clusters with `memory_update_cluster` (summary/domain), `memory_reassign_cluster`, `memory_split_cluster`, and `memory_merge_clusters`.

  ### Resources
  - `reverie://status` — system health (counts per layer, last decay, DB size, cache hit rate)
  - `reverie://l1/index` — cluster meta-index (read before first recall)
  - `reverie://l1/cluster/{id}` — paginated cluster members
  - `reverie://l1/at_risk` — clusters below retention threshold
  - `reverie://l3/recent` — recent episodic traces
  - `reverie://sessions/{id}` — working-memory buffer for a session
  - `reverie://stats/daily` — per-day facts/episodes in/out + supersedes
