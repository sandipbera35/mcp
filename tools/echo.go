package tools

import (
	"context"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterEchoTool adds the echo tool to the MCP server
func RegisterEchoTool(s *server.MCPServer) {
	echoTool := mcp.NewTool("echo",
		mcp.WithDescription("Echoes the provided message back"),
		mcp.WithString("message", mcp.Required(), mcp.Description("The message to echo")),
	)

	s.AddTool(echoTool, EchoHandler)
}

func EchoHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	message, err := request.RequireString("message")
	if err != nil {
		return mcp.NewToolResultError("message argument is required"), nil
	}

	response := fmt.Sprintf("Echo: %s", message)
	return mcp.NewToolResultText(response), nil
}
