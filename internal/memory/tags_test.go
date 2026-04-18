package memory

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// --- normalizeTags unit tests ---

func TestNormalizeTags_Empty(t *testing.T) {
	got, err := normalizeTags(nil)
	if err != nil {
		t.Fatalf("nil: %v", err)
	}
	if got == nil {
		t.Fatal("nil input should return non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}

	got, err = normalizeTags([]string{})
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("empty slice: got %v", got)
	}
}

func TestNormalizeTags_CanonicalForm(t *testing.T) {
	in := []string{"Zebra", "alpha", "  beta  ", "alpha", "ALPHA", "", "gamma"}
	want := []string{"alpha", "beta", "gamma", "zebra"}
	got, err := normalizeTags(in)
	if err != nil {
		t.Fatalf("normalizeTags: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNormalizeTags_StripsEmptyAfterTrim(t *testing.T) {
	got, err := normalizeTags([]string{"", "   ", "\t"})
	if err != nil {
		t.Fatalf("normalizeTags: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("all-whitespace input should normalize to empty, got %v", got)
	}
}

func TestNormalizeTags_RejectsTooMany(t *testing.T) {
	in := make([]string, 17)
	for i := range in {
		in[i] = string(rune('a' + i))
	}
	_, err := normalizeTags(in)
	if err == nil {
		t.Fatal("expected error for 17 tags")
	}
	if !strings.Contains(err.Error(), "too many tags") {
		t.Errorf("error = %q, want contains 'too many tags'", err)
	}
}

func TestNormalizeTags_AcceptsSixteen(t *testing.T) {
	in := make([]string, 16)
	for i := range in {
		in[i] = string(rune('a' + i))
	}
	got, err := normalizeTags(in)
	if err != nil {
		t.Fatalf("16 tags should be accepted: %v", err)
	}
	if len(got) != 16 {
		t.Errorf("len = %d, want 16", len(got))
	}
}

func TestNormalizeTags_RejectsLongTag(t *testing.T) {
	long := strings.Repeat("x", 33)
	_, err := normalizeTags([]string{"ok", long})
	if err == nil {
		t.Fatal("expected error for 33-char tag")
	}
	if !strings.Contains(err.Error(), "exceeds max length") {
		t.Errorf("error = %q, want contains 'exceeds max length'", err)
	}
}

func TestNormalizeTags_Accepts32Chars(t *testing.T) {
	tag := strings.Repeat("x", 32)
	got, err := normalizeTags([]string{tag})
	if err != nil {
		t.Fatalf("32-char tag should be accepted: %v", err)
	}
	if len(got) != 1 || got[0] != tag {
		t.Errorf("got %v, want [%s]", got, tag)
	}
}

// --- Fact tag round-trip (sqliteStore) ---

func TestSQLiteInsertFact_TagsRoundTrip(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	f := testdataFacts()[0]
	f.Tags = []string{"Go", "sqlite", "go"} // mixed case, dup
	id, err := s.InsertFact(ctx, f)
	if err != nil {
		t.Fatalf("InsertFact: %v", err)
	}

	got, err := s.GetFact(ctx, id)
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	if got == nil {
		t.Fatal("GetFact returned nil")
	}
	want := []string{"go", "sqlite"}
	if !reflect.DeepEqual(got.Tags, want) {
		t.Errorf("Tags = %v, want %v", got.Tags, want)
	}
}

func TestSQLiteInsertFact_NoTagsReadsAsEmptySlice(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	f := testdataFacts()[0]
	f.Tags = nil
	id, err := s.InsertFact(ctx, f)
	if err != nil {
		t.Fatalf("InsertFact: %v", err)
	}
	got, err := s.GetFact(ctx, id)
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	if got.Tags == nil {
		t.Error("Tags should be non-nil empty slice, got nil")
	}
	if len(got.Tags) != 0 {
		t.Errorf("Tags len = %d, want 0", len(got.Tags))
	}
}

func TestSQLiteInsertFact_Rejects17Tags(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	f := testdataFacts()[0]
	f.Tags = make([]string, 17)
	for i := range f.Tags {
		f.Tags[i] = string(rune('a' + i))
	}
	_, err := s.InsertFact(ctx, f)
	if err == nil {
		t.Fatal("InsertFact should reject 17 tags")
	}
}

func TestSQLiteInsertFact_Rejects33CharTag(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	f := testdataFacts()[0]
	f.Tags = []string{strings.Repeat("x", 33)}
	_, err := s.InsertFact(ctx, f)
	if err == nil {
		t.Fatal("InsertFact should reject 33-char tag")
	}
}

func TestSQLiteListFacts_TagsAnyFilter(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	facts := testdataFacts()
	facts[0].Tags = []string{"foo"}
	facts[1].Tags = []string{"bar"}
	facts[2].Tags = []string{"foo", "baz"}

	for _, f := range facts {
		if _, err := s.InsertFact(ctx, f); err != nil {
			t.Fatalf("InsertFact: %v", err)
		}
	}

	got, err := s.ListFacts(ctx, ListFilter{TagsAny: []string{"foo"}})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("tags_any=[foo] returned %d, want 2", len(got))
	}
	for _, f := range got {
		has := false
		for _, tag := range f.Tags {
			if tag == "foo" {
				has = true
			}
		}
		if !has {
			t.Errorf("result %q lacks 'foo' tag: %v", f.Content, f.Tags)
		}
	}

	// Filter matching nothing.
	none, err := s.ListFacts(ctx, ListFilter{TagsAny: []string{"unknown"}})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("tags_any=[unknown] returned %d, want 0", len(none))
	}

	// Empty slice = no filter: all three facts.
	all, err := s.ListFacts(ctx, ListFilter{TagsAny: []string{}})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("tags_any=[] returned %d, want 3", len(all))
	}

	// Case-insensitive match.
	upper, err := s.ListFacts(ctx, ListFilter{TagsAny: []string{"FOO"}})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(upper) != 2 {
		t.Errorf("tags_any=[FOO] returned %d, want 2", len(upper))
	}
}

// --- Episode tag round-trip (sqliteStore) ---

func TestSQLiteInsertEpisode_TagsRoundTrip(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	ep := testdataEpisodes()[0]
	ep.Tags = []string{"learning", "refactor", "Learning"}
	id, err := s.InsertEpisode(ctx, ep)
	if err != nil {
		t.Fatalf("InsertEpisode: %v", err)
	}
	got, err := s.GetEpisode(ctx, id)
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	want := []string{"learning", "refactor"}
	if !reflect.DeepEqual(got.Tags, want) {
		t.Errorf("Tags = %v, want %v", got.Tags, want)
	}
}

func TestSQLiteInsertEpisode_NoTagsReadsAsEmptySlice(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	ep := testdataEpisodes()[0]
	ep.Tags = nil
	id, err := s.InsertEpisode(ctx, ep)
	if err != nil {
		t.Fatalf("InsertEpisode: %v", err)
	}
	got, err := s.GetEpisode(ctx, id)
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if got.Tags == nil {
		t.Error("Tags should be non-nil empty slice, got nil")
	}
	if len(got.Tags) != 0 {
		t.Errorf("Tags len = %d, want 0", len(got.Tags))
	}
}

func TestSQLiteInsertEpisode_Rejects17Tags(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	ep := testdataEpisodes()[0]
	ep.Tags = make([]string, 17)
	for i := range ep.Tags {
		ep.Tags[i] = string(rune('a' + i))
	}
	_, err := s.InsertEpisode(ctx, ep)
	if err == nil {
		t.Fatal("InsertEpisode should reject 17 tags")
	}
}

func TestSQLiteInsertEpisode_Rejects33CharTag(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	ep := testdataEpisodes()[0]
	ep.Tags = []string{strings.Repeat("y", 33)}
	_, err := s.InsertEpisode(ctx, ep)
	if err == nil {
		t.Fatal("InsertEpisode should reject 33-char tag")
	}
}

func TestSQLiteListEpisodes_TagsAnyFilter(t *testing.T) {
	s := openTestDB(t)
	ctx := context.Background()

	eps := testdataEpisodes()
	eps[0].Tags = []string{"deploy"}
	eps[1].Tags = []string{"refactor"}
	eps[2].Tags = []string{"deploy", "test"}
	for _, ep := range eps {
		if _, err := s.InsertEpisode(ctx, ep); err != nil {
			t.Fatalf("InsertEpisode: %v", err)
		}
	}

	got, err := s.ListEpisodes(ctx, ListFilter{TagsAny: []string{"deploy"}})
	if err != nil {
		t.Fatalf("ListEpisodes: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("tags_any=[deploy] returned %d, want 2", len(got))
	}
}

// --- Mem store mirror tests ---

func TestMemInsertFact_TagsRoundTrip(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	f := testdataFacts()[0]
	f.Tags = []string{"Go", "sqlite", "go"}
	id, err := s.InsertFact(ctx, f)
	if err != nil {
		t.Fatalf("InsertFact: %v", err)
	}
	got, err := s.GetFact(ctx, id)
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	want := []string{"go", "sqlite"}
	if !reflect.DeepEqual(got.Tags, want) {
		t.Errorf("Tags = %v, want %v", got.Tags, want)
	}
}

func TestMemInsertFact_NoTagsReadsAsEmptySlice(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	f := testdataFacts()[0]
	f.Tags = nil
	id, err := s.InsertFact(ctx, f)
	if err != nil {
		t.Fatalf("InsertFact: %v", err)
	}
	got, err := s.GetFact(ctx, id)
	if err != nil {
		t.Fatalf("GetFact: %v", err)
	}
	if got.Tags == nil {
		t.Error("Tags should be non-nil empty slice")
	}
	if len(got.Tags) != 0 {
		t.Errorf("Tags len = %d, want 0", len(got.Tags))
	}
}

func TestMemInsertFact_Rejects17Tags(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	f := testdataFacts()[0]
	f.Tags = make([]string, 17)
	for i := range f.Tags {
		f.Tags[i] = string(rune('a' + i))
	}
	_, err := s.InsertFact(ctx, f)
	if err == nil {
		t.Fatal("InsertFact should reject 17 tags")
	}
}

func TestMemInsertFact_Rejects33CharTag(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	f := testdataFacts()[0]
	f.Tags = []string{strings.Repeat("z", 33)}
	_, err := s.InsertFact(ctx, f)
	if err == nil {
		t.Fatal("InsertFact should reject 33-char tag")
	}
}

func TestMemListFacts_TagsAnyFilter(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	facts := testdataFacts()
	facts[0].Tags = []string{"foo"}
	facts[1].Tags = []string{"bar"}
	facts[2].Tags = []string{"foo", "baz"}
	for _, f := range facts {
		if _, err := s.InsertFact(ctx, f); err != nil {
			t.Fatalf("InsertFact: %v", err)
		}
	}

	got, err := s.ListFacts(ctx, ListFilter{TagsAny: []string{"foo"}})
	if err != nil {
		t.Fatalf("ListFacts: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("tags_any=[foo] returned %d, want 2", len(got))
	}
}

func TestMemInsertEpisode_TagsRoundTrip(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	ep := testdataEpisodes()[0]
	ep.Tags = []string{"Refactor", "test", "refactor"}
	id, err := s.InsertEpisode(ctx, ep)
	if err != nil {
		t.Fatalf("InsertEpisode: %v", err)
	}
	got, err := s.GetEpisode(ctx, id)
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	want := []string{"refactor", "test"}
	if !reflect.DeepEqual(got.Tags, want) {
		t.Errorf("Tags = %v, want %v", got.Tags, want)
	}
}

func TestMemInsertEpisode_NoTagsReadsAsEmptySlice(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	ep := testdataEpisodes()[0]
	ep.Tags = nil
	id, err := s.InsertEpisode(ctx, ep)
	if err != nil {
		t.Fatalf("InsertEpisode: %v", err)
	}
	got, err := s.GetEpisode(ctx, id)
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if got.Tags == nil || len(got.Tags) != 0 {
		t.Errorf("Tags = %v, want empty non-nil", got.Tags)
	}
}

func TestMemInsertEpisode_Rejects17Tags(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	ep := testdataEpisodes()[0]
	ep.Tags = make([]string, 17)
	for i := range ep.Tags {
		ep.Tags[i] = string(rune('a' + i))
	}
	_, err := s.InsertEpisode(ctx, ep)
	if err == nil {
		t.Fatal("InsertEpisode should reject 17 tags")
	}
}

func TestMemInsertEpisode_Rejects33CharTag(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	ep := testdataEpisodes()[0]
	ep.Tags = []string{strings.Repeat("q", 33)}
	_, err := s.InsertEpisode(ctx, ep)
	if err == nil {
		t.Fatal("InsertEpisode should reject 33-char tag")
	}
}

func TestMemListEpisodes_TagsAnyFilter(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	eps := testdataEpisodes()
	eps[0].Tags = []string{"deploy"}
	eps[1].Tags = []string{"refactor"}
	eps[2].Tags = []string{"deploy", "test"}
	for _, ep := range eps {
		if _, err := s.InsertEpisode(ctx, ep); err != nil {
			t.Fatalf("InsertEpisode: %v", err)
		}
	}
	got, err := s.ListEpisodes(ctx, ListFilter{TagsAny: []string{"deploy"}})
	if err != nil {
		t.Fatalf("ListEpisodes: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("tags_any=[deploy] returned %d, want 2", len(got))
	}
}
