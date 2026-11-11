# auth-service

Login and token API for the identity platform: a minimal OIDC provider for the **customer** realm
(online and mobile banking) and the **workforce** realm (employee SSO).

**Stack:** Python 3.13, Django 5.2 LTS, Gunicorn (gthread workers).

## Status

| Version | Scope | State |
|---|---|---|
| v0 | Platform-ready service: validated config, probes, JSON logs, request IDs, Gunicorn tuned for Kubernetes | Done |
| v1 | Client credentials flow for the workforce realm: `/.well-known/openid-configuration`, `/jwks`, `/token`; PostgreSQL for clients and keys | Planned |
| v2 | Authorization code + PKCE, `/authorize`, `/login`, `/userinfo`; Redis sessions and rate limits; customer realm | Planned |
| v3 | Login events to Kafka; risk score from `signals-service`; step-up MFA | Planned |

v1 starts with client credentials because it needs no browser or UI. It is the smallest slice that
issues a real, verifiable token. Build vs buy for the OIDC logic (`django-oauth-toolkit` vs
hand-written) will be recorded in an ADR.

## Endpoints (v0)

| Endpoint | Probe | Behaviour |
|---|---|---|
| `GET /healthz` | Liveness | `200 ok` while the process can serve HTTP. Never checks dependencies |
| `GET /readyz` | Readiness | `200` when every readiness check passes, `503` otherwise, with per-check detail in JSON |

Probes are answered by middleware **before** Django's host validation. Kubernetes probes connect to
the pod IP, so their `Host` header is not in `ALLOWED_HOSTS`; without this they would get `400` and the
pod would never become ready. All other paths still enforce `ALLOWED_HOSTS`.

Every response carries an `X-Request-ID` header. A valid incoming ID is reused; otherwise one is generated.
Every log line written during a request includes it.

## Configuration

All settings come from environment variables, validated in [authsvc/config.py](authsvc/config.py) and
shared by Django and Gunicorn. Invalid values stop the service at startup, and every problem is reported at once.

| Variable | Default | Meaning |
|---|---|---|
| `DEBUG` | `false` | Local development only |
| `DJANGO_SECRET_KEY` | none | Required unless `DEBUG`; at least 32 characters. Comes from a secret store, never from git |
| `ALLOWED_HOSTS` | none | Required unless `DEBUG`; comma-separated |
| `LOG_LEVEL` | `INFO` | `DEBUG`, `INFO`, `WARNING`, `ERROR`. Probe requests are logged at `DEBUG` |
| `PORT` | `8080` | HTTP port |
| `WEB_CONCURRENCY` | `2` | Gunicorn worker processes |
| `GUNICORN_THREADS` | `4` | Threads per worker |
| `REQUEST_TIMEOUT` | `30` | Seconds before a stuck worker is replaced |
| `GRACEFUL_TIMEOUT` | `20` | Seconds in-flight requests get to finish after SIGTERM |
| `KEEPALIVE` | `75` | Seconds; must exceed the load balancer idle timeout (ALB default 60) to avoid random 502s |

## Graceful shutdown

Gunicorn handles SIGTERM: it stops accepting connections and gives in-flight requests
`GRACEFUL_TIMEOUT` seconds. Gunicorn runs several processes, so a shared "not ready" flag is
awkward. Draining is handled by each runtime instead, which keeps it the same for every service
regardless of language:

| Runtime | How traffic is drained before SIGTERM |
|---|---|
| Kubernetes (EKS, AKS) | `preStop` sleep (about 5s) while the pod is removed from endpoints. Set by the platform, not the service |
| EC2 Auto Scaling | ALB target deregistration delay + ASG lifecycle hook |
| On-prem VMs | `vm-agent` sets the server to drain in HAProxy, then stops the service |

Keep `preStop + GRACEFUL_TIMEOUT` below the pod's `terminationGracePeriodSeconds` (default 30s).

## Dependency failure behaviour (planned)

| Dependency down | Effect |
|---|---|
| PostgreSQL | Readiness fails, so traffic moves to healthy replicas. Liveness stays green, so pods are not restarted |
| Redis | Rate limiting fails **closed** for the customer realm (security over availability) and **open** with stricter in-process limits for workforce service tokens. Decision to be recorded in an ADR |

## Latency budget (target: p99 `/token` < 300 ms)

| Step | Budget |
|---|---|
| Network and TLS at the edge | 50 ms |
| Redis (rate limit, session) | 20 ms |
| PostgreSQL (client lookup) | 50 ms |
| JWT signing | 10 ms |
| Headroom (queuing, retries) | 170 ms |

## Layout

```
authsvc/config.py      environment configuration and validation (no Django imports)
authsvc/settings.py    Django settings, all values from config
authsvc/jsonlog.py     JSON log formatter; stamps the request ID on every record
core/middleware.py     request ID, access log, probe handling
core/probes.py         readiness check registry
core/tests.py          tests
gunicorn.conf.py       production server settings
```

## Run and test

```sh
python3.13 -m venv .venv && .venv/bin/pip install -r requirements.txt

# development
DEBUG=true .venv/bin/python manage.py runserver 8080

# production-like
DJANGO_SECRET_KEY=<32+ chars> ALLOWED_HOSTS=localhost .venv/bin/gunicorn authsvc.wsgi

# tests
DEBUG=true .venv/bin/python manage.py test
```
