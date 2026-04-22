package mcpserver

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerPrompts adds all prompt definitions to the SDK server.
func (s *Server) registerPrompts(srv *mcpsdk.Server) {
	srv.AddPrompt(&mcpsdk.Prompt{
		Name:        "session_start",
		Description: "Bootstrap memory context for a new session. Initialize the session, read the L1 cluster index, and recall relevant memories with the buffer auto-updating.",
		Arguments: []*mcpsdk.PromptArgument{
			{
				Name:        "session_id",
				Description: "Stable client-generated session identifier (e.g., project name + date). Required to persist the working-memory buffer across tool calls.",
				Required:    true,
			},
			{
				Name:        "project_hint",
				Description: "Project name or path to scope the initial recall query.",
				Required:    false,
			},
		},
	}, s.handleSessionStartPrompt)

	srv.AddPrompt(&mcpsdk.Prompt{
		Name:        "session_end",
		Description: "Wrap up a session: close the session (which runs a scoped decay tick) and optionally write an L3 episode summarizing significant work.",
		Arguments: []*mcpsdk.PromptArgument{
			{
				Name:        "session_id",
				Description: "The session_id passed to memory_session_init at the start of the session.",
				Required:    true,
			},
		},
	}, s.handleSessionEndPrompt)
}

func (s *Server) handleSessionStartPrompt(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	var hint, sessionID string
	if req != nil && req.Params != nil {
		hint = req.Params.Arguments["project_hint"]
		sessionID = req.Params.Arguments["session_id"]
	}

	query := "recent context and user preferences"
	if hint != "" {
		query = hint + " project context, conventions, and recent decisions"
	}

	sessionIDLiteral := sessionID
	if sessionIDLiteral == "" {
		// No session_id argument supplied by the caller. Leave a placeholder
		// so the rendered text still walks through the correct flow; the
		// operator substitutes their own stable identifier.
		sessionIDLiteral = "<session_id>"
	}

	text := `Memory bootstrap for this session.

1. Call memory_session_init with session_id="` + sessionIDLiteral + `" (and optional project_hint / tags). This creates a new session or resumes an existing one.
2. If the returned Created=false, inspect the returned buffer — it holds prior session context (prior recalls, reinforced memories, recent writes). Treat it as already-loaded working memory.
3. Read the reverie://l1/index resource to see the cluster landscape.
4. Call memory_recall with session_id="` + sessionIDLiteral + `" and query: "` + query + `". Passing the session_id makes the buffer auto-update with the returned candidates; future reinforce/write calls that also pass session_id keep the buffer in sync.

Clusters with high utility are your strongest context — prioritize them.`

	return &mcpsdk.GetPromptResult{
		Description: "Session start recall",
		Messages: []*mcpsdk.PromptMessage{
			{
				Role:    "user",
				Content: &mcpsdk.TextContent{Text: text},
			},
		},
	}, nil
}

func (s *Server) handleSessionEndPrompt(_ context.Context, req *mcpsdk.GetPromptRequest) (*mcpsdk.GetPromptResult, error) {
	var sessionID string
	if req != nil && req.Params != nil {
		sessionID = req.Params.Arguments["session_id"]
	}
	sessionIDLiteral := sessionID
	if sessionIDLiteral == "" {
		sessionIDLiteral = "<session_id>"
	}

	text := `Session wrap-up.

Call ` + "`memory_session_end`" + ` with the session_id="` + sessionIDLiteral + `". Optionally provide an ` + "`episode`" + ` payload summarizing the session:
  - situation: what triggered the work
  - action: what was done
  - outcome: what happened as a result
  - preemptive: actionable lesson for next time

memory_session_end runs a scoped decay tick (bumping only clusters touched this session), links the optional episode to fact IDs currently in the buffer, and marks the session closed. Skip the episode payload for trivial sessions (quick question, no lasting decisions).`

	return &mcpsdk.GetPromptResult{
		Description: "Session end consolidation",
		Messages: []*mcpsdk.PromptMessage{
			{
				Role:    "user",
				Content: &mcpsdk.TextContent{Text: text},
			},
		},
	}, nil
}
