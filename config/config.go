package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultServerName        = "ProductionMCP"
	defaultServerVersion     = "2.0.0"
	defaultTransport         = "sse"
	defaultPort              = "8080"
	defaultHTTPTimeout       = 20 * time.Second
	defaultFetchMaxBytes     = 2 * 1024 * 1024
	defaultFileMaxBytes      = 5 * 1024 * 1024
	defaultSearchResultLimit = 5
	defaultChunkSize         = 900
	defaultChunkOverlap      = 150
)

type Config struct {
	ServerName        string
	ServerVersion     string
	Transport         string
	Host              string
	Port              string
	BasePath          string
	DataDir           string
	ReadRoot          string
	KnowledgeStore    string
	HTTPTimeout       time.Duration
	FetchMaxBytes     int64
	FileMaxBytes      int64
	SearchResultLimit int
	DefaultChunkSize  int
	DefaultOverlap    int
	VectorDBProvider  string
	VectorDBURL       string
	VectorDBFilePath  string
	VectorDBAPIKey    string
	VectorDBUsername  string
	VectorDBPassword  string
	VectorCollection  string
	VectorDimension   int
	VectorDistance    string
	VectorDBTenant    string
	VectorDBDatabase  string
	WebSearchURL      string
	PublicBaseURL     string
}

func Load() (Config, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return Config{}, fmt.Errorf("get working directory: %w", err)
	}

	dataDir := envOrDefault("DATA_DIR", filepath.Join(workingDir, "data"))
	readRoot := envOrDefault("READ_ROOT", workingDir)

	cfg := Config{
		ServerName:        envOrDefault("SERVER_NAME", defaultServerName),
		ServerVersion:     envOrDefault("SERVER_VERSION", defaultServerVersion),
		Transport:         strings.ToLower(envOrDefault("TRANSPORT", defaultTransport)),
		Host:              envOrDefault("HOST", "0.0.0.0"),
		Port:              envOrDefault("PORT", defaultPort),
		BasePath:          normalizeBasePath(envOrDefault("BASE_PATH", "")),
		DataDir:           dataDir,
		ReadRoot:          readRoot,
		KnowledgeStore:    envOrDefault("KNOWLEDGE_STORE_PATH", filepath.Join(dataDir, "knowledge-store.json")),
		HTTPTimeout:       envDuration("HTTP_TIMEOUT", defaultHTTPTimeout),
		FetchMaxBytes:     envInt64("FETCH_MAX_BYTES", defaultFetchMaxBytes),
		FileMaxBytes:      envInt64("FILE_MAX_BYTES", defaultFileMaxBytes),
		SearchResultLimit: envInt("SEARCH_RESULT_LIMIT", defaultSearchResultLimit),
		DefaultChunkSize:  envInt("DEFAULT_CHUNK_SIZE", defaultChunkSize),
		DefaultOverlap:    envInt("DEFAULT_CHUNK_OVERLAP", defaultChunkOverlap),
		VectorDBProvider:  strings.ToLower(envOrDefault("VECTOR_DB_PROVIDER", "file")),
		VectorDBURL:       envOrDefault("VECTOR_DB_URL", ""),
		VectorDBFilePath:  envOrDefault("VECTOR_DB_FILE_PATH", filepath.Join(dataDir, "vector-store.json")),
		VectorDBAPIKey:    envOrDefault("VECTOR_DB_API_KEY", ""),
		VectorDBUsername:  envOrDefault("VECTOR_DB_USERNAME", ""),
		VectorDBPassword:  envOrDefault("VECTOR_DB_PASSWORD", ""),
		VectorCollection:  envOrDefault("VECTOR_DB_COLLECTION", "mcp_knowledge"),
		VectorDimension:   envInt("VECTOR_DB_DIMENSION", 384),
		VectorDistance:    envOrDefault("VECTOR_DB_DISTANCE", "Cosine"),
		VectorDBTenant:    envOrDefault("VECTOR_DB_TENANT", "default_tenant"),
		VectorDBDatabase:  envOrDefault("VECTOR_DB_DATABASE", "default_database"),
		WebSearchURL:      envOrDefault("WEB_SEARCH_URL_TEMPLATE", ""),
		PublicBaseURL:     envOrDefault("PUBLIC_BASE_URL", ""),
	}

	if cfg.Transport != "sse" && cfg.Transport != "stdio" {
		return Config{}, fmt.Errorf("unsupported transport %q: expected \"sse\" or \"stdio\"", cfg.Transport)
	}

	if cfg.SearchResultLimit <= 0 {
		cfg.SearchResultLimit = defaultSearchResultLimit
	}
	if cfg.DefaultChunkSize < 200 {
		cfg.DefaultChunkSize = defaultChunkSize
	}
	if cfg.DefaultOverlap < 0 || cfg.DefaultOverlap >= cfg.DefaultChunkSize {
		cfg.DefaultOverlap = defaultChunkOverlap
	}
	if cfg.VectorDimension <= 0 {
		cfg.VectorDimension = 384
	}

	cfg.DataDir, err = filepath.Abs(cfg.DataDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve data dir: %w", err)
	}
	cfg.ReadRoot, err = filepath.Abs(cfg.ReadRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve read root: %w", err)
	}
	cfg.KnowledgeStore, err = filepath.Abs(cfg.KnowledgeStore)
	if err != nil {
		return Config{}, fmt.Errorf("resolve knowledge store path: %w", err)
	}
	cfg.VectorDBFilePath, err = filepath.Abs(cfg.VectorDBFilePath)
	if err != nil {
		return Config{}, fmt.Errorf("resolve vector db file path: %w", err)
	}

	switch cfg.VectorDBProvider {
	case "file", "qdrant", "chroma":
	default:
		return Config{}, fmt.Errorf("unsupported VECTOR_DB_PROVIDER %q: expected \"file\", \"qdrant\" or \"chroma\"", cfg.VectorDBProvider)
	}
	if cfg.VectorDBProvider != "file" && strings.TrimSpace(cfg.VectorDBURL) == "" {
		return Config{}, fmt.Errorf("VECTOR_DB_URL is required and must be set via environment")
	}
	if strings.TrimSpace(cfg.WebSearchURL) == "" {
		return Config{}, fmt.Errorf("WEB_SEARCH_URL_TEMPLATE is required and must be set via environment")
	}
	if strings.TrimSpace(cfg.PublicBaseURL) == "" {
		return Config{}, fmt.Errorf("PUBLIC_BASE_URL is required and must be set via environment")
	}
	if !strings.Contains(cfg.WebSearchURL, "%s") {
		return Config{}, fmt.Errorf("WEB_SEARCH_URL_TEMPLATE must contain %%s for the encoded query")
	}

	return cfg, nil
}

func (c Config) ListenAddr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func normalizeBasePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "/" {
		return ""
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	return strings.TrimRight(value, "/")
}
