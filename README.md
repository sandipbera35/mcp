<p align="center">
  <img alt="MCP Coding Agent Server with RAG and CAG" src="https://img.shields.io/badge/MCP%20Coding%20Agent%20Server%20with%20RAG%20and%20CAG-2563eb?style=for-the-badge&logo=go&logoColor=white" />
</p>

# MCP Coding Agent Server with RAG and CAG

This project is a Go-based MCP server that can act as both:

- a coding agent service for reading, writing, and editing files inside a safe workspace root
- a retrieval-enabled agent service that uses:
  - RAG through an external vector database
  - CAG through locally cached context bundles

It is designed so you can either:

- use the built-in free file-based vector store by default
- or connect to an external vector database running in another container or service

## What This Server Does

At a high level, this server gives an MCP-compatible model or client a set of tools it can call.

Those tools let the model:

- read local files
- write local files
- edit local files
- fetch web pages
- run web search through a configured external search URL
- ingest knowledge into a vector database
- search that knowledge later
- store reusable cached context
- retrieve cached context later

That means the same MCP server can behave like:

- a coding assistant
- a documentation retrieval assistant
- a project-aware agent
- a context-aware assistant with reusable memory packs

## What RAG Means Here

RAG stands for `Retrieval-Augmented Generation`.

In this server, RAG means:

1. You ingest content into a vector database.
2. That content is chunked and embedded.
3. Later, when the model needs knowledge, it calls `search_knowledge`.
4. The most relevant chunks are returned.
5. The model uses those chunks to answer with better context.

Practical example:

- You ingest `docs/runbook.md`
- The server sends chunked vectors to Qdrant
- Later the model asks: "How do we roll back a blue-green deploy?"
- `search_knowledge` returns the most relevant chunks
- The model answers using those retrieved chunks

Use RAG when:

- the knowledge base is large
- you want semantic search
- you want to search documents by meaning, not only exact words
- you want to keep knowledge outside the model prompt until needed

## What CAG Means Here

CAG here means `Cached-Augmented Generation`.

In this server, CAG is the lightweight reusable context layer.

Instead of searching a vector database every time, you can store named context bundles locally and reuse them directly.

Examples:

- customer-specific instructions
- product constraints
- coding conventions
- project briefing notes
- reusable agent setup context

Practical example:

1. Save a context called `customer-alpha`
2. Store notes like SLA language, preferred wording, and restrictions
3. Later the model calls `get_cached_context`
4. The model immediately gets that known context without a vector search

Use CAG when:

- the context is small and stable
- the same context is reused often
- you want deterministic reusable context packs
- you do not need semantic retrieval for that information

## RAG vs CAG

RAG:

- best for searching large document sets
- uses the external vector database
- good for semantic retrieval
- good for changing or growing corpora

CAG:

- best for small reusable context bundles
- stored locally by key
- fast and direct
- good for repeated known context

The two are complementary, not competing.

A strong agent often uses both:

- CAG for stable repeated context
- RAG for document retrieval on demand

## Architecture

The system is split into two parts:

1. MCP Server
2. Vector Database

The MCP server:

- exposes tools to the model
- manages file operations
- manages cached context
- talks to the vector database for RAG

The vector database:

- stores embeddings and metadata
- serves semantic search results
- runs separately from the MCP server

Current vector backends:

- `file` as the default free local option
- `qdrant`
- `chroma`

## Tool Surface

### File and coding tools

- `read_file`
  Reads a text file inside `READ_ROOT`
- `write_file`
  Creates or overwrites a text file inside `READ_ROOT`
- `edit_file`
  Edits a text file inside `READ_ROOT` using `replace` or `append`

### Web and utility tools

- `fetch_url`
  Fetches a remote URL with size and timeout controls
- `web_search`
  Uses the search URL template defined in env
- `echo`
  Simple test tool

### RAG tools

- `ingest_knowledge`
  Sends content into the vector database
- `search_knowledge`
  Searches the vector database for relevant chunks

### CAG tools

- `cache_context`
  Saves a named context bundle locally
- `get_cached_context`
  Retrieves a saved context bundle
- `list_cached_contexts`
  Lists saved context bundles

## Safe File Access

This server is intended to be usable as a coding agent.

To keep that safe, file operations are restricted by `READ_ROOT`.

That means:

- `read_file` can only read inside `READ_ROOT`
- `write_file` can only write inside `READ_ROOT`
- `edit_file` can only edit inside `READ_ROOT`
- file-based knowledge ingestion is also restricted to `READ_ROOT`

If a path is outside `READ_ROOT`, the request is rejected.

## Runtime Modes

### `TRANSPORT=sse`

Use this when the server should run as an HTTP service.

Endpoints:

- `GET /sse`
- `POST /message`
- `GET /healthz`
- `GET /readyz`

If `BASE_PATH=/mcp`, the endpoints become:

