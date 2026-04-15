package tests

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandipbera35/mcp/vector"
)

func TestFileVectorStoreIngestAndSearch(t *testing.T) {
	store, err := vector.NewFileStore(vector.FileConfig{
		Path:       filepath.Join(t.TempDir(), "vector-store.json"),
		Collection: "test_collection",
		Dimension:  384,
	})
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}

	result, err := store.Ingest(context.Background(), vector.IngestInput{
		SourceType: "text",
		Source:     "unit-test",
		Title:      "Runbook",
		Tags:       []string{"ops"},
		Text:       "Rotate credentials before blue green deployment and verify rollback readiness.",
		ChunkSize:  500,
		Overlap:    50,
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if result.Chunks == 0 {
		t.Fatalf("expected chunks to be created")
	}

	results, err := store.Search(context.Background(), "blue green rollback", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results")
	}
	if !strings.Contains(results[0].ChunkText, "blue green") {
		t.Fatalf("unexpected top result: %s", results[0].ChunkText)
	}
}
