package mcp

import (
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools registers all floop MCP tools with the server.
func (s *Server) registerTools() error {
	// Register floop_active tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_active",
		Description: "Get active behaviors for the current context (file, task, environment)",
	}, s.handleFloopActive)

	// Register floop_learn tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_learn",
		Description: "Capture a correction and extract a reusable behavior",
	}, s.handleFloopLearn)

	// Register floop_list tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_list",
		Description: "List all behaviors or corrections",
	}, s.handleFloopList)

	// Register floop_deduplicate tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_deduplicate",
		Description: "Find and merge duplicate behaviors in the store",
	}, s.handleFloopDeduplicate)

	// Register floop_backup tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_backup",
		Description: "Export full graph state (nodes + edges) to a backup file",
	}, s.handleFloopBackup)

	// Register floop_restore tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_restore",
		Description: "Import graph state from a backup file (merge or replace)",
	}, s.handleFloopRestore)

	// Register floop_connect tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_connect",
		Description: "Create an edge between two behaviors for spreading activation",
	}, s.handleFloopConnect)

	// Register floop_validate tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_validate",
		Description: "Validate the behavior graph for consistency issues (dangling references, cycles, self-references)",
	}, s.handleFloopValidate)

	// Register floop_graph tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_graph",
		Description: "Render the behavior graph in DOT (Graphviz), JSON, or interactive HTML format for visualization",
	}, s.handleFloopGraph)

	// Register floop_feedback tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_feedback",
		Description: "Provide explicit feedback on a behavior: confirmed (helpful) or overridden (contradicted)",
	}, s.handleFloopFeedback)

	// Register floop_pack_install tool
	sdk.AddTool(s.server, &sdk.Tool{
		Name:        "floop_pack_install",
		Description: "Install a skill pack from a local path, URL, or GitHub shorthand (gh:owner/repo)",
	}, s.handleFloopPackInstall)

	return nil
}

// registerResources registers MCP resources for auto-loading into context.
func (s *Server) registerResources() error {
	// Register the active behaviors resource
	// This gets automatically loaded into Claude's context
	s.server.AddResource(&sdk.Resource{
		URI:         "floop://behaviors/active",
		Name:        "floop-active-behaviors",
		Description: "Patterns and suggestions from previous sessions that may be relevant to the current task.",
		MIMEType:    "text/markdown",
	}, s.handleBehaviorsResource)

	// Register expansion resource template for getting full behavior details
	s.server.AddResourceTemplate(&sdk.ResourceTemplate{
		URITemplate: "floop://behaviors/expand/{id}",
		Name:        "floop-behavior-expand",
		Description: "Get full details for a specific behavior. Use this when you need the complete content of a summarized behavior.",
		MIMEType:    "text/markdown",
	}, s.handleBehaviorExpandResource)

	return nil
}
