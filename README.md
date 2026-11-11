# Hybrid Identity Platform

A reference implementation of an identity platform that runs across **on-premises VMs, AWS, and Azure**,
built on a shared Kubernetes platform runtime.

It models a common enterprise journey: identity services that started on fixed-capacity on-prem servers,
moved to the cloud in stages (rehost to EC2, then replatform to EKS and AKS), and now need to be
elastic, resilient to the loss of a cloud provider, secure by default, and measured against SLOs.

> Status: early development. See the [roadmap](#roadmap).

---

## Scenario

A large financial institution runs three identity capabilities that every digital channel depends on:

| Capability | Users | Service |
|---|---|---|
| Customer login (web and mobile) | Customers | `auth-service` |
| Workforce single sign-on | Employees | `auth-service` (workforce realm) |
| Joiner / mover / leaver provisioning | HR, employees | `lifecycle-service` |

The platform must:

- Absorb a **10x credential-stuffing spike** without an outage.
- Keep serving logins if **an entire cloud provider** becomes unreachable.
- Revoke a leaver's access across downstream apps **within 15 minutes**.
- Let teams deploy any service to **EKS, AKS, EC2, or on-prem VMs** the same way.
- Keep workforce identity **on-premises** for data-residency reasons.

## Service level objectives

| SLI | Objective |
|---|---|
| Login availability | 99.95% of valid requests succeed (30 days) |
| Token latency | 99% of `/token` requests under 300 ms |
| Leaver deprovisioning | 99% of terminations fully revoked within 15 minutes |

---

## Architecture

```
        Route 53 (public customer login:            Internal DNS (workforce realm)
        latency + health-check failover)
                 ┌──────────────────────┐                       │
                 ▼                      ▼                       ▼
        AWS ca-central-1          Azure canadacentral      On-premises data centre
        ┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐
        │ EKS              │     │ AKS              │     │ HAProxy ─► VMs   │
        │  auth, signals,  │     │  auth, signals,  │     │  auth-service    │
        │  lifecycle,Kafka │     │  lifecycle       │     │  (workforce)     │
        │ EC2 ASG (rehost, │     │ PostgreSQL       │     │ PostgreSQL       │
        │  being retired)  │     │  (logical replica│     │ Redis            │
        │ RDS PostgreSQL   │◄────┤   / DR)          │     └────────┬─────────┘
        │ ElastiCache Redis│     │ Azure Cache Redis│              │
        └────────┬─────────┘     └────────┬─────────┘              │
                 └──── service mesh (mTLS) / private connectivity ─┘
```

## Components

| Component | Language | Purpose |
|---|---|---|
| `services/auth-service` | Go | Minimal OIDC provider: authorization code + PKCE, client credentials, JWKS, step-up MFA |
| `services/signals-service` | Go | Consumes login events from Kafka, scores risk (credential stuffing, brute force, impossible travel), streams to SIEM. Scales with KEDA on consumer lag |
| `services/lifecycle-service` | Go | SCIM 2.0 joiner/mover/leaver provisioning on Knative, with retries, idempotency, circuit breakers, dead-letter queue |
| `platform/layer0-operator` | Go | Kubernetes operator: one `PlatformService` manifest generates workloads, autoscaling, PDBs, network policy, SLO alerts, and canary config; provisions cloud dependencies; enforces guardrails |
| `platform/opsbot` | Go / Python | Read-only incident assistant using MCP tools, with human approval and audit logging |
| `infra/` | Terraform | AWS, Azure, and on-prem infrastructure as code, per environment |

## Tech stack

**Languages:** Go, Python
**Compute:** Amazon EKS, Azure AKS, Amazon EC2, on-prem VMs
**Infrastructure as code:** Terraform
**Delivery:** GitHub Actions, Helm, Argo CD, Argo Rollouts
**Scaling:** HPA, KEDA, Knative, Karpenter
**Data:** PostgreSQL, Redis, Kafka
**Networking:** Gateway API, Linkerd, Route 53
**Observability:** Prometheus, Grafana, OpenTelemetry, Datadog
**Security:** Kyverno, cosign, External Secrets, AWS Secrets Manager, Azure Key Vault
**Testing:** Go tests, k6 load testing
**Cost:** OpenCost

## Environments

| | Staging | Production |
|---|---|---|
| AWS | 1 EKS cluster, spot nodes | EKS across 3 AZs, Multi-AZ RDS |
| Azure | 1 AKS cluster | AKS across zones |
| On-prem | 1 VM | 2 VMs behind HAProxy |
| Deploys | Auto-sync from `main` | Promotion by PR, canary gated on SLOs and error budget |

Cloud environments are ephemeral: created for a session and destroyed afterwards.

---

## Roadmap

- [x] Repository structure
- [ ] `auth-service`: platform-ready HTTP server (probes, graceful shutdown, timeouts, structured logs)
- [ ] `auth-service`: OIDC flows with PostgreSQL and Redis; container image; local Kubernetes
- [ ] On-prem VMs with HAProxy
- [ ] Terraform: AWS network, EC2 (rehost), EKS
- [ ] Terraform: AKS; same deployment to Azure
- [ ] Hybrid connectivity, service mesh, global routing
- [ ] Kafka, `signals-service` with KEDA, `lifecycle-service` on Knative
- [ ] Observability, SLOs, burn-rate alerts, error-budget gate
- [ ] `layer0-operator`: platform runtime, cloud dependencies, guardrails, readiness scorecard
- [ ] Security hardening: mTLS, admission policies, signed images, threat model
- [ ] Performance and cost: load tests, profiling, Karpenter, OpenCost
- [ ] Migration: EC2 → EKS/AKS with weighted traffic; production canaries
- [ ] Chaos game days and postmortems
- [ ] `opsbot` incident assistant

## Repository layout

```
services/   application services (Go)
platform/   platform runtime: operator, charts, opsbot
infra/      Terraform for AWS, Azure, on-prem
docs/adr/   architecture decision records
```
