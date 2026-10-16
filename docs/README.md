# HookRelay documentation

[Español](es/README.md) · Documentation review: 2026-10-16. Code reference: `35c4373`.

HookRelay accepts events through an authenticated HTTP API, stores delivery work in SQLite and sends signed webhooks in the background. This documentation explains the implementation, its operating procedures and the evidence behind its limits.

## Start here

| Your goal | Read |
| --- | --- |
| Look up technical vocabulary | [Glossary](GLOSSARY.md) |
| Understand scope and guarantees | [Project overview](CONTEXT_MASTER.md) |
| Run a local example | [Getting started](GETTING_STARTED.md) |
| Understand packages, transactions and data | [Architecture](ARCHITECTURE.md) |
| Configure clients, destinations and limits | [Configuration](CONFIGURATION.md) |
| Integrate publishers and receivers | [Delivery contract](DELIVERY_CONTRACT.md), [OpenAPI](openapi.yaml) |
| Operate, back up and restore | [Operations](OPERATIONS.md) |
| Investigate storage failures | [Storage failure validation and recovery](STORAGE_FAILURES.md) |
| Change code and reproduce checks | [Maintainer guide](MAINTAINING.md) |
| Use the local visual demonstration | [Flow Lab](PLAYGROUND.md) |
| Review past changes and measurements | [Engineering history](history/SESSION.md), [backend review](history/AUDIT-2026-10-15.md), [UX review](history/PLAYGROUND_UX.md), [benchmarks](benchmarks/README.md) |

New developers should read the overview, getting started, architecture and maintainer guide in that order. Operators should read configuration, operations and storage failures. Auditors should start with guarantees, the delivery contract and the dated evidence; a reported PASS describes its recorded execution only.

## Organization and authority

Current guides stay at this level. `history/` contains dated records; `benchmarks/` contains historical reports and unchanged measurement JSON. This small structure separates instructions from evidence without scattering related guides.

English is the primary editorial version. [The complete Spanish edition](es/README.md) mirrors all guides, historical reports and OpenAPI descriptions. Commands, paths, JSON keys, HTTP fields, states and identifiers remain unchanged. Raw benchmark JSON is shared by both editions to preserve a single original record; each Spanish report links directly to it and explains the measurements in Spanish.

Implementation sources resolve factual questions: handlers and application code define behavior, migrations define schema, `go.mod` and the web lockfile define dependencies, and the Makefile/workflows define checks. OpenAPI documents the HTTP contract. A mismatch is a documentation or implementation defect to reconcile, not permission to silently reinterpret behavior.

## Evidence and maintenance

This revision reorganizes and checks documentation against code reference `35c4373`; it does not rerun the historical load, fault, Docker or CI experiments. Historical results retain their dates, baseline references and limitations. No production certification or new performance guarantee is implied.

Update both languages when behavior changes. Keep one main explanation per topic and link to it elsewhere. Preserve raw measurements. Record commands, exact revision, environment and remaining limits for new evidence. The history is consolidated, not a verbatim transcript; the full earlier documentation remains in Git at `35c4373`.
