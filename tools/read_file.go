package tools

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterReadFileTool creates and registers the read_file tool
func RegisterReadFileTool(s *server.MCPServer) {
	tool := mcp.NewTool("read_file",
		mcp.WithDescription("Reads the text content of a local file."),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("The absolute or relative path to the file to read (e.g., ./data.txt or /var/log/syslog)"),
		),
	)

	s.AddTool(tool, readFileHandler)
}

func readFileHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil || path == "" {
		return mcp.NewToolResultError("the 'path' parameter is required and must be a non-empty string"), nil
	}

	log.Printf("Reading file: %s", path)

	fileInfo, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return mcp.NewToolResultError(fmt.Sprintf("File not found: %s", path)), nil
		}
		return mcp.NewToolResultError(fmt.Sprintf("Error checking file stats: %v", err)), nil
	}

	if fileInfo.IsDir() {
		return mcp.NewToolResultError(fmt.Sprintf("The path %s is a directory, not a file", path)), nil
	}

	// We'll read the entire file. For extremely large files, this isn't safe, but for
	// generic LLM context retrieval this usually suffices. You might add a size limit in production.
	if fileInfo.Size() > 5*1024*1024 { // 5 MB limit
		return mcp.NewToolResultError("File is too large to read (exceeds 5MB limit)"), nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		log.Printf("Error reading file: %v", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to read file: %v", err)), nil
	}

	resultText := string(content)
	return mcp.NewToolResultText(resultText), nil
}
