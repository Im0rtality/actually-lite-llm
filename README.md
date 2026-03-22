# actually-lite-llm

A stateless Go LLM proxy gateway with an OpenAI-compatible API. Routes requests to OpenAI or Anthropic, translates formats transparently, and exposes per-app Prometheus metrics — without a database, UI, or 800MB of idle RAM.

## Why

LiteLLM does what this does, but it weighs ~800MB at idle and can't track usage per virtual key without a database. This project replaces it with a single stateless binary that fits in a sidecar.

## Features

- **OpenAI-compatible API** — use the standard OpenAI SDK, point `base_url` at the gateway
- **Multi-provider routing** — alias map + prefix rules route models to OpenAI or Anthropic
- **Format translation** — Anthropic's Messages API is translated transparently (system prompt extraction, stop sequences, token fields, streaming SSE chunks)
- **Virtual API keys** — per-app keys defined in YAML; unknown keys get 401, disallowed models get 403
- **Prometheus metrics** — `llm_requests_total`, `llm_tokens_total`, `llm_request_duration_seconds`, `llm_stream_first_byte_seconds`, `llm_provider_errors_total`, `llm_cost_dollars_total` — all labeled by `virtual_key`, `provider`, `model`
- **Structured access logs** — one-liner text via `log/slog`, including virtual key, provider, model, duration
- **Stateless** — horizontally scalable; no shared state between replicas
- **Small** — `< 50MB` idle RAM; distroless container image

## Quick Start

```bash
cp config.example.yaml config.local.yaml
# edit config.local.yaml — add your provider API keys and virtual keys

go run . -config config.local.yaml
```

Test it:

```bash
curl http://localhost:8080/health

curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer your-virtual-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}'
```

## Configuration

```yaml
listen: ":8080"

providers:
  openai:
    api_key: "${OPENAI_API_KEY}"        # env var interpolation supported
    base_url: "https://api.openai.com/v1"   # optional
  anthropic:
    api_key: "${ANTHROPIC_API_KEY}"
    base_url: "https://api.anthropic.com/v1" # optional

models:
  # Alias → upstream model + provider
  "gpt-4o":        { provider: "openai",     model: "gpt-4o" }
  "claude-sonnet": { provider: "anthropic",  model: "claude-sonnet-4-20250514" }
  "claude-haiku":  { provider: "anthropic",  model: "claude-haiku-4-5-20251001" }

routing:
  # Prefix-based fallback for models not in the alias map
  - prefix: "gpt-"    # any model starting with gpt- → OpenAI
    provider: "openai"
  - prefix: "claude-" # any model starting with claude- → Anthropic
    provider: "anthropic"

api_keys:
  - key: "${VKEY_BACKEND}"
    app: "my-backend"
    allowed_models: ["*"]      # wildcard = all models
  - key: "${VKEY_CHATBOT}"
    app: "chatbot"
    allowed_models: ["claude-sonnet", "gpt-4o"]
```

All `${}` values are expanded from environment variables at startup.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/chat/completions` | OpenAI-compatible chat completion (streaming + non-streaming) |
| `POST` | `/v1/responses` | OpenAI Responses API (streaming + non-streaming; translates to chat completions internally) |
| `GET`  | `/v1/models` | List models the caller's API key is permitted to use |
| `GET`  | `/health` | Liveness/readiness check |
| `GET`  | `/metrics` | Prometheus metrics |

## Prometheus Metrics

| Metric | Type | Labels |
|--------|------|--------|
| `llm_requests_total` | Counter | `virtual_key`, `provider`, `model`, `status` |
| `llm_request_duration_seconds` | Histogram | `virtual_key`, `provider`, `model`, `stream` |
| `llm_tokens_total` | Counter | `virtual_key`, `provider`, `model`, `direction` |
| `llm_stream_first_byte_seconds` | Histogram | `virtual_key`, `provider`, `model` |
| `llm_provider_errors_total` | Counter | `provider`, `error_type` |
| `llm_cost_dollars_total` | Counter | `virtual_key`, `provider`, `model` |

## Releases

Docker images and Helm charts are published automatically to GHCR on every `v*` tag push.

### Docker

```bash
docker pull ghcr.io/im0rtality/actually-lite-llm:0.1.0
# or latest patch of a minor
docker pull ghcr.io/im0rtality/actually-lite-llm:0.1
```

Run it:

```bash
docker run -p 8080:8080 \
  -e OPENAI_API_KEY=sk-... \
  -v $(pwd)/config.yaml:/etc/actually-lite-llm/config.yaml \
  ghcr.io/im0rtality/actually-lite-llm:0.1.0 \
  -config /etc/actually-lite-llm/config.yaml
```

### Helm chart (OCI)

```bash
helm install gateway oci://ghcr.io/im0rtality/charts/actually-lite-llm --version 0.1.0
```

With values:

```bash
helm install gateway oci://ghcr.io/im0rtality/charts/actually-lite-llm \
  --version 0.1.0 \
  --set image.tag=0.1.0 \
  -f my-values.yaml
```

Or install from a local values file:

```yaml
# my-values.yaml
config:
  inline: |
    listen: ":8080"
    providers:
      openai:
        api_key: "${OPENAI_API_KEY}"
    ...
```

Enable the Prometheus `ServiceMonitor` (requires kube-prometheus-stack):

```yaml
serviceMonitor:
  enabled: true
  interval: 30s
```

## Kubernetes / Helm (local chart)

```bash
helm install gateway ./helm/actually-lite-llm \
  --set config.existingSecret=my-gateway-config \
  --set extraEnv[0].name=OPENAI_API_KEY \
  --set extraEnv[0].valueFrom.secretKeyRef.name=provider-keys \
  --set extraEnv[0].valueFrom.secretKeyRef.key=openai
```

## Docker (local build)

```bash
docker build -t actually-lite-llm .
docker run -p 8080:8080 \
  -e OPENAI_API_KEY=sk-... \
  -v $(pwd)/config.yaml:/etc/actually-lite-llm/config.yaml \
  actually-lite-llm -config /etc/actually-lite-llm/config.yaml
```

## Development

```bash
go test ./...
go build .
go vet ./...
```

## Planned Features

- Google Gemini provider
- Additional providers (Mistral, Groq, etc.)
