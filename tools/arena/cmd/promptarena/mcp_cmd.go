package main

import (
	"github.com/spf13/cobra"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentmcp"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run an MCP server exposing PromptArena authoring knowledge over stdio",
	Long: `Start a stdio MCP server that exposes the PromptArena agent knowledge base —
authoring concepts, the example catalog, and version-locked JSON schemas — as MCP
tools and resources, so any MCP client can author kits.

Tools: explain, get_schema, list_examples, show_example.
Resources: promptarena://concepts/<id>, promptarena://schemas/<type>, promptarena://catalog.

Add to an MCP client config (e.g. Claude Code .mcp.json):
  { "mcpServers": { "promptarena": { "command": "promptarena", "args": ["mcp"] } } }`,
	RunE: runMCP,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCP(cmd *cobra.Command, _ []string) error {
	srv := agentmcp.NewServer(GetVersion())
	return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
}
