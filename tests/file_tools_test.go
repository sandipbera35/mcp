package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sandipbera35/mcp/knowledge"
	"github.com/sandipbera35/mcp/tools"
)

func TestWriteFileHandler(t *testing.T) {
	cfg := testConfig(t)
	root := t.TempDir()
	cfg.ReadRoot = root

	store, err := knowledge.NewStore(filepath.Join(t.TempDir(), "knowledge.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handlers := tools.NewHandlers(cfg, store, newFakeVectorStore())

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "write_file",
			Arguments: map[string]any{
				"path":    "pkg/example.txt",
				"content": "hello from write_file",
			},
		},
	}

	result, err := handlers.WriteFileHandler(context.Background(), request)
	if err != nil {
		t.Fatalf("writeFileHandler returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("result marked as error")
	}

	content, err := os.ReadFile(filepath.Join(root, "pkg/example.txt"))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(content) != "hello from write_file" {
		t.Fatalf("unexpected written content: %s", string(content))
	}
}

func TestEditFileHandlerReplaceAndAppend(t *testing.T) {
	cfg := testConfig(t)
	root := t.TempDir()
	cfg.ReadRoot = root

	store, err := knowledge.NewStore(filepath.Join(t.TempDir(), "knowledge.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handlers := tools.NewHandlers(cfg, store, newFakeVectorStore())

	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	replaceRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "edit_file",
			Arguments: map[string]any{
				"path":      "main.go",
				"operation": "replace",
				"old_text":  "func main() {}",
				"new_text":  "func main() {\n\tprintln(\"hello\")\n}",
			},
		},
	}

	result, err := handlers.EditFileHandler(context.Background(), replaceRequest)
	if err != nil {
		t.Fatalf("editFileHandler replace returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("replace result marked as error")
	}

	appendRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "edit_file",
			Arguments: map[string]any{
				"path":      "main.go",
				"operation": "append",
				"new_text":  "\n// done\n",
			},
		},
	}

	result, err = handlers.EditFileHandler(context.Background(), appendRequest)
	if err != nil {
		t.Fatalf("editFileHandler append returned error: %v", err)
	}
	if result.IsError {
		t.Fatalf("append result marked as error")
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read edited file: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, `println("hello")`) {
		t.Fatalf("expected replaced content, got: %s", text)
	}
	if !strings.Contains(text, "// done") {
		t.Fatalf("expected appended content, got: %s", text)
	}
}
