# Kiro Gateway (Go)

A lightweight Go implementation of a proxy gateway for the Kiro API (Amazon Q Developer / AWS CodeWhisperer). Provides OpenAI-compatible API endpoints for use with AI coding assistants.

## Features

- **OpenAI-compatible API** - Use standard OpenAI client libraries
- **Multiple authentication methods**:
  - SQLite database (kiro-cli)
  - JSON credentials file
  - Refresh token
- **Streaming support** - Full SSE streaming for real-time responses
- **Tool/function calling** - Support for Claude tools and function definitions
- **Automatic token refresh** - Handles AWS SSO OIDC token expiration
- **Retry logic** - Exponential backoff for rate limits and server errors
- **Debug mode** - Log all HTTP requests/responses and auth events

## Quick Start

```bash
# Build
go build -o bin/server ./cmd/server

# Run with environment variables
export KIRO_CLI_DB_FILE=~/.local/share/kiro-cli/data.sqlite3
export PROXY_API_KEY=your-secret-key

./bin/server
```

## Configuration

Set these environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_HOST` | Listen address | `0.0.0.0` |
| `SERVER_PORT` | Listen port | `8000` |
| `PROXY_API_KEY` | **Required** - API key for clients | (none) |
| `KIRO_REGION` | AWS region | `us-east-1` |
| `DEBUG` | Enable debug logging (`true`/`false`) | `false` |
| `DEBUG_LOG_FILE` | Path to debug log file (empty = stdout) | stdout |

> **Important**: You must set a strong, unique `PROXY_API_KEY` before running the server.

### Client Authentication

All API requests (except health check) require the `Authorization` header with the value set to the `PROXY_API_KEY` environment variable:

```bash
curl -H "Authorization: Bearer ${PROXY_API_KEY}" ...
```

The server validates this header and rejects requests with a `401 Unauthorized` error if it doesn't match.

### Debug Mode

When `DEBUG=true`, the server logs all HTTP traffic and authentication events in JSON format:

```bash
# Log to stdout
export DEBUG=true
./bin/server

# Log to file
export DEBUG=true
export DEBUG_LOG_FILE=/tmp/kiro-debug.log
./bin/server
```

Debug output includes:
- **Gateway requests/responses** - Client to gateway HTTP traffic
- **Amazon Q requests/responses** - Gateway to Amazon Q HTTP traffic  
- **Auth events** - Token refresh, rotation, and failures

Sensitive data (API keys, tokens) are best-effort redacted in logs.

> **Warning:** Debug logs may contain sensitive data. Treat debug logs as sensitive and avoid enabling `DEBUG` in production or sharing log files.

### Authentication (choose one)

| Variable | Description |
|----------|-------------|
| `KIRO_CLI_DB_FILE` | Path to kiro-cli SQLite database |
| `KIRO_CREDS_FILE` | Path to JSON credentials file |
| `REFRESH_TOKEN` | Direct refresh token |
| `KIRO_CLIENT_ID` | AWS SSO OIDC client ID |
| `KIRO_CLIENT_SECRET` | AWS SSO OIDC client secret |

## API Endpoints

### Health Check

```bash
curl http://localhost:8000/
# {"status":"ok","message":"Kiro Gateway is running"}

curl http://localhost:8000/health
# {"status":"healthy","timestamp":"..."}
```

### List Models

```bash
curl -H "Authorization: Bearer your-secret-key" \
  http://localhost:8000/v1/models
```

### Chat Completions

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.5",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": false
  }'
```

### Streaming

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Authorization: Bearer your-secret-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.5",
    "messages": [{"role": "user", "content": "Count to 5"}],
    "stream": true
  }'
```

## Supported Models

- `auto` - Kiro's automatic model selection
- `claude-sonnet-4.5`
- `claude-haiku-4.5`
- `claude-opus-4.5`
- `claude-sonnet-4`
- `deepseek-v3.2`
- `mini-max-m2.1`
- `qwen3-coder-next`

## Development

### Running Tests

```bash
go test ./...
```

### With Coverage

```bash
go test ./... -cover
```

### Pre-commit Hooks

To install the pre-commit hook:

```bash
ln -s ../../scripts/pre-commit .git/hooks/pre-commit
```

## Architecture

```
kiro-go-gw/
├── cmd/server/main.go       # Entry point
└── internal/
    ├── auth/                # Authentication (SQLite, JSON, token)
    ├── client/             # HTTP client with retry logic
    ├── config/             # Configuration from environment
    ├── converter/          # OpenAI → Kiro payload conversion
    ├── models/             # Request/response types
    ├── server/             # HTTP handlers
    └── streaming/          # SSE parsing and conversion
```

## Credits

Inspired by [kiro-gateway](https://github.com/jwadow/kiro-gateway) by @jwadow.

## License

MIT License.
