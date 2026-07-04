# AI Agents

This document describes the AI agents and providers used in the AI Revolver proxy service, and serves as the foundational mandate for agentic assistants.

## Agent Mandates

1. **Foundational Context**: Always use `AGENTS.md` as the primary foundational instruction file (equivalent to `GEMINI.md`) at the start of a session. If both exist, `AGENTS.md` takes precedence.
2. **Context Propagation**: Always pass `context.Context` through all layers of the application. Upstream requests MUST be context-aware (using `http.NewRequestWithContext`).
3. **Error Handling**: Per-candidate upstream attempts use `response_timeout_seconds` from config (default **300s**). Failover to the next candidate occurs when the attempt times out or returns a non-200 status.
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
│  │  /api/v1/*  │  │  /mcp       │  │  Web UI (React)         │ │
│  │  OpenAI API │  │  Streamable │  │  / /config /test        │ │
│  └──────┬──────┘  │  HTTP (MCP) │  └─────────────────────────┘ │
│         │         └──────┬──────┘                               │
│         └────────────────┼──────────────────────────────────────┘
│                          │
│  ┌───────────────────────▼───────────────────────┐
│  │              Model Selection                  │
│  │  • Tiered: Active → Degraded → BlockedTemp    │
│  │  • Round-robin within priority tiers          │
│  │  • Last successful model priority (Active)    │
│  │  • Auto-failover on errors (max_retries)      │
│  │  • Rate limiting                              │
│  │  • Model blocking / EWMA degradation          │
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
  },
  "max_retries": 5,
  "connect_timeout_seconds": 5,
  "response_timeout_seconds": 300,
  "warmup_enabled": true,
  "warmup_interval": 180,
  "warmup_debounce": 60
}
```

| Config field | Default | Description |
|--------------|---------|-------------|
| `max_retries` | 5 | Maximum failover attempts per request |
| `connect_timeout_seconds` | 5 | TCP/TLS connect timeout |
| `response_timeout_seconds` | 300 | Per-candidate upstream attempt timeout |
| `warmup_interval` | 180 | Warmup tick interval (seconds) |
| `warmup_debounce` | 60 | Debounce after status change (seconds) |

### Stability & Performance

- **Context-Aware Cancellation**: If a client disconnects, the proxy immediately cancels all pending upstream requests to prevent resource leaks and reduce provider costs.
- **Resilient Failover**: Each candidate attempt uses a per-attempt timeout equal to `response_timeout_seconds` from config (default **300s**, overridable via `-config`). On timeout or non-200 response, the proxy tries the next candidate (up to `max_retries`, default **5**).
- **Connection Timeouts**: TCP/TLS connect timeout defaults to **5s** (`connect_timeout_seconds` in config).
- **Optimized Connection Pooling**: Defaults — `MaxIdleConns: 100`, `MaxIdleConnsPerHost: 20` (CLI flags `-max-idle-conns`, `-max-idle-conns-per-host`).
- **Zero-Timeout Read**: Server `ReadTimeout: 0` supports large prompt uploads; `ReadHeaderTimeout: 10s`, `WriteTimeout: 10min`, `IdleTimeout: 120s`.
- **Warmup**: Optional background warmup (`warmup_enabled`, interval default **180s**, debounce default **60s**).

### Provider Selection Logic

1. Providers are sorted by priority (lower number = higher priority); disabled providers are skipped.
2. Candidates are assembled in tiers: **Active** → **Degraded** (high EWMA latency) → **BlockedTemp** (sorted by EWMA latency within each tier).
3. Within the Active tier, the last successful model is tried first; remaining Active candidates follow round-robin order by model index across providers.
4. When a provider's `current_usage >= rate_limit`, it is skipped.
5. Failover stops after `max_retries` attempts (default **5**, configurable in `config.json`).
6. Request metrics are tracked per-provider and per-model in SQLite.
7. **Cancellation Check**: Before starting any candidate attempt, the proxy checks if the request context has been cancelled.
8. **Fallback**: If all candidates are blocked, the proxy may attempt the blocked model with the lowest EWMA latency.

### Rate Limiting

Each provider has a configurable `rate_limit` (per-provider, set in `config.json`; no global default). When `current_usage >= rate_limit`:
- The provider is skipped during candidate selection
- Rate limit events are logged to the database

### Model Blocking & Degradation

Model health is tracked per provider:model pair with EWMA latency (α = 0.2).

| Condition | Result |
|-----------|--------|
| EWMA latency > `-latency-threshold` (default **10000 ms**) | **Degraded** — still used, but lower priority (Tier 2) |
| 5xx or network/timeout (HTTP 0) | **BlockedTemp** after **1** failure; exponential backoff: `(2^level × 2)` minutes |
| 429 Too Many Requests | **BlockedTemp** for **5 minutes** |
| 400 Bad Request | **BlockedTemp** for **5 minutes** |
| 401 / 404 | **BlockedFatal** — permanently skipped |
| Other HTTP errors | **BlockedTemp** for **5 minutes** |

The `-block-duration` flag (default **5 minutes**) controls the failure-tracker cleanup window (`failureCooldown`), not the block durations above.

## Usage

### OpenAI-Compatible API

Send requests to the proxy endpoint with the desired model:

```bash
curl -X POST http://localhost:8081/api/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "arcee-ai/trinity-large-preview:free",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Streaming Requests

```bash
curl -X POST http://localhost:8081/api/v1/chat/completions \
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
| `-block-duration` | 5 | Failure-tracker cleanup window in minutes (0 = disable cleanup) |
| `-max-idle-conns` | 100 | Maximum idle connections in pool |
| `-max-idle-conns-per-host` | 20 | Maximum idle connections per host |
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
