package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type FileConfig struct {
	Path       string
	Collection string
	Dimension  int
}

type FileStore struct {
	path   string
	config FileConfig
	mu     sync.RWMutex
	data   filePersistedStore
}

type filePersistedStore struct {
	Collection string            `json:"collection"`
	Points     []fileVectorPoint `json:"points"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

type fileVectorPoint struct {
	ID          string    `json:"id"`
	DocumentID  string    `json:"document_id"`
	Document    string    `json:"document"`
	Source      string    `json:"source"`
	SourceType  string    `json:"source_type"`
	ChunkID     string    `json:"chunk_id"`
	ChunkText   string    `json:"chunk_text"`
	Tags        []string  `json:"tags"`
	ChunkNumber int       `json:"chunk_number"`
	Vector      []float32 `json:"vector"`
}

func NewFileStore(cfg FileConfig) (*FileStore, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("vector database file path is required")
	}
	if cfg.Collection == "" {
		cfg.Collection = "mcp_knowledge"
	}
	if cfg.Dimension <= 0 {
		cfg.Dimension = 384
	}

	store := &FileStore{
		path:   cfg.Path,
		config: cfg,
		data: filePersistedStore{
			Collection: cfg.Collection,
			Points:     []fileVectorPoint{},
		},
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o755); err != nil {
		return nil, fmt.Errorf("create file vector store directory: %w", err)
	}

	content, err := os.ReadFile(cfg.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, fmt.Errorf("read file vector store: %w", err)
	}
	if len(content) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(content, &store.data); err != nil {
		return nil, fmt.Errorf("parse file vector store: %w", err)
	}
	if store.data.Points == nil {
		store.data.Points = []fileVectorPoint{}
	}
	if store.data.Collection == "" {
		store.data.Collection = cfg.Collection
	}
	return store, nil
}

func (f *FileStore) EnsureCollection(ctx context.Context) error {
	_ = ctx
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data.Collection == "" {
		f.data.Collection = f.config.Collection
	}
	return f.persistLocked()
}

func (f *FileStore) Ingest(ctx context.Context, input IngestInput) (IngestResult, error) {
	_ = ctx
	if err := f.EnsureCollection(context.Background()); err != nil {
		return IngestResult{}, err
	}

	documentID := newID("doc")
	title := fallbackTitle(input.Title, input.Source)
	chunks := buildChunks(documentID, input.Text, input.ChunkSize, input.Overlap)
	points := make([]fileVectorPoint, 0, len(chunks))
	for _, chunk := range chunks {
		points = append(points, fileVectorPoint{
			ID:          chunk.ID,
			DocumentID:  documentID,
			Document:    title,
			Source:      input.Source,
			SourceType:  input.SourceType,
			ChunkID:     chunk.ID,
			ChunkText:   chunk.Text,
			Tags:        append([]string(nil), input.Tags...),
			ChunkNumber: chunk.Position,
			Vector:      EmbedText(chunk.Text, f.config.Dimension),
		})
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.data.Points = append(f.data.Points, points...)
	f.data.UpdatedAt = time.Now().UTC()
	if err := f.persistLocked(); err != nil {
		return IngestResult{}, err
	}

	return IngestResult{
		DocumentID: documentID,
		Title:      title,
		Source:     input.Source,
		Chunks:     len(chunks),
	}, nil
}

func (f *FileStore) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	_ = ctx
	if limit <= 0 {
		limit = 5
	}

	queryVector := EmbedText(query, f.config.Dimension)

	f.mu.RLock()
	defer f.mu.RUnlock()

	results := make([]SearchResult, 0, len(f.data.Points))
	for _, point := range f.data.Points {
		score := cosineSimilarity(queryVector, point.Vector)
		if score <= 0 {
			continue
		}
		results = append(results, SearchResult{
			DocumentID:  point.DocumentID,
			Document:    point.Document,
			Source:      point.Source,
			ChunkID:     point.ChunkID,
			ChunkText:   point.ChunkText,
			Tags:        append([]string(nil), point.Tags...),
			Score:       score,
			ChunkNumber: point.ChunkNumber,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			if results[i].Document == results[j].Document {
				return results[i].ChunkNumber < results[j].ChunkNumber
			}
			return results[i].Document < results[j].Document
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (f *FileStore) Health(ctx context.Context) error {
	_ = ctx
	f.mu.RLock()
	defer f.mu.RUnlock()
	return nil
}

func (f *FileStore) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return fmt.Errorf("create vector store directory: %w", err)
	}
	content, err := json.MarshalIndent(f.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal vector store: %w", err)
	}
	if err := os.WriteFile(f.path, content, 0o644); err != nil {
		return fmt.Errorf("write vector store: %w", err)
	}
	return nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	var normA float64
	var normB float64
	for i := range a {
		dot += float64(a[i] * b[i])
		normA += float64(a[i] * a[i])
		normB += float64(b[i] * b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
