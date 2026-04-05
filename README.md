# Go MCP Server

A sophisticated, production-ready Model Context Protocol (MCP) server written in Go. This server communicates via Server-Sent Events (SSE) over an HTTP port and provides tools that AI Assistants can use dynamically.

## Features

- **SSE Transport Strategy:** Listens on an HTTP port instead of using standard input/output, which allows networking and avoids issues with terminal UI blocking.
- **`.env` Configuration:** Automatically loads variables such as system ports from an `.env` file configuration.
- **Built-in Tools:**
  - `fetch_url`: Downloads text and HTML directly from web sources.
  - `read_file`: Reads text files locally from your computer (safeguarded against large files).

## Prerequisites

- Go 1.20+ installed
- Create a `.env` file in the root directory (optional, defaults to `8080` if none exists):
  ```env
  PORT=8080
  ```

## Running the Server

Start the server using Go from your terminal:

```bash
go run .
```

If it starts successfully, you should see output output like this:
```
Starting MCP Server...
MCP Server initialized and listening on port 8080
Connect to SSE stream at http://localhost:8080/sse
```

## How to Connect an MCP Client

Because this server uses the SSE transport over HTTP instead of Stdio, you will configure your AI client differently. Instead of passing a command line executable path, you typically connect via URL.

### Connecting via Claude Desktop (or other compatible clients)

Not all clients natively support HTTP/SSE connection through simple UI config natively right away without an HTTP wrap (many default to Stdio). However, for applications or codebases integrating this:

1. **SSE Endpoint:** Point your MCP EventSource initialization to `http://localhost:8080/sse`
2. **Messages Endpoint:** The MCP Client will handle POST requests automatically to the server's message routes after connection. 

If you are developing a custom client, connect using standard SSE implementation plugins designed by Anthropic/Mark3Labs.

### Example SDK Client integration
If writing your own AI script using the MCP Go or TypeScript SDK:

```go
// Connect to the remote SSE server
client, err := mcpclient.NewSSEClient("http://localhost:8080/sse")
```

## Extending the Server

To add new tools, follow these steps:

1. Create a new file in the `tools/` package (e.g., `tools/my_tool.go`).
2. Define the tool using the `mcp.Tool` struct and create a handler function.
3. Register the tool in the `tools/tools.go` file.

### Example: Adding an Echo Tool

**1. Create `tools/echo.go`:**

```go
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

	s.AddTool(echoTool, echoHandler)
}

func echoHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	message, ok := request.Params.Arguments["message"].(string)
	if !ok {
		return mcp.NewToolResultError("message argument is required"), nil
	}

	response := fmt.Sprintf("Echo: %s", message)
	return mcp.NewToolResultText(response), nil
}
```

**2. Register in `tools/tools.go`:**

```go
package tools

import (
	"github.com/mark3labs/mcp-go/server"
)

// RegisterAll initializes and adds all custom tools to the provided MCP server.
func RegisterAll(s *server.MCPServer) {
	RegisterFetchUrlTool(s)
	RegisterReadFileTool(s)
	RegisterEchoTool(s) // <--- Add your new tool here
}
```
