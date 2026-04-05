package tools

import (
	"github.com/mark3labs/mcp-go/server"
)

// RegisterAll binds all the custom tools to the provided MCP server
func RegisterAll(s *server.MCPServer) {
	RegisterFetchURLTool(s)
	RegisterReadFileTool(s)
}
