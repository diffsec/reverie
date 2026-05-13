package memory

import (
	"context"
	"testing"
)

// kgTestFactory constructs a fresh Store. Both backends provide one.
type kgTestFactory func(t *testing.T) Store

// kgRunAddEdgeIdempotent covers the AddEdge happy path and idempotency.
func kgRunAddEdgeIdempotent(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	created, err := s.AddEdge(ctx, Edge{SrcID: "a", DstID: "b", EdgeType: "refines", Weight: 1.0})
	if err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	if !created {
		t.Error("first AddEdge: created = false, want true")
	}

	created, err = s.AddEdge(ctx, Edge{SrcID: "a", DstID: "b", EdgeType: "refines", Weight: 5.0})
	if err != nil {
		t.Fatalf("AddEdge repeat: %v", err)
	}
	if created {
		t.Error("repeat AddEdge: created = true, want false")
	}

	// Default weight: omitting Weight should land 1.0 in the row.
	created, err = s.AddEdge(ctx, Edge{SrcID: "a", DstID: "c", EdgeType: "evidence"})
	if err != nil || !created {
		t.Fatalf("AddEdge default weight: created=%v err=%v", created, err)
	}
	edges, err := s.ListEdges(ctx, "a", 1)
	if err != nil {
		t.Fatalf("ListEdges: %v", err)
	}
	var found bool
	for _, e := range edges {
		if e.Edge.DstID == "c" && e.Edge.EdgeType == "evidence" {
			if e.Edge.Weight != 1.0 {
				t.Errorf("default Weight = %v, want 1.0", e.Edge.Weight)
			}
			found = true
		}
	}
	if !found {
		t.Error("default-weight edge not found in ListEdges results")
	}
}

