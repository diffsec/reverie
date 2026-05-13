package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"
)

// migration describes a single schema migration. Version must be unique and
// strictly monotonically increasing across the migrations slice.
type migration struct {
	Version int
	Name    string
	SQL     string
}

// migrations is the ordered list of schema migrations. New migrations MUST be
// appended at the end; never reorder or renumber existing entries.
//
// Migration 1 is the initial schema (identical to schema.sql). Every statement
// in it uses CREATE TABLE/INDEX IF NOT EXISTS, so running it against a legacy
// database that already has these tables (from pre-migration startup paths)
// is a safe no-op. Subsequent migrations then advance the schema forward.
var migrations = []migration{
	{
		Version: 1,
		Name:    "initial_schema",
		SQL:     schemaSQL,
	},
	{
		Version: 2,
		Name:    "add_tags_columns",
		SQL: `ALTER TABLE facts    ADD COLUMN tags TEXT DEFAULT '[]';
ALTER TABLE episodes ADD COLUMN tags TEXT DEFAULT '[]';`,
	},
	{
		Version: 3,
		Name:    "observability_tables",
		// Creates the two tables needed by Phase 5 observability:
		//   daily_stats — per-day activity counters, maintained by triggers on
		//                 facts/episodes so any write path (handlers or CLIs)
		//                 stays consistent.
		//   decay_state — singleton row holding last_tick timestamp for the
		//                 decay scheduler (populated by 5A's TickDecay; seeded
		//                 NULL here so the row always exists).
		//
		// Triggers use date('now'), which SQLite evaluates in UTC — aligning
		// with the UTC created_at timestamps already written by the store.
		//
		// Backfill strategy: a single LEFT JOIN-ed UNION aggregate per date
		// would be simplest but clumsy with IF NOT EXISTS across versions; we
		// instead run two inserts. The facts backfill creates the date row
		// (other columns default to 0); the episodes backfill then uses
		// ON CONFLICT(date) DO UPDATE so it correctly adds episodes_in onto
		// rows already seeded by the facts pass. Plain INSERT OR IGNORE would
		// silently drop the episodes count for any date that already had a
		// row, which is the gotcha the spec warns about.
		//
		// Both backfills are naturally idempotent across reruns: PRIMARY KEY
		// collisions fire ON CONFLICT, and since the migration only runs when
		// schema_migrations says version<3, we never actually hit the
		// second-run path in practice — the one-shot semantics come from
		// applyMigrations' bookkeeping, not the SQL itself.
		SQL: `CREATE TABLE IF NOT EXISTS daily_stats (
  date         TEXT PRIMARY KEY,
  facts_in     INTEGER DEFAULT 0,
  facts_out    INTEGER DEFAULT 0,
  episodes_in  INTEGER DEFAULT 0,
  episodes_out INTEGER DEFAULT 0,
  supersedes   INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS decay_state (
  id        INTEGER PRIMARY KEY CHECK(id = 1),
  last_tick TEXT
);
INSERT OR IGNORE INTO decay_state (id, last_tick) VALUES (1, NULL);

CREATE TRIGGER IF NOT EXISTS trg_facts_insert
AFTER INSERT ON facts BEGIN
  INSERT INTO daily_stats(date, facts_in) VALUES (date('now'), 1)
  ON CONFLICT(date) DO UPDATE SET facts_in = facts_in + 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_facts_delete
AFTER DELETE ON facts BEGIN
  INSERT INTO daily_stats(date, facts_out) VALUES (date('now'), 1)
  ON CONFLICT(date) DO UPDATE SET facts_out = facts_out + 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_facts_supersede
AFTER UPDATE OF superseded_by ON facts
WHEN NEW.superseded_by IS NOT NULL AND OLD.superseded_by IS NULL BEGIN
  INSERT INTO daily_stats(date, supersedes) VALUES (date('now'), 1)
  ON CONFLICT(date) DO UPDATE SET supersedes = supersedes + 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_episodes_insert
AFTER INSERT ON episodes BEGIN
  INSERT INTO daily_stats(date, episodes_in) VALUES (date('now'), 1)
  ON CONFLICT(date) DO UPDATE SET episodes_in = episodes_in + 1;
END;

CREATE TRIGGER IF NOT EXISTS trg_episodes_delete
AFTER DELETE ON episodes BEGIN
  INSERT INTO daily_stats(date, episodes_out) VALUES (date('now'), 1)
  ON CONFLICT(date) DO UPDATE SET episodes_out = episodes_out + 1;
END;

INSERT OR IGNORE INTO daily_stats(date, facts_in)
SELECT date(created_at), COUNT(*) FROM facts
GROUP BY date(created_at);

INSERT INTO daily_stats(date, episodes_in)
SELECT date(created_at), COUNT(*) FROM episodes
GROUP BY date(created_at)
ON CONFLICT(date) DO UPDATE SET episodes_in = episodes_in + excluded.episodes_in;`,
	},
	{
		Version: 4,
		Name:    "session_metadata",
		// Extends the sessions table with the fields required by Phase 6:
		// project_hint / tags for TaskMeta round-trips, created_at so the
		// resource can surface a start time, and closed_at so closed sessions
		// can be detected without a side table. ALTER TABLE ADD COLUMN is
		// safe on existing rows (they pick up the defaults), and SQLite's
		// NULL default for closed_at is exactly the "open session" sentinel
		// the rest of the code relies on.
		SQL: `ALTER TABLE sessions ADD COLUMN project_hint TEXT DEFAULT '';
ALTER TABLE sessions ADD COLUMN tags TEXT DEFAULT '[]';
ALTER TABLE sessions ADD COLUMN created_at TEXT DEFAULT (datetime('now'));
ALTER TABLE sessions ADD COLUMN closed_at TEXT;
CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);`,
	},
	{
		Version: 5,
		Name:    "knowledge_graph",
		// Promotes the proto-graph (fact_episode_links) to a real knowledge
		// graph for Phase 7. Three new tables are created and the legacy
		// links table is replaced by the more general memory_edges:
		//
		//   memory_edges    — typed, weighted, directed edges between any
		//                     two memory IDs (fact, episode, or entity).
		//                     src_id/dst_id are deliberately polymorphic —
		//                     no FK on either column — because the layer of
		//                     the endpoint is decided at the application
		//                     layer. Indexes on src_id and dst_id make
		//                     either-direction traversal cheap; the
		//                     PRIMARY KEY (src,dst,type) gives INSERT OR
		//                     IGNORE its idempotency.
		//
		//   entities        — first-class graph nodes (files, repos, libs,
		//                     concepts, …) that decay using the same
		//                     Ebbinghaus formula as L1 clusters. Default
		//                     utility/frequency/retention match a fresh
		//                     cluster so the first decay tick can't
		//                     immediately drop a new entity below
		//                     threshold. UNIQUE(name, entity_type) gates
		//                     dedup by exact match; cosine-similarity
		//                     dedup runs application-side before INSERT.
		//
		//   entity_mentions — memory -> entity many-to-many. memory_id is
		//                     polymorphic over facts and episodes; no FK
		//                     for the same reason as memory_edges.
		//                     PRIMARY KEY (memory_id, entity_id) keeps the
		//                     write-side INSERT OR IGNORE idempotent.
		//
		// Data move: every existing fact_episode_links row is copied into
		// memory_edges with src_id=fact_id, dst_id=episode_id. The
		// COALESCE on link_type defends against NULLs that may have been
		// written before Phase 4 enforced the 'evidence' default — Phase
		// 4 added the DEFAULT but didn't backfill NULL columns. created_at
		// is stamped to datetime('now') because the legacy table never
		// stored a creation timestamp; we'd rather have "migrated at
		// migration time" than NULL.
		//
		// fact_episode_links is then dropped. The previous ON DELETE
		// CASCADE was carried by SQLite FKs; on memory_edges and
		// entity_mentions the application layer (DeleteFact/DeleteEpisode
		// in the store) is responsible for the cascade, because the IDs
		// are polymorphic.
		SQL: `CREATE TABLE memory_edges (
  src_id      TEXT NOT NULL,
  dst_id      TEXT NOT NULL,
  edge_type   TEXT NOT NULL,
  weight      REAL DEFAULT 1.0,
  created_at  TEXT,
  PRIMARY KEY (src_id, dst_id, edge_type)
);
CREATE INDEX idx_edges_src ON memory_edges(src_id);
CREATE INDEX idx_edges_dst ON memory_edges(dst_id);

CREATE TABLE entities (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  entity_type  TEXT NOT NULL,
  embedding    BLOB,
  utility      REAL DEFAULT 0.5,
  frequency    REAL DEFAULT 0.5,
  turns_since  INTEGER DEFAULT 0,
  retention    REAL DEFAULT 1.0,
  last_access  TEXT,
  created_at   TEXT,
  UNIQUE(name, entity_type)
);

CREATE TABLE entity_mentions (
  memory_id  TEXT NOT NULL,
  entity_id  TEXT NOT NULL,
  role       TEXT,
  PRIMARY KEY (memory_id, entity_id)
);

-- Forward every fact_episode_links row into memory_edges. COALESCE keeps
-- pre-Phase-4 NULL link_type rows from violating the NOT NULL on edge_type
-- by substituting the canonical 'evidence' default.
INSERT INTO memory_edges (src_id, dst_id, edge_type, created_at)
SELECT fact_id, episode_id, COALESCE(link_type, 'evidence'), datetime('now')
FROM fact_episode_links;

DROP TABLE fact_episode_links;`,
	},
}

// applyMigrations ensures the schema_migrations bookkeeping table exists and
// applies every migration with Version > max(applied). Each migration runs in
// its own transaction: on failure the transaction is rolled back and no row is
// recorded in schema_migrations, so the migration will be retried on the next
// Open call. Fully-migrated databases are a no-op past the bookkeeping query.
func applyMigrations(db *sql.DB) error {
	// Bookkeeping table. Uses IF NOT EXISTS so repeated calls are safe.
	_, err := db.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			applied_at TEXT NOT NULL
		)`,
	)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Determine the highest applied version. COALESCE protects against an
	// empty table where MAX() returns NULL.
	var current int
	if err := db.QueryRow(
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
	).Scan(&current); err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if err := applyMigration(db, m); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
		}
	}
	return nil
}

// applyMigration runs a single migration inside a transaction. On any error
// the transaction is rolled back, leaving the database unchanged and the
// migration un-recorded.
func applyMigration(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Rollback is a no-op once Commit has succeeded; it's safe to defer
	// unconditionally so that any early return path tears the tx down.
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(m.SQL); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.Version, m.Name, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("insert schema_migrations: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
