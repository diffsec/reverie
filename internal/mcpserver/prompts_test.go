package mcpserver

import (
	"context"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// promptText extracts the first text message from a GetPromptResult. Both
// prompts in this package emit a single TextContent message so the helper
// keeps the assertions compact.
func promptText(t *testing.T, r *mcpsdk.GetPromptResult) string {
	t.Helper()
	if r == nil {
		t.Fatal("nil GetPromptResult")
	}
	if len(r.Messages) == 0 {
		t.Fatal("no messages in prompt result")
	}
	tc, ok := r.Messages[0].Content.(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("first message content is %T, want *mcpsdk.TextContent", r.Messages[0].Content)
	}
	return tc.Text
}

func TestSessionStartPrompt_References_Init_Recall_L1Index(t *testing.T) {
	s := newTestServer(newStubEmbedder(4))
	defer s.recallCache.stop()

	req := &mcpsdk.GetPromptRequest{}
	req.Params = &mcpsdk.GetPromptParams{
		Arguments: map[string]string{
			"session_id":   "sess-test",
			"project_hint": "reverie",
		},
	}
	result, err := s.handleSessionStartPrompt(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionStartPrompt: %v", err)
	}

	text := promptText(t, result)

	// Phase 6d spec — the prompt text must walk the operator through the
	// four-step bootstrap. We check for the tool/resource names rather than
	// prose wording so future copy edits don't break the assertion.
	for _, want := range []string{
		"memory_session_init",
		"session_id",
		"sess-test",          // the literal argument echoed into the text
		"reverie://l1/index", // step 3
		"memory_recall",      // step 4
		"reverie",            // project_hint echoed into the query
	} {
		if !strings.Contains(text, want) {
			t.Errorf("session_start prompt missing %q; got:\n%s", want, text)
		}
	}
}

func TestSessionStartPrompt_NoSessionID_RendersPlaceholder(t *testing.T) {
	// Even without the argument supplied the rendered text must still
	// mention memory_session_init and session_id so operators reading the
	// prompt know the shape of the first call.
	s := newTestServer(newStubEmbedder(4))
	defer s.recallCache.stop()

	req := &mcpsdk.GetPromptRequest{}
	req.Params = &mcpsdk.GetPromptParams{Arguments: map[string]string{}}
	result, err := s.handleSessionStartPrompt(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionStartPrompt: %v", err)
	}
	text := promptText(t, result)

	for _, want := range []string{"memory_session_init", "session_id"} {
		if !strings.Contains(text, want) {
			t.Errorf("session_start prompt (no args) missing %q; got:\n%s", want, text)
		}
	}
}

func TestSessionEndPrompt_References_SessionEnd(t *testing.T) {
	s := newTestServer(newStubEmbedder(4))
	defer s.recallCache.stop()

	req := &mcpsdk.GetPromptRequest{}
	req.Params = &mcpsdk.GetPromptParams{
		Arguments: map[string]string{"session_id": "sess-end"},
	}
	result, err := s.handleSessionEndPrompt(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSessionEndPrompt: %v", err)
	}

	text := promptText(t, result)
	for _, want := range []string{
		"memory_session_end",
		"session_id",
		"sess-end",
		"episode",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("session_end prompt missing %q; got:\n%s", want, text)
		}
	}

	// Regression guard: after Phase 6d the prompt should no longer instruct
	// the operator to call memory_decay_tick directly — memory_session_end
	// owns the scoped tick.
	if strings.Contains(text, "memory_decay_tick") {
		t.Errorf("session_end prompt still references memory_decay_tick; Phase 6d moved the tick into memory_session_end. Got:\n%s", text)
	}
}
