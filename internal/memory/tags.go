package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Tag validation bounds. Shared by sqliteStore and memStore so the two
// back-ends agree on what "normalized" means at write time.
const (
	maxTagsPerMemory = 16
	maxTagLength     = 32
)

// normalizeTags lowercases, trims, strips empties, dedupes, and sorts the
// input tags in-place (semantically — a fresh slice is returned). It returns
// an error when the normalized slice exceeds maxTagsPerMemory or any single
// tag exceeds maxTagLength. A nil/empty input yields an empty, non-nil slice
// so callers can round-trip tags as `[]` in JSON rather than `null`.
func normalizeTags(in []string) ([]string, error) {
	if len(in) == 0 {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		t := strings.ToLower(strings.TrimSpace(raw))
		if t == "" {
			continue
		}
		if len(t) > maxTagLength {
			return nil, fmt.Errorf("tag %q exceeds max length %d", t, maxTagLength)
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	if len(out) > maxTagsPerMemory {
		return nil, fmt.Errorf("too many tags: got %d, max %d", len(out), maxTagsPerMemory)
	}
	sort.Strings(out)
	return out, nil
}

// encodeTags renders a tags slice as the JSON TEXT stored in SQLite. The
// zero-value and nil cases both render as "[]".
func encodeTags(tags []string) (string, error) {
	if len(tags) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "", fmt.Errorf("encode tags: %w", err)
	}
	return string(b), nil
}

// decodeTags parses a tags TEXT column back into a slice. Empty, NULL, or
// malformed-but-empty values return a non-nil empty slice so handlers can
// serialize tags as `[]` rather than `null`.
func decodeTags(raw string) ([]string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return []string{}, nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(s), &tags); err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

// tagMatchesAny reports whether have contains at least one tag that appears
// (case-insensitively, trimmed) in want. An empty want returns true (no filter).
func tagMatchesAny(have, want []string) bool {
	if len(want) == 0 {
		return true
	}
	if len(have) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(have))
	for _, t := range have {
		set[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	for _, w := range want {
		k := strings.ToLower(strings.TrimSpace(w))
		if k == "" {
			continue
		}
		if _, ok := set[k]; ok {
			return true
		}
	}
	return false
}