- `/mcp/sse`
- `/mcp/message`
- `/mcp/healthz`
- `/mcp/readyz`

### `TRANSPORT=stdio`

Use this when the MCP client launches the server directly as a process.

## Environment Configuration

This project expects external links and service addresses to come from environment variables.

That includes:

- vector database URL
- public base URL
- web search URL template

The local `.env` file can use `export KEY=value` format.

Example:

```env
export SERVER_NAME=ProductionMCP
export SERVER_VERSION=2.0.0
export TRANSPORT=sse
export HOST=0.0.0.0
export PORT=4090
export BASE_PATH=

export DATA_DIR=./data
export READ_ROOT=.
export KNOWLEDGE_STORE_PATH=./data/knowledge-store.json

export VECTOR_DB_PROVIDER=qdrant
export VECTOR_DB_URL=http://localhost:6333
export VECTOR_DB_API_KEY=
export VECTOR_DB_USERNAME=
export VECTOR_DB_PASSWORD=
export VECTOR_DB_COLLECTION=mcp_knowledge
export VECTOR_DB_DIMENSION=384
export VECTOR_DB_DISTANCE=Cosine

export WEB_SEARCH_URL_TEMPLATE=https://search.yahoo.com/search?p=%s
export PUBLIC_BASE_URL=http://localhost:4090

export HTTP_TIMEOUT=20s
export FETCH_MAX_BYTES=2097152
export FILE_MAX_BYTES=5242880
export SEARCH_RESULT_LIMIT=5
export DEFAULT_CHUNK_SIZE=900
export DEFAULT_CHUNK_OVERLAP=150
```

## Important Variables

### Core server variables

- `TRANSPORT`
  `sse` or `stdio`
- `HOST`
  Bind host for HTTP mode
- `PORT`
  Bind port for HTTP mode
- `BASE_PATH`
  Optional URL prefix
- `PUBLIC_BASE_URL`
  Publicly advertised base URL for the service

### File safety variables

- `READ_ROOT`
  The workspace root the coding agent is allowed to use
- `FILE_MAX_BYTES`
  Maximum allowed file read/write size

### Vector database variables

- `VECTOR_DB_PROVIDER`
  Currently supported values:
  - `file`
  - `qdrant`
  - `chroma`
- `VECTOR_DB_URL`
  Address of the external vector database service
- `VECTOR_DB_FILE_PATH`
  Local JSON file path used when `VECTOR_DB_PROVIDER=file`
- `VECTOR_DB_API_KEY`
  API key if your selected backend uses one
- `VECTOR_DB_USERNAME`
  Optional username for basic auth
- `VECTOR_DB_PASSWORD`
  Optional password for basic auth
- `VECTOR_DB_COLLECTION`
  Collection name used by the server
- `VECTOR_DB_DIMENSION`
  Embedding dimension used by this server
- `VECTOR_DB_DISTANCE`
  Distance metric used by providers that support it directly, such as Qdrant
- `VECTOR_DB_TENANT`
  Used by Chroma
- `VECTOR_DB_DATABASE`
  Used by Chroma

### Search variables

- `WEB_SEARCH_URL_TEMPLATE`
  External search URL template used by `web_search`

Important:

`WEB_SEARCH_URL_TEMPLATE` must contain `%s` because the server inserts the encoded query there.

Example:

```text
https://search.yahoo.com/search?p=%s
```

## Default Free Vector DB

The default provider is:

- `file`

This is a local file-based vector store that persists vectors to disk as JSON.

That means:

- it is free to use
- it requires no external service
- it works out of the box
- it is a good default for local development and lightweight deployments

Example:

```env
export VECTOR_DB_PROVIDER=file
export VECTOR_DB_FILE_PATH=./data/vector-store.json
export VECTOR_DB_COLLECTION=mcp_knowledge
export VECTOR_DB_DIMENSION=384
```

Use the file backend when:

- you want zero setup
- you want a fully local install
- you do not want to run another container
- your corpus size is moderate

## How to Connect to an External Vector DB

This is the core setup for RAG.

If you do not use the default file backend, the MCP server does not run the vector database itself.

You run your chosen vector database separately, then point the MCP server to it with:

- `VECTOR_DB_URL`
- optional credentials
- collection settings

### Qdrant example

If Qdrant is running locally on port `6333`:

```env
export VECTOR_DB_PROVIDER=qdrant
export VECTOR_DB_URL=http://localhost:6333
export VECTOR_DB_COLLECTION=mcp_knowledge
export VECTOR_DB_DIMENSION=384
export VECTOR_DB_DISTANCE=Cosine
```

### Chroma example

If Chroma is running locally on port `8000`:

```env
export VECTOR_DB_PROVIDER=chroma
export VECTOR_DB_URL=http://localhost:8000
export VECTOR_DB_TENANT=default_tenant
export VECTOR_DB_DATABASE=default_database
export VECTOR_DB_COLLECTION=mcp_knowledge
export VECTOR_DB_DIMENSION=384
```

