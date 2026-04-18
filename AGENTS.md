# AI Agents

This document describes the AI agents and providers used in the AI Revolver proxy service, and serves as the foundational mandate for agentic assistants.

## Agent Mandates

1. **Foundational Context**: Always use `AGENTS.md` as the primary foundational instruction file (equivalent to `GEMINI.md`) at the start of a session. If both exist, `AGENTS.md` takes precedence.
2. **Context Propagation**: Always pass `context.Context` through all layers of the application. Upstream requests MUST be context-aware (using `http.NewRequestWithContext`).
3. **Error Handling**: Follow the 15-second per-candidate timeout rule in proxy logic.
4. **Stat Retention**: Ensure all request metrics are persisted to the SQLite stats database.
5. **Operational Continuity**: NEVER kill the service running on port 8081. If port 8081 is occupied, use an alternative port.
6. **Isolated Testing**: For any test execution, always use a port other than 8080 or 8081 to avoid interference with the live service.

## Overview

AI Revolver is a proxy service that routes AI requests to multiple upstream providers with automatic failover, load balancing, and rate limiting capabilities.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        AI Revolver                              │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐ │
│  │  /v1/*      │  │  /mcp       │  │  Web UI (React)         │ │
│  │  OpenAI API │  │  Streamable │  │  /config /test /stats   │ │
│  └──────┬──────┘  │  HTTP (MCP) │  └─────────────────────────┘ │
│         │         └──────┬──────┘                               │
│         └────────────────┼──────────────────────────────────────┘
│                          │
│  ┌───────────────────────▼───────────────────────┐
│  │              Model Selection                  │
│  │  • Round-robin across providers               │
│  │  • Last successful model priority             │
│  │  • Auto-failover on errors                    │
│  │  • Rate limiting                              │
│  │  • Model blocking (configurable duration)     │
│  └───────────────────────┬───────────────────────┘
│                          │
│  ┌───────────────────────▼───────────────────────┐
│  │              Async Services                    │
│  │  ┌─────────────────┐  ┌─────────────────────┐ │
│  │  │  Logger         │  │  Database           │ │
│  │  │  Buffered chan   │  │  Worker pool (2)    │ │
│  │  │  Non-blocking    │  │  Buffered chan       │ │
│  │  └─────────────────┘  └─────────────────────┘ │
│  └───────────────────────────────────────────────┘
└─────────────────────────────────────────────────────────────────┘
                           │
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  OpenRouter  │  │   KiloCode   │  │     Groq     │
│  Priority 2  │  │  Priority 1  │  │  Priority 3  │
└──────────────┘  └──────────────┘  └──────────────┘
```

## Providers

### KiloCode

**Base URL:** `https://api.kilo.ai/api/gateway/`

A multi-provider AI gateway offering access to various models with high availability.

**Priority:** 1 (highest)

#### Models

| Model | Max Tokens | Thinking | Reasoning | Tools |
|-------|------------|----------|-----------|-------|
| `arcee-ai/trinity-large-preview:free` | 131,000 | ❌ | ✅ | ✅ |
| `stepfun/step-3.5-flash:free` | 256,000 | ❌ | ✅ | ✅ |
| `openrouter/healer-alpha` | 262,144 | ❌ | ✅ | ✅ |
| `openrouter/hunter-alpha` | 1,048,576 | ❌ | ✅ | ✅ |
| `giga-potato` | 256,000 | ❌ | ✅ | ✅ |
| `giga-potato-thinking` | 256,000 | ✅ | ✅ | ✅ |
| `minimax/minimax-m2.5:free` | 204,800 | ❌ | ✅ | ✅ |

---

### OpenRouter

**Base URL:** `https://openrouter.ai/api/v1`

A unified API for accessing multiple LLM providers with built-in rate limiting and cost optimization.

**Priority:** 2

#### Models

| Model | Max Tokens | Thinking | Reasoning | Tools |
|-------|------------|----------|-----------|-------|
| `arcee-ai/trinity-large-preview:free` | 131,000 | ❌ | ✅ | ✅ |
| `openrouter/healer-alpha` | 262,144 | ❌ | ✅ | ✅ |
| `openrouter/hunter-alpha` | 1,048,576 | ❌ | ✅ | ✅ |

---

### Groq

**Base URL:** `https://api.groq.com/openai/v1`

High-performance inference platform optimized for low-latency responses.

**Priority:** 3

#### Models

*No models configured (available for future use)*

---

## Configuration

### Auto Mode

The proxy supports automatic provider selection:

```json
{
  "auto_mode": {
    "enabled": true,
    "fallback_strategy": "round-robin"
  }
}
```

### Stability & Performance

- **Context-Aware Cancellation**: If a client disconnects, the proxy immediately cancels all pending upstream requests to prevent resource leaks and reduce provider costs.
- **Resilient Failover**: Each provider connection attempt has a **15-second timeout**. If a provider is unresponsive, the proxy fails over to the next candidate immediately.
- **Optimized Connection Pooling**: Configured with `MaxIdleConnsPerHost: 100` to prevent head-of-line blocking under heavy load.
- **Zero-Timeout Read**: Server is configured with `ReadTimeout: 0` to support multi-megabyte prompt uploads without premature disconnection.

### Provider Selection Logic

1. Providers are sorted by priority (lower number = higher priority)
2. In auto mode, requests use round-robin across all models
3. When a provider hits rate limits, the system automatically falls back to the next available provider
4. Request metrics are tracked per-provider and per-model
5. Models are blocked after failures (configurable duration via `-block-duration`)
6. **Cancellation Check**: Before starting any candidate attempt, the proxy checks if the request context has been cancelled.

### Rate Limiting

Each provider has a configurable rate limit (default: 30 requests). When exceeded:
- Requests are redirected to fallback providers
- Rate limit resets are tracked in the database

### Model Blocking

Models are automatically blocked when:
- 2+ consecutive errors occur
- Latency exceeds configured threshold (`-latency-threshold`)

Block duration is configurable via `-block-duration` (default: 5 minutes).

## Usage

### OpenAI-Compatible API

Send requests to the proxy endpoint with the desired model:

```bash
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "arcee-ai/trinity-large-preview:free",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Streaming Requests

```bash
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "giga-potato-thinking",
    "messages": [{"role": "user", "content": "Explain quantum computing"}],
    "stream": true
  }'
```

### Streamable HTTP (MCP)

The `/mcp` endpoint implements MCP Streamable HTTP transport for AI agents:

```bash
# List models with capabilities
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "models/list"
  }'

# Chat completion via MCP
curl -X POST http://localhost:8081/mcp \
  -H "Content-Type: application/json" \
  -H "Accept: text/event-stream" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/call",
    "params": {
      "name": "chat_completion",
      "arguments": {
        "model": "auto",
        "messages": [{"role": "user", "content": "Hello"}],
        "stream": true
      }
    }
  }'
```

#### MCP Methods

| Method | Description |
|--------|-------------|
| `initialize` | Initialize session, get server info |
| `notifications/initialized` | Acknowledge initialization |
| `tools/list` | List available tools |
| `tools/call` | Call a tool (chat_completion) |
| `models/list` | List models with capabilities |

#### MCP Session Management

Sessions are automatically managed via `Mcp-Session-Id` header:
- New sessions created on first request
- Sessions reused when ID provided
- Inactive sessions cleaned up after 30 minutes

## Monitoring

Request statistics are logged to `data/stats.db` including:
- Total requests per provider/model
- Success/failure counts
- Average latency
- Rate limit events
- Error tracking

## Model Capabilities

| Capability | Description |
|------------|-------------|
| **thinking** | Extended reasoning with thought chains |
| **reasoning** | Logical deduction and problem solving |
| **tools** | Function calling and tool usage support |

## Adding New Providers

To add a new AI provider:

1. Add provider configuration to `data/config.json`:
   ```json
   {
     "name": "new-provider",
     "api_key": "your-api-key",
     "base_url": "https://api.newprovider.com/v1",
     "enabled": true,
     "models": [
       {
         "name": "provider/model-name",
         "max_tokens": 128000,
         "thinking": false,
         "reasoning": true,
         "tools": true
       }
     ],
     "rate_limit": 30,
     "priority": 4
   }
   ```

2. Restart the proxy service (or send SIGHUP to reload config)

3. The new provider will be automatically included in the selection pool

## Command Line Options

| Option | Default | Description |
|--------|---------|-------------|
| `-port` | 8081 | Port to listen on |
| `-latency-threshold` | 10000 | Latency threshold in ms (0 = disable) |
| `-block-duration` | 5 | Block duration in minutes (0 = disable) |
| `-max-idle-conns` | 100 | Maximum idle connections in pool |
| `-max-idle-conns-per-host` | 100 | Maximum idle connections per host |
| `-idle-conn-timeout` | 90s | Idle connection timeout |
| `-stream-buffer-size` | 2MB | Buffer size for streaming responses |
| `-config` | data/config.json | Path to config file |
| `-stats` | data/stats.db | Path to stats database |
| `-debug` | false | Enable debug logging |
| `-trace` | false | Enable trace logging for all payloads |

## Graceful Shutdown

The proxy handles SIGINT/SIGTERM for graceful shutdown:
- Completes pending database writes
- Flushes remaining log entries
- Closes database connection
