package vector

import "context"

type Store interface {
	EnsureCollection(ctx context.Context) error
	Ingest(ctx context.Context, input IngestInput) (IngestResult, error)
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
	Health(ctx context.Context) error
}

type IngestInput struct {
	SourceType string
	Source     string
	Title      string
	Tags       []string
	Text       string
	ChunkSize  int
	Overlap    int
}

type IngestResult struct {
	DocumentID string
	Title      string
	Source     string
	Chunks     int
}

type SearchResult struct {
	DocumentID  string
	Document    string
	Source      string
	ChunkID     string
	ChunkText   string
	Tags        []string
	Score       float64
	ChunkNumber int
}
