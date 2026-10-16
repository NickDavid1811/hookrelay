# Project overview and scope

[Español](es/CONTEXT_MASTER.md) · Documentation review: 2026-10-16.

HookRelay is a self-hosted webhook dispatcher and a backend engineering project. It receives events, persists delivery work, sends HTTP requests and records outcomes so that failures can be inspected and exhausted deliveries can be replayed.

## Implemented scope

The backend is a modular Go monolith with ports and adapters and a local SQLite v5 database. It supports authenticated clients, ownership isolation, atomic publication, publication idempotency per client, static and dynamic endpoints, signed outgoing requests, configurable workers and retry budgets, audited replay, retention, admission quotas, protected metrics and backup/restore tooling. The local Flow Lab demonstrates real requests and failure scenarios.

An event currently creates one delivery to one endpoint. There is no general delivery-list/search API: clients query known delivery IDs. Client provisioning uses startup configuration; dynamic endpoint creation is available through the business API.

## Guarantees and boundaries

| Behavior | Meaning |
| --- | --- |
| HTTP 202 | The publication transaction was accepted; it is not proof that the receiver processed the event. |
| Atomic publication | Event, delivery and idempotency receipt commit together. |
| Retry and recovery | Eligible work is retried within finite budgets; abandoned claims can be recovered after their lease expires. |
| Stable identifiers | Event and delivery IDs survive retries and replay; receivers use them to deduplicate effects. |
| HTTP 2xx | The receiver accepted the HTTP request; business completion is outside HookRelay's knowledge. |
| Persistence | SQLite WAL with FULL synchronization, subject to filesystem and hardware guarantees. |

Duplicates remain possible after a lost response, interrupted process, uncertain finalization or restoration of an old backup. There is no exactly-once guarantee, strict ordering, high availability or supported multi-instance active deployment. Finite attempts do not guarantee eventual reception. HookRelay does not make the publisher's business transaction atomic with publication; an application may need its own outbox.

IDs use 128 cryptographically random bits encoded as 32 hexadecimal characters. UUIDv7 was an early proposal, not the implemented format. Retry defaults, quotas and retention are documented in [configuration](CONFIGURATION.md); receiver behavior is specified in the [delivery contract](DELIVERY_CONTRACT.md).

## Intended use and maturity

The technical objective is to demonstrate Go, HTTP APIs, transactions, concurrency, security, failure recovery and operation with reproducible evidence. A possible commercial audience includes small teams maintaining SaaS or business-system integrations. Those are hypotheses: this repository does not establish customers, revenue or willingness to pay, and the project name has no documented trademark/domain clearance.

The repository contains tests for process interruption, duplicate effects, storage exhaustion, selected corruption and WAL I/O failures. Historical local benchmarks exist; they are not sustained-capacity measurements for the current worker/read-pool implementation. See [evidence](benchmarks/README.md) and [storage validation](STORAGE_FAILURES.md).

Production suitability still requires independent review and evidence for the actual deployment, including sustained load, external HTTPS delivery, TLS, secrets, alerting and restoration procedures. Physical power-loss and device/controller guarantees remain unproven by the software fault tests.

## Scope decisions

Current design keeps one dispatcher deployment and local SQLite. Broker integration, multiple active replicas, transformations, provider-specific connectors, billing, SSO, advanced roles and a full consumer portal are outside the initial scope. Rate controls, tenant fairness, circuit breakers, secret rotation and richer delivery search are possible extensions, not existing guarantees or commitments in this documentation revision.

This document replaces the initial mixed proposal/current-state context. Dated implementation decisions and corrections are retained in the [engineering history](history/SESSION.md). It does not authorize development, deployment or publication by itself.

Current priorities and acceptance criteria are tracked in the [engineering backlog](BACKLOG.md).
