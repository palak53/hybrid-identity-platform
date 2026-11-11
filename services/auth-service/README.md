# auth-service

Login and token API for the identity platform: a minimal OIDC provider for the **customer** realm
(online and mobile banking) and the **workforce** realm (employee SSO).

## Status

| Version | Scope | State |
|---|---|---|
| v0 | Platform-ready HTTP server: config, probes, graceful shutdown, timeouts, JSON logs, request IDs | Done |
| v1 | Client credentials flow for the workforce realm: `/.well-known/openid-configuration`, `/jwks`, `/token`; PostgreSQL for clients and keys | Planned |
| v2 | Authorization code + PKCE, `/authorize`, `/login`, `/userinfo`; Redis sessions and rate limits; customer realm | Planned |
| v3 | Login events to Kafka; risk score from `signals-service`; step-up MFA | Planned |

v1 starts with client credentials because it needs no browser or UI. It is the smallest slice that
issues a real, verifiable token.

## Endpoints (v0)

| Endpoint | Probe | Behaviour |
|---|---|---|
| `GET /healthz` | Liveness | `200 ok` while the process can serve HTTP. Never checks dependencies |
| `GET /readyz` | Readiness | `200 ready` after startup; `503 not ready` before startup and during shutdown |

Every response carries an `X-Request-ID` header. A valid incoming ID is reused; otherwise one is generated.

## Configuration

All settings come from environment variables. Invalid values stop the service at startup and every
problem is reported at once.

| Variable | Default | Meaning |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. Probe requests are logged at `debug` |
| `DRAIN_DELAY` | `5s` | Time to keep serving after readiness turns false, while load balancers stop routing here |
| `SHUTDOWN_TIMEOUT` | `20s` | Maximum time for in-flight requests to finish |

Keep `DRAIN_DELAY + SHUTDOWN_TIMEOUT` below the pod's `terminationGracePeriodSeconds` (default 30s).

## Shutdown sequence

On `SIGTERM` (Kubernetes, systemd, ASG instance refresh) or `SIGINT`:

1. Readiness turns false → the pod is removed from Service endpoints.
2. Keep serving for `DRAIN_DELAY` → requests already routed here still succeed.
3. Stop accepting connections; wait up to `SHUTDOWN_TIMEOUT` for in-flight requests.
4. Exit 0. A second signal exits immediately.

## Dependency failure behaviour (planned)

| Dependency down | Effect |
|---|---|
| PostgreSQL | `/token` fails for new clients (503); readiness fails so traffic moves to healthy replicas. Liveness stays green |
| Redis | Rate limiting fails **closed** for the customer realm (security over availability) and **open** with stricter in-memory limits for workforce service tokens. Decision to be recorded in an ADR |

## Latency budget (target: p99 `/token` < 300 ms)

| Step | Budget |
|---|---|
| Network and TLS at the edge | 50 ms |
| Redis (rate limit, session) | 20 ms |
| PostgreSQL (client lookup) | 50 ms |
| JWT signing | 10 ms |
| Headroom (GC, queuing, retries) | 170 ms |

## Layout

```
cmd/auth-service/     main: wiring, startup, graceful shutdown
internal/config/      environment configuration and validation
internal/middleware/  request ID, structured logging, panic recovery
internal/server/      routes and probe handlers
```

## Run and test

```sh
go run ./cmd/auth-service
go test -race ./...
```
