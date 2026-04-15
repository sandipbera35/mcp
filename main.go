package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sandipbera35/mcp/config"
	"github.com/sandipbera35/mcp/knowledge"
	"github.com/sandipbera35/mcp/tools"
	"github.com/sandipbera35/mcp/vector"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lshortfile)

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading it. Using environment variables.")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	store, err := knowledge.NewStore(cfg.KnowledgeStore)
	if err != nil {
		log.Fatalf("initialize knowledge store: %v", err)
	}

	vectorStore, err := buildVectorStore(cfg)
	if err != nil {
		log.Fatalf("initialize vector database client: %v", err)
	}
	if err := vectorStore.EnsureCollection(context.Background()); err != nil {
		log.Fatalf("ensure vector collection: %v", err)
	}

	mcpServer := server.NewMCPServer(
		cfg.ServerName,
		cfg.ServerVersion,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
		server.WithLogging(),
		server.WithRecovery(),
		server.WithInstructions(`You are connected to a production-ready MCP service. Prefer cached context for repeatable tasks, use search_knowledge for RAG retrieval, ingest_knowledge to expand the corpus, and standard web/file tools only when needed.`),
	)

	handlers := tools.NewHandlers(cfg, store, vectorStore)
	tools.RegisterAll(mcpServer, handlers)

	if cfg.Transport == "stdio" {
		log.Printf("starting MCP server in stdio mode")
		stdioServer := server.NewStdioServer(mcpServer)
		if err := stdioServer.Listen(context.Background(), os.Stdin, os.Stdout); err != nil {
			log.Fatalf("stdio server error: %v", err)
		}
		return
	}

	sseServer := server.NewSSEServer(mcpServer)
	mux := http.NewServeMux()
	basePath := cfg.BasePath
	mux.Handle(basePath+"/sse", sseServer.SSEHandler())
	mux.Handle(basePath+"/message", sseServer.MessageHandler())
	mux.HandleFunc(basePath+"/healthz", healthHandler(cfg))
	mux.HandleFunc(basePath+"/readyz", readyHandler(cfg, store, vectorStore))
	mux.HandleFunc(basePath+"/", rootHandler(cfg))

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown error: %v", err)
		}
	}()

	log.Printf("MCP server %s v%s listening on %s", cfg.ServerName, cfg.ServerVersion, cfg.ListenAddr())
	log.Printf("SSE endpoint: %s", publicURL(cfg, "/sse"))
	log.Printf("Message endpoint: %s", publicURL(cfg, "/message"))
	log.Printf("Knowledge store: %s", cfg.KnowledgeStore)
	log.Printf("Vector database: provider=%s target=%s collection=%s", cfg.VectorDBProvider, vectorTarget(cfg), cfg.VectorCollection)

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server error: %v", err)
	}
}

func rootHandler(cfg config.Config) http.HandlerFunc {
	type route struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"name":      cfg.ServerName,
			"version":   cfg.ServerVersion,
			"transport": cfg.Transport,
			"routes": []route{
				{Name: "sse", Path: cfg.BasePath + "/sse"},
				{Name: "message", Path: cfg.BasePath + "/message"},
				{Name: "healthz", Path: cfg.BasePath + "/healthz"},
				{Name: "readyz", Path: cfg.BasePath + "/readyz"},
			},
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func healthHandler(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "ok",
			"name":      cfg.ServerName,
			"version":   cfg.ServerVersion,
			"timestamp": time.Now().UTC(),
		})
	}
}

func readyHandler(cfg config.Config, store *knowledge.Store, vectorStore vector.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := os.Stat(cfg.KnowledgeStore)
		ready := err == nil || os.IsNotExist(err)
		if ready && vectorStore != nil {
			healthCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			ready = vectorStore.Health(healthCtx) == nil
		}
		status := http.StatusOK
		if !ready {
			status = http.StatusServiceUnavailable
		}

		payload := map[string]any{
			"status":          map[bool]string{true: "ready", false: "not_ready"}[ready],
			"knowledge_store": cfg.KnowledgeStore,
			"context_count":   len(store.ListContexts("", 1000)),
		}
		writeJSON(w, status, payload)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to serialize response: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func publicURL(cfg config.Config, suffix string) string {
	return cfg.PublicBaseURL + cfg.BasePath + suffix
}

func buildVectorStore(cfg config.Config) (vector.Store, error) {
	client := &http.Client{Timeout: cfg.HTTPTimeout}

	switch cfg.VectorDBProvider {
	case "file":
		return vector.NewFileStore(vector.FileConfig{
			Path:       cfg.VectorDBFilePath,
			Collection: cfg.VectorCollection,
			Dimension:  cfg.VectorDimension,
		})
	case "qdrant":
		return vector.NewQdrantStore(vector.QdrantConfig{
			URL:        cfg.VectorDBURL,
			APIKey:     cfg.VectorDBAPIKey,
			Username:   cfg.VectorDBUsername,
			Password:   cfg.VectorDBPassword,
			Collection: cfg.VectorCollection,
			Dimension:  cfg.VectorDimension,
			Distance:   cfg.VectorDistance,
		}, client)
	case "chroma":
		return vector.NewChromaStore(vector.ChromaConfig{
			URL:        cfg.VectorDBURL,
			APIKey:     cfg.VectorDBAPIKey,
			Username:   cfg.VectorDBUsername,
			Password:   cfg.VectorDBPassword,
			Tenant:     cfg.VectorDBTenant,
			Database:   cfg.VectorDBDatabase,
			Collection: cfg.VectorCollection,
			Dimension:  cfg.VectorDimension,
		}, client)
	default:
		return nil, fmt.Errorf("unsupported vector provider %q", cfg.VectorDBProvider)
	}
}

func vectorTarget(cfg config.Config) string {
	if cfg.VectorDBProvider == "file" {
		return cfg.VectorDBFilePath
	}
	return cfg.VectorDBURL
}