func kgRunRemoveEdge(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	// Missing edge -> deleted=false, no error.
	deleted, err := s.RemoveEdge(ctx, "x", "y", "refines")
	if err != nil {
		t.Fatalf("RemoveEdge missing: %v", err)
	}
	if deleted {
		t.Error("RemoveEdge missing: deleted = true, want false")
	}

	// Insert then remove.
	if _, err := s.AddEdge(ctx, Edge{SrcID: "x", DstID: "y", EdgeType: "refines"}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	deleted, err = s.RemoveEdge(ctx, "x", "y", "refines")
	if err != nil {
		t.Fatalf("RemoveEdge present: %v", err)
	}
	if !deleted {
		t.Error("RemoveEdge present: deleted = false, want true")
	}
}

func kgRunListEdgesTwoHops(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()
	// Build a -> b -> c chain.
	if _, err := s.AddEdge(ctx, Edge{SrcID: "a", DstID: "b", EdgeType: "refines"}); err != nil {
		t.Fatalf("AddEdge a->b: %v", err)
	}
	if _, err := s.AddEdge(ctx, Edge{SrcID: "b", DstID: "c", EdgeType: "refines"}); err != nil {
		t.Fatalf("AddEdge b->c: %v", err)
	}

	// hops=1: only a->b.
	hop1, err := s.ListEdges(ctx, "a", 1)
	if err != nil {
		t.Fatalf("ListEdges hops=1: %v", err)
	}
	if len(hop1) != 1 {
		t.Errorf("hops=1 returned %d edges, want 1", len(hop1))
	}
	if len(hop1) > 0 && hop1[0].Distance != 1 {
		t.Errorf("hops=1 distance = %d, want 1", hop1[0].Distance)
	}

	// hops=2: both edges, with correct distances.
	hop2, err := s.ListEdges(ctx, "a", 2)
	if err != nil {
		t.Fatalf("ListEdges hops=2: %v", err)
	}
	if len(hop2) != 2 {
		t.Fatalf("hops=2 returned %d edges, want 2", len(hop2))
	}
	distByDst := map[string]int{}
	for _, e := range hop2 {
		// "other" endpoint at this depth.
		if e.Distance == 1 {
			distByDst[e.Edge.DstID] = 1
		} else {
			// at depth 2 the new endpoint is c.
			distByDst[e.Edge.DstID] = e.Distance
		}
	}
	if distByDst["b"] != 1 {
		t.Errorf("distance to b = %d, want 1", distByDst["b"])
	}
	if distByDst["c"] != 2 {
		t.Errorf("distance to c = %d, want 2", distByDst["c"])
	}

	// Empty seed: no edges.
	empty, err := s.ListEdges(ctx, "nonexistent", 2)
	if err != nil {
		t.Fatalf("ListEdges empty: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("ListEdges on isolated id returned %d edges, want 0", len(empty))
	}
}

func kgRunUpsertEntityExactDedup(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	id1, created1, sim1, err := s.UpsertEntity(ctx, "config.go", "file", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("first UpsertEntity: %v", err)
	}
	if !created1 || sim1 {
		t.Errorf("first upsert: created=%v matchedBySim=%v, want true/false", created1, sim1)
	}

	id2, created2, sim2, err := s.UpsertEntity(ctx, "config.go", "file", []float32{0, 1, 0})
	if err != nil {
		t.Fatalf("repeat UpsertEntity: %v", err)
	}
	if created2 || sim2 {
		t.Errorf("repeat upsert: created=%v matchedBySim=%v, want false/false", created2, sim2)
	}
	if id1 != id2 {
		t.Errorf("repeat upsert returned different id: %q vs %q", id1, id2)
	}
}

func kgRunUpsertEntitySimilarityDedup(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	// Insert with a known unit vector.
	id1, _, _, err := s.UpsertEntity(ctx, "configloader", "file", []float32{1, 0, 0, 0})
	if err != nil {
		t.Fatalf("first UpsertEntity: %v", err)
	}

	// Different name, almost-parallel vector (cos ~= 0.96, well above 0.55).
	id2, created, sim, err := s.UpsertEntity(ctx, "config_loader", "file", []float32{0.96, 0.28, 0, 0})
	if err != nil {
		t.Fatalf("second UpsertEntity: %v", err)
	}
	if created {
		t.Error("similarity dedup: created = true, want false (should reuse)")
	}
	if !sim {
		t.Error("similarity dedup: matchedBySimilarity = false, want true")
	}
	if id1 != id2 {
		t.Errorf("similarity dedup returned different id: %q vs %q", id1, id2)
	}
}

func kgRunUpsertEntityDifferentType(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()
	// Same name, different entity_type, identical embedding -> two distinct ids.
	id1, _, _, err := s.UpsertEntity(ctx, "auth", "file", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("first UpsertEntity: %v", err)
	}
	id2, created2, sim2, err := s.UpsertEntity(ctx, "auth", "concept", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("second UpsertEntity: %v", err)
	}
	if !created2 {
		t.Error("cross-type upsert: created = false, want true")
	}
	if sim2 {
		t.Error("cross-type upsert: matchedBySim = true, want false (similarity is intra-type only)")
	}
	if id1 == id2 {
		t.Error("cross-type upsert collapsed onto same id; types should partition the namespace")
	}
}

func kgRunTickAllEntitiesNoAccess(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	id, _, _, err := s.UpsertEntity(ctx, "foo", "file", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}
	if err := s.TickAllEntities(ctx, nil); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	if err := s.TickAllEntities(ctx, nil); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}
	ent, err := s.GetEntity(ctx, id)
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if ent.TurnsSince != 2 {
		t.Errorf("turns_since = %d, want 2", ent.TurnsSince)
	}
	if ent.Retention >= 1.0 {
		t.Errorf("retention = %v, want < 1.0 after two ticks without access", ent.Retention)
	}
	if ent.Retention <= 0 {
		t.Errorf("retention = %v, want > 0", ent.Retention)
	}
}

func kgRunTickAllEntitiesAccessedReset(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	id, _, _, err := s.UpsertEntity(ctx, "bar", "file", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	// One tick without access -> turns_since=1.
	if err := s.TickAllEntities(ctx, nil); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	// Now tick with the entity in accessedIDs -> resets.
	if err := s.TickAllEntities(ctx, []string{id}); err != nil {
		t.Fatalf("Tick accessed: %v", err)
	}
	ent, err := s.GetEntity(ctx, id)
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if ent.TurnsSince != 0 {
		t.Errorf("turns_since = %d, want 0 after access", ent.TurnsSince)
	}
	if ent.Retention != 1.0 {
		t.Errorf("retention = %v, want 1.0 after access", ent.Retention)
	}
	if ent.LastAccess.IsZero() {
		t.Error("last_access not bumped on access")
	}
}

func kgRunAddEntityMentionsIdempotent(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	factID, err := s.InsertFact(ctx, testdataFacts()[0])
	if err != nil {
		t.Fatalf("InsertFact: %v", err)
	}
	eid, _, _, err := s.UpsertEntity(ctx, "foo", "file", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	n, err := s.AddEntityMentions(ctx, factID, []string{eid}, "subject")
	if err != nil {
		t.Fatalf("AddEntityMentions: %v", err)
	}
	if n != 1 {
		t.Errorf("first AddEntityMentions inserted = %d, want 1", n)
	}

	n, err = s.AddEntityMentions(ctx, factID, []string{eid}, "subject")
	if err != nil {
		t.Fatalf("AddEntityMentions repeat: %v", err)
	}
	if n != 0 {
		t.Errorf("repeat AddEntityMentions inserted = %d, want 0", n)
	}
}

func kgRunListMemoriesByEntity(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	factID, err := s.InsertFact(ctx, testdataFacts()[0])
	if err != nil {
		t.Fatalf("InsertFact: %v", err)
	}
	epID, err := s.InsertEpisode(ctx, testdataEpisodes()[0])
	if err != nil {
		t.Fatalf("InsertEpisode: %v", err)
	}
	eid, _, _, err := s.UpsertEntity(ctx, "auth", "file", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}
	if _, err := s.AddEntityMentions(ctx, factID, []string{eid}, ""); err != nil {
		t.Fatalf("AddEntityMentions fact: %v", err)
	}
	if _, err := s.AddEntityMentions(ctx, epID, []string{eid}, ""); err != nil {
		t.Fatalf("AddEntityMentions episode: %v", err)
	}

	refs, err := s.ListMemoriesByEntity(ctx, eid, 25)
	if err != nil {
		t.Fatalf("ListMemoriesByEntity: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("ListMemoriesByEntity returned %d, want 2", len(refs))
	}
	gotLayers := map[MemoryType]bool{}
	for _, r := range refs {
		gotLayers[r.Layer] = true
		if r.Content == "" {
			t.Errorf("ref %s: content empty", r.ID)
		}
		if len(r.Content) > 120 {
			t.Errorf("ref %s: content not truncated (len=%d)", r.ID, len(r.Content))
		}
	}
	if !gotLayers[TypeL2Semantic] || !gotLayers[TypeL3Episodic] {
		t.Errorf("layers seen = %v, want both l2 and l3", gotLayers)
	}
}

func kgRunListEntityNeighborsHops1(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	factID, err := s.InsertFact(ctx, testdataFacts()[0])
	if err != nil {
		t.Fatalf("InsertFact: %v", err)
	}
	// Two entities, one mention, one entity-entity edge.
	a, _, _, err := s.UpsertEntity(ctx, "alpha", "file", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("UpsertEntity a: %v", err)
	}
	b, _, _, err := s.UpsertEntity(ctx, "beta", "file", []float32{0, 1, 0})
	if err != nil {
		t.Fatalf("UpsertEntity b: %v", err)
	}
	if _, err := s.AddEntityMentions(ctx, factID, []string{a}, ""); err != nil {
		t.Fatalf("AddEntityMentions: %v", err)
	}
	if _, err := s.AddEdge(ctx, Edge{SrcID: a, DstID: b, EdgeType: "references"}); err != nil {
		t.Fatalf("AddEdge a->b: %v", err)
	}

	memories, entities, err := s.ListEntityNeighbors(ctx, a, 1)
	if err != nil {
		t.Fatalf("ListEntityNeighbors: %v", err)
	}
	if len(memories) != 1 {
		t.Errorf("memories = %d, want 1", len(memories))
	} else {
		if memories[0].ID != factID {
			t.Errorf("memory id = %q, want %q", memories[0].ID, factID)
		}
		if memories[0].Distance != 1 {
			t.Errorf("memory distance = %d, want 1", memories[0].Distance)
		}
	}
	if len(entities) != 1 {
		t.Errorf("entities = %d, want 1", len(entities))
	} else {
		if entities[0].ID != b {
			t.Errorf("neighbor entity id = %q, want %q", entities[0].ID, b)
		}
		if entities[0].Name != "beta" {
			t.Errorf("neighbor entity name = %q, want %q", entities[0].Name, "beta")
		}
	}
}

func kgRunListEntitiesByMemoryIDs(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	// Empty input → empty (non-nil) slice, no error.
	got, err := s.ListEntitiesByMemoryIDs(ctx, nil)
	if err != nil {
		t.Fatalf("ListEntitiesByMemoryIDs(nil): %v", err)
	}
	if got == nil {
		t.Error("nil input: returned nil slice, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("nil input: len=%d, want 0", len(got))
	}

	// Two facts, three entities, mixed mentions:
	//   F1 → {E1, E2}
	//   F2 → {E2, E3}    (E2 must dedupe in the result)
	f1, err := s.InsertFact(ctx, testdataFacts()[0])
	if err != nil {
		t.Fatalf("InsertFact f1: %v", err)
	}
	f2, err := s.InsertFact(ctx, testdataFacts()[1])
	if err != nil {
		t.Fatalf("InsertFact f2: %v", err)
	}
	e1, _, _, err := s.UpsertEntity(ctx, "e1", "file", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("UpsertEntity e1: %v", err)
	}
	e2, _, _, err := s.UpsertEntity(ctx, "e2", "file", []float32{0, 1, 0})
	if err != nil {
		t.Fatalf("UpsertEntity e2: %v", err)
	}
	e3, _, _, err := s.UpsertEntity(ctx, "e3", "file", []float32{0, 0, 1})
	if err != nil {
		t.Fatalf("UpsertEntity e3: %v", err)
	}
	if _, err := s.AddEntityMentions(ctx, f1, []string{e1, e2}, ""); err != nil {
		t.Fatalf("AddEntityMentions f1: %v", err)
	}
	if _, err := s.AddEntityMentions(ctx, f2, []string{e2, e3}, ""); err != nil {
		t.Fatalf("AddEntityMentions f2: %v", err)
	}

	got, err = s.ListEntitiesByMemoryIDs(ctx, []string{f1, f2})
	if err != nil {
		t.Fatalf("ListEntitiesByMemoryIDs: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len=%d, want 3 (E1, E2 deduped, E3): got=%v", len(got), got)
	}
	gotSet := map[string]bool{}
	for _, id := range got {
		gotSet[id] = true
	}
	for _, want := range []string{e1, e2, e3} {
		if !gotSet[want] {
			t.Errorf("missing entity id %q in result %v", want, got)
		}
	}
}

func kgRunCountEntitiesAndEdges(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	// Empty.
	if n, err := s.CountEntities(ctx); err != nil || n != 0 {
		t.Errorf("CountEntities empty: n=%d err=%v, want 0/nil", n, err)
	}
	if n, err := s.CountEdges(ctx); err != nil || n != 0 {
		t.Errorf("CountEdges empty: n=%d err=%v, want 0/nil", n, err)
	}

	// Seed.
	if _, _, _, err := s.UpsertEntity(ctx, "a", "file", []float32{1, 0, 0}); err != nil {
		t.Fatalf("UpsertEntity a: %v", err)
	}
	if _, _, _, err := s.UpsertEntity(ctx, "b", "file", []float32{0, 1, 0}); err != nil {
		t.Fatalf("UpsertEntity b: %v", err)
	}
	if _, err := s.AddEdge(ctx, Edge{SrcID: "x", DstID: "y", EdgeType: "refines"}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	if n, err := s.CountEntities(ctx); err != nil || n != 2 {
		t.Errorf("CountEntities seeded: n=%d err=%v, want 2/nil", n, err)
	}
	if n, err := s.CountEdges(ctx); err != nil || n != 1 {
		t.Errorf("CountEdges seeded: n=%d err=%v, want 1/nil", n, err)
	}
}

func kgRunCascadeOnDeleteFact(t *testing.T, newStore kgTestFactory) {
	s := newStore(t)
	ctx := context.Background()

	factID, err := s.InsertFact(ctx, testdataFacts()[0])
	if err != nil {
		t.Fatalf("InsertFact: %v", err)
	}
	eid, _, _, err := s.UpsertEntity(ctx, "auth", "file", []float32{1, 0, 0})
	if err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}
	if _, err := s.AddEntityMentions(ctx, factID, []string{eid}, ""); err != nil {
		t.Fatalf("AddEntityMentions: %v", err)
	}
	// Edge involving the fact (fact -> entity).
	if _, err := s.AddEdge(ctx, Edge{SrcID: factID, DstID: eid, EdgeType: "depends_on"}); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	// Pre-delete sanity.
	mems, err := s.ListMemoriesByEntity(ctx, eid, 10)
	if err != nil {
		t.Fatalf("pre ListMemoriesByEntity: %v", err)
	}
	if len(mems) != 1 {
		t.Fatalf("pre-delete mentions = %d, want 1", len(mems))
	}

	if err := s.DeleteFact(ctx, factID); err != nil {
		t.Fatalf("DeleteFact: %v", err)
	}

	// Mention should be gone.
	mems, err = s.ListMemoriesByEntity(ctx, eid, 10)
	if err != nil {
		t.Fatalf("post ListMemoriesByEntity: %v", err)
	}
	if len(mems) != 0 {
		t.Errorf("post-delete mentions = %d, want 0", len(mems))
	}
	// Edge from the fact should be gone.
	edges, err := s.ListEdges(ctx, factID, 1)
	if err != nil {
		t.Fatalf("ListEdges: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("post-delete edges = %d, want 0", len(edges))
	}
	// Entity itself must survive.
	ent, err := s.GetEntity(ctx, eid)
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if ent.ID == "" {
		t.Error("entity was cascade-deleted; should survive")
	}
}
