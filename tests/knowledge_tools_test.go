package tests

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/sandipbera35/mcp/knowledge"
	"github.com/sandipbera35/mcp/tools"
	"github.com/sandipbera35/mcp/vector"
)

func TestKnowledgeIngestAndSearch(t *testing.T) {
	cfg := testConfig(t)
	store, err := knowledge.NewStore(filepath.Join(t.TempDir(), "knowledge.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handlers := tools.NewHandlers(cfg, store, newFakeVectorStore())

	ingestRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "ingest_knowledge",
			Arguments: map[string]any{
				"source_type": "text",
				"title":       "Runbook",
				"content":     "The deployment runbook says to rotate credentials before enabling blue green rollout.",
				"tags":        []any{"ops", "deploy"},
			},
		},
	}

	ingestResult, err := handlers.IngestKnowledgeHandler(context.Background(), ingestRequest)
	if err != nil {
		t.Fatalf("ingest handler error: %v", err)
	}
	if ingestResult.IsError {
		t.Fatalf("ingest result marked as error")
	}

	searchRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "search_knowledge",
			Arguments: map[string]any{
				"query": "blue green rollout credentials",
			},
		},
	}

	searchResult, err := handlers.SearchKnowledgeHandler(context.Background(), searchRequest)
	if err != nil {
		t.Fatalf("search handler error: %v", err)
	}
	if searchResult.IsError {
		t.Fatalf("search result marked as error")
	}

	textContent := searchResult.Content[0].(mcp.TextContent)
	if !strings.Contains(textContent.Text, "Runbook") {
		t.Fatalf("expected runbook result, got: %s", textContent.Text)
	}
	if !strings.Contains(textContent.Text, "blue green rollout") {
		t.Fatalf("expected relevant chunk text, got: %s", textContent.Text)
	}
}

func TestCacheContextRoundTrip(t *testing.T) {
	cfg := testConfig(t)
	store, err := knowledge.NewStore(filepath.Join(t.TempDir(), "knowledge.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	handlers := tools.NewHandlers(cfg, store, newFakeVectorStore())

	cacheRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "cache_context",
			Arguments: map[string]any{
				"key":     "customer-alpha",
				"title":   "Customer Alpha",
				"content": "Alpha uses the premium tier and requires 99.9% SLA language in every draft.",
			},
		},
	}

	cacheResult, err := handlers.CacheContextHandler(context.Background(), cacheRequest)
	if err != nil {
		t.Fatalf("cache handler error: %v", err)
	}
	if cacheResult.IsError {
		t.Fatalf("cache result marked as error")
	}

	getRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "get_cached_context",
			Arguments: map[string]any{
				"key": "customer-alpha",
			},
		},
	}

	getResult, err := handlers.GetCachedContextHandler(context.Background(), getRequest)
	if err != nil {
		t.Fatalf("get cached context handler error: %v", err)
	}
	if getResult.IsError {
		t.Fatalf("get cached context result marked as error")
	}

	textContent := getResult.Content[0].(mcp.TextContent)
	if !strings.Contains(textContent.Text, "premium tier") {
		t.Fatalf("expected cached content, got: %s", textContent.Text)
	}
}

type fakeVectorStore struct {
	docs []vector.IngestInput
}

func newFakeVectorStore() *fakeVectorStore {
	return &fakeVectorStore{}
}

func (f *fakeVectorStore) EnsureCollection(ctx context.Context) error {
	return nil
}

func (f *fakeVectorStore) Ingest(ctx context.Context, input vector.IngestInput) (vector.IngestResult, error) {
	f.docs = append(f.docs, input)
	return vector.IngestResult{
		DocumentID: "doc-1",
		Title:      input.Title,
		Source:     input.Source,
		Chunks:     1,
	}, nil
}

func (f *fakeVectorStore) Search(ctx context.Context, query string, limit int) ([]vector.SearchResult, error) {
	if len(f.docs) == 0 {
		return nil, nil
	}
	doc := f.docs[0]
	return []vector.SearchResult{{
		DocumentID:  "doc-1",
		Document:    doc.Title,
		Source:      doc.Source,
		ChunkID:     "chunk-1",
		ChunkText:   doc.Text,
		Tags:        doc.Tags,
		Score:       0.99,
		ChunkNumber: 0,
	}}, nil
}

func (f *fakeVectorStore) Health(ctx context.Context) error {
	return nil
}
