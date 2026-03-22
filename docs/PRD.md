# Product Requirements Document: actually-lite-llm

## 1. Executive Summary

**Problem Statement**: LiteLLM consumes ~800MB idle RAM for what is essentially a config-driven LLM request proxy. It cannot do per-application API key usage tracking without a database, which is the main gap for homelab operators who want to hand out virtual keys to different apps and track spend per key.

**Proposed Solution**: A stateless Go binary that accepts OpenAI-compatible requests, routes them to the correct upstream provider (translating API formats as needed), and exposes per-virtual-key usage metrics via Prometheus. Config-driven only — no database, no UI.

**Success Criteria**:
- Idle memory usage < 50MB (vs LiteLLM's ~800MB)
- Added request latency overhead < 5ms p99 (proxy tax only, excluding upstream)
- Correctly translates OpenAI SDK requests to Anthropic API format (passing the Anthropic SDK test cases for chat completions)
- Per-virtual-key Prometheus metrics for tokens, requests, and duration
- Deploys as a single stateless container in Kubernetes; horizontally scalable via replicas

---

## 2. User Experience & Functionality

### User Personas

1. **Homelab Operator** (primary): Runs multiple self-hosted apps (chat UIs, coding assistants, automation scripts) that need LLM access. Wants a single gateway to manage API keys and track which app is consuming what.
2. **App Developer** (consumer): Writes code using the OpenAI SDK. Points the SDK's `base_url` at the gateway. Doesn't care which upstream provider is used — the gateway handles routing.

### User Stories

**US-1**: As a homelab operator, I want to define virtual API keys in a YAML config so that each application gets its own key and I can track usage per app without a database.
- AC: Config file defines keys with an `app` label and optional `allowed_models` list
- AC: Requests with an unknown key return HTTP 401
- AC: Requests to a disallowed model return HTTP 403

**US-2**: As an app developer, I want to use the standard OpenAI SDK and have my requests routed to the correct provider so that I don't need provider-specific code.
- AC: `POST /v1/chat/completions` accepts OpenAI chat completion format
- AC: Model name determines provider routing (via alias map or prefix rules)
- AC: Both streaming (SSE) and non-streaming responses work

**US-3**: As a homelab operator, I want Prometheus metrics labeled by virtual key / app name so that I can build Grafana dashboards showing spend and usage per application.
- AC: `/metrics` endpoint exposes token counts, request counts, and latency histograms
- AC: All metrics include `app`, `provider`, `model` labels

**US-4**: As a homelab operator, I want structured access logs on stdout so that I can pipe them to my existing log aggregation.
- AC: Each request logs: timestamp, method, path, status code, duration, app name (from virtual key), upstream provider, model
- AC: JSON format for machine parseability

**US-5**: As a homelab operator, I want to run multiple gateway replicas behind a Kubernetes service so that I get availability without needing shared state.
- AC: No in-process state beyond config; any replica can serve any request
- AC: Health endpoint (`GET /health`) returns 200 when ready
- AC: Graceful shutdown on SIGTERM (drain in-flight requests)

### Non-Goals

- **No UI or dashboard** — Grafana + Prometheus is the monitoring layer
- **No database** — all config from YAML, all metrics from Prometheus
- **No response caching** — every request goes upstream
- **No retry or fallback** — if a provider errors, the error is returned to the caller
- **No rate limiting** — handled at the Kubernetes/infra layer if needed
- **No authentication federation** — virtual keys are static strings in config, not OAuth/OIDC
- **No multi-tenancy beyond virtual keys** — no billing, quotas, or admin API

---

## 3. Technical Specifications

### Architecture Overview

```
┌──────────────┐     ┌─────────────────────────────────┐     ┌──────────────┐
│  App (OpenAI │────▶│  actually-lite-llm               │────▶│  OpenAI API  │
│  SDK)        │     │                                  │     └──────────────┘
└──────────────┘     │  1. Auth (virtual key lookup)    │
                     │  2. Route (model→provider)       │     ┌──────────────┐
┌──────────────┐     │  3. Translate (OpenAI→provider)  │────▶│ Anthropic API│
│  Prometheus  │◀────│  4. Proxy (forward request)      │     └──────────────┘
└──────────────┘     │  5. Translate back (→OpenAI fmt) │
                     │  6. Metrics + access log         │
                     └─────────────────────────────────┘
```

**Request flow**:
1. Inbound request hits auth middleware → extract `Bearer` token, look up app name + allowed models
2. Parse OpenAI-format `ChatCompletionRequest`
3. Resolve model: check `models` alias map, then fall back to `routing` prefix rules
4. Provider adapter translates request to native format, makes upstream HTTP call
5. Provider adapter translates response back to OpenAI format
6. Write response (JSON or SSE stream) to caller
7. Record Prometheus metrics, write access log line

### Config Schema (`config.yaml`)

```yaml
listen: ":8080"

providers:
  openai:
    api_key: "${OPENAI_API_KEY}"
    base_url: "https://api.openai.com/v1"    # optional, has default
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
    base_url: "https://api.anthropic.com/v1"  # optional, has default

models:
  # alias → upstream model + provider
  "gpt-4o": { provider: "openai", model: "gpt-4o" }
  "claude-sonnet": { provider: "anthropic", model: "claude-sonnet-4-20250514" }
  "claude-haiku": { provider: "anthropic", model: "claude-haiku-4-5-20251001" }

routing:
  # Prefix-based fallback for models not in the alias map
  - prefix: "gpt-"
    provider: "openai"
  - prefix: "claude-"
    provider: "anthropic"

api_keys:
  - key: "${VKEY_BACKEND}"
    app: "my-backend"
    allowed_models: ["*"]
  - key: "${VKEY_CHATBOT}"
    app: "chatbot"
    allowed_models: ["claude-sonnet", "gpt-4o"]
```

Environment variables are interpolated at config load time via `os.ExpandEnv`.

### Integration Points

**Upstream APIs**:
- OpenAI: `POST /v1/chat/completions` — passthrough with key swap
- Anthropic: `POST /v1/messages` — full format translation (see below)

**Anthropic format translation**:
| OpenAI → Anthropic | Notes |
|---|---|
| `messages` with `role: "system"` → top-level `system` field | Extract out of array |
| `max_tokens` → `max_tokens` | Required by Anthropic; default to 4096 |
| `stop` → `stop_sequences` | Rename |
| `stream` → `stream` | Direct |
| Response `content[].text` → `choices[0].message.content` | Concatenate text blocks |
| `stop_reason: "end_turn"` → `finish_reason: "stop"` | Map values |
| `usage.input_tokens` → `usage.prompt_tokens` | Rename |
| Streaming: `content_block_delta` → SSE chunk with `delta.content` | Event type translation |

**Outbound headers (Anthropic)**: `x-api-key`, `anthropic-version: 2023-06-01`, `content-type: application/json`

### Prometheus Metrics

| Metric | Type | Labels |
|---|---|---|
| `llm_requests_total` | Counter | `app`, `provider`, `model`, `status` |
| `llm_request_duration_seconds` | Histogram | `app`, `provider`, `model`, `stream` |
| `llm_tokens_total` | Counter | `app`, `provider`, `model`, `direction` (prompt/completion) |
| `llm_stream_first_byte_seconds` | Histogram | `app`, `provider`, `model` |
| `llm_provider_errors_total` | Counter | `provider`, `error_type` |

### Security & Privacy

- Virtual API keys stored in config YAML — should be injected via env vars or Kubernetes secrets, not hardcoded
- Upstream provider API keys similarly via env vars
- No request/response bodies are logged or stored — only metadata in access logs
- No TLS termination (handled by ingress/load balancer in k8s)

---

## 4. Risks & Roadmap

### Phased Rollout

**MVP (v0.1)**: Anthropic + OpenAI providers, non-streaming + streaming, virtual keys, Prometheus metrics, Docker image, Helm chart, access logs

**Planned**: Google Gemini provider, additional providers as needed (Mistral, Groq, etc.) — should be straightforward given the provider interface pattern

### Technical Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Anthropic API format changes | Breaks translation layer | Pin `anthropic-version` header; adapter is isolated |
| SSE streaming edge cases (partial chunks, connection drops) | Broken streams to client | Use `context.Context` propagation; client disconnect cancels upstream |
| Config errors at startup | Gateway won't start | Validate config at load time, fail fast with clear errors |
| High cardinality Prometheus labels | Memory growth in Prometheus | `app` and `model` are bounded by config; no unbounded labels |

---

## 5. Tech Stack

| Component | Choice | Rationale |
|---|---|---|
| Language | Go 1.22+ | Low memory, fast startup, stdlib HTTP server |
| HTTP routing | `net/http.ServeMux` | Go 1.22 added method-based routing; no framework needed |
| Logging | `log/slog` | Structured JSON logging in stdlib since Go 1.21 |
| YAML | `gopkg.in/yaml.v3` | Standard Go YAML library |
| Metrics | `github.com/prometheus/client_golang` | Official Prometheus client |
| UUIDs | `github.com/google/uuid` | Request ID generation |
| Container | Distroless base image | Minimal attack surface, small image |
| Orchestration | Helm chart | Standard k8s deployment |