### Docker Compose example with Qdrant

```yaml
services:
  mcp:
    build: .
    environment:
      TRANSPORT: sse
      HOST: 0.0.0.0
      PORT: 8080
      PUBLIC_BASE_URL: http://localhost:8080
      VECTOR_DB_PROVIDER: qdrant
      VECTOR_DB_URL: http://qdrant:6333
      VECTOR_DB_COLLECTION: mcp_knowledge
      VECTOR_DB_DIMENSION: 384
      VECTOR_DB_DISTANCE: Cosine
      WEB_SEARCH_URL_TEMPLATE: https://search.yahoo.com/search?p=%s
    depends_on:
      - qdrant
    ports:
      - "8080:8080"

  qdrant:
    image: qdrant/qdrant:latest
    ports:
      - "6333:6333"
```

In that setup:

- the MCP server reaches Qdrant at `http://qdrant:6333`
- your local machine reaches the MCP server at `http://localhost:8080`

You can swap Qdrant for Chroma by changing `VECTOR_DB_PROVIDER` and `VECTOR_DB_URL` and, for Chroma, also setting `VECTOR_DB_TENANT` and `VECTOR_DB_DATABASE`.

## Running the Server

### HTTP / SSE mode

```bash
go run .
```

Typical startup output:

```text
MCP server ProductionMCP v2.0.0 listening on 0.0.0.0:4090
SSE endpoint: http://localhost:4090/sse
Message endpoint: http://localhost:4090/message
Knowledge store: /absolute/path/data/knowledge-store.json
Vector database: http://localhost:6333 collection=mcp_knowledge
```

### Stdio mode

```bash
TRANSPORT=stdio go run .
```

## How a Client Uses This Server

An MCP-compatible client connects to the server and lets the model call tools.

### In SSE mode

Connect to:

- SSE stream: `PUBLIC_BASE_URL + BASE_PATH + /sse`
- message endpoint: `PUBLIC_BASE_URL + BASE_PATH + /message`

Example with current sample env:

- `http://localhost:4090/sse`
- `http://localhost:4090/message`

### In stdio mode

The client launches the process directly.

## Typical RAG Workflow

### Step 1: ingest knowledge

Example request:

```json
{
  "source_type": "file",
  "path": "docs/runbook.md",
  "title": "Ops Runbook",
  "tags": ["ops", "deploy"]
}
```

What happens:

1. The file is read
2. The text is chunked
3. Embeddings are generated
4. The chunks are written into Qdrant

### Step 2: search knowledge

Example request:

```json
{
  "query": "blue green deploy rollback procedure",
  "limit": 3
}
```

What happens:

1. The query is embedded
2. Qdrant performs vector search
3. The top matching chunks are returned
4. The model answers using those chunks

## Typical CAG Workflow

### Step 1: cache stable context

Example request:

```json
{
  "key": "customer-alpha",
  "title": "Customer Alpha Notes",
  "content": "Use premium SLA wording. Avoid casual language. Include migration caveats.",
  "tags": ["customer", "enterprise"]
}
```

### Step 2: retrieve cached context later

Example request:

```json
{
  "key": "customer-alpha"
}
```

What happens:

1. The server loads the saved context bundle
2. The model gets that stable context directly
3. No vector search is needed for that data

## Coding Agent Workflow

This server can also support coding-agent tasks inside `READ_ROOT`.

Typical flow:

1. Use `read_file`
2. Decide on changes
3. Use `write_file` for full-file writes
4. Use `edit_file` for targeted replacements or appends

### Example `write_file`

```json
{
  "path": "tmp/example.txt",
  "content": "hello world"
}
```

### Example `edit_file` replace

```json
{
  "path": "main.go",
  "operation": "replace",
  "old_text": "func main() {}",
  "new_text": "func main() {\n\tprintln(\"hello\")\n}"
}
```

### Example `edit_file` append

```json
{
  "path": "main.go",
  "operation": "append",
  "new_text": "\n// done\n"
}
```

## Operational Notes

- file operations are restricted to `READ_ROOT`
- cached context is stored locally on disk
- RAG retrieval depends on external vector DB connectivity
- readiness depends on vector DB health
- external service links should be provided through env
- web fetches and search responses are bounded by byte limits

## Current Limitation

The vector backend is currently implemented for multiple free/self-hostable providers.

That means:

- `VECTOR_DB_PROVIDER` can currently be `file`, `qdrant`, or `chroma`
- the external vector service must match the selected provider

The MCP surface is already shaped so another vector backend could be added later without changing how clients use the tools.

## Testing

Run:

```bash
GOCACHE=$(pwd)/.gocache go test ./...
```

The tests are hermetic and do not rely on live internet access.
