# KVM MicroVM Management – Implementation Plan

**Status:** Draft v0.1 (POC) · **Companion to:** Architecture doc

> **Assumption:** the agent's VM *create* routine already works (manually tested). The plan therefore starts by wrapping it in an API rather than building it. Durations are rough, for one developer; shrink them if you have more people.

## 1. Approach

- **Vertical slices, not layers.** Get one VM created from the UI end to end as early as possible, then widen.
- **Contract first.** Agree the center API and agent API (OpenAPI) before coding the three components, so frontend and agent can be built in parallel against mocks.
- **Agent stays thin.** Anything that can live in the center, does.

## 2. Milestones overview

| # | Milestone | Outcome | Est. |
| --- | --- | --- | --- |
| M0 | Foundations | Repo layout, API contracts, dev environment, CI skeleton | 2–3 d |
| M1 | Agent API + registration | Agent exposes create/start/stop/delete/list; registers and heartbeats to center | 4–5 d |
| M2 | Center core | Node registry, VM + task tables, REST API, task worker calling agents | 5–7 d |
| M3 | Frontend MVP | Node list, VM list, create form, VM detail, task feedback | 4–5 d |
| M4 | Status monitoring | Heartbeat metrics, offline detection, live updates (SSE), drift fix on agent restart | 3–4 d |
| M5 | Command execution | Exec API with limits + audit, UI console panel | 3–4 d |
| M6 | Hardening and demo | Auth, error handling, logs, packaging, docs, demo script | 4–5 d |

**Total:** roughly 5–7 weeks for one person; M3 can overlap M2 once contracts exist.

## 3. Milestone details

### M0 – Foundations

- Decide and record: language/framework per component, DB, runtime (Firecracker or other), network model. *(See open questions in the Architecture doc.)*
- Write OpenAPI for:
  - Center REST (`/api/v1`)
  - Agent REST (`/v1`)
  - Agent → center calls (register, heartbeat, events)
- Repo layout proposal: `kvm-manager/`, `kvm-agent/`, `kvm-frontend/` (new), `api/` (specs), `docs/`, `deploy/`.
- Dev setup: one Linux host (or nested-virt VM) with `/dev/kvm`; `make dev` / `docker compose` for center + DB.
- CI: lint, unit tests, build for each component.

**Exit criteria:** specs reviewed; every component builds and runs "hello" locally.

### M1 – Agent API and registration

Tasks:

1. Refactor the working create function into a package/module with a clear interface (`CreateVM(spec) -> VM`), no hidden globals, returns typed errors.
2. Add `Start`, `Stop`, `Delete`, `Get`, `List`, backed by a local VM registry (directory + metadata file per VM, or SQLite).
3. HTTP server with token auth, request validation, `request_id` idempotency cache.
4. State probing: determine real state from the runtime (process alive, API socket reachable) rather than trusting stored metadata.
5. Register + heartbeat client (config: center URL, token, interval).
6. Host facts: CPU, memory, disk free, agent version.
7. Systemd unit + sample config.

**Exit criteria:** with `curl` only, create → start → stop → delete a VM on a host, and see heartbeats arrive at a stub receiver.

### M2 – Center core

Tasks:

1. DB schema + migrations (`nodes`, `vms`, `tasks`, `audit_log`, `users`).
2. Agent-facing endpoints: register (issues/validates node token), heartbeat (upserts node + VM states), events.
3. User-facing endpoints: nodes (list/get), VMs (CRUD + start/stop), tasks (list/get).
4. Scheduler v0: filter online nodes with enough free vCPU/mem/disk → choose the one with most free memory.
5. Task worker: pick pending tasks, call agent, handle timeout/retry/backoff, update VM + task state.
6. Auth v0: static admin token or basic login + JWT.
7. Consistent error format and request-id middleware.

**Exit criteria:** a script using only the center API can create/list/stop/delete VMs on a real agent; tasks show correct terminal states, including on agent failure.

### M3 – Frontend MVP

Tasks:

1. Project scaffold, routing, API client generated from OpenAPI, auth guard.
2. Pages: Dashboard (counts), Nodes, VMs (table with filters), VM detail, Create VM form, Tasks.
3. Task feedback: after create/start/stop/delete, show a pending toast and update the row when the task finishes (polling at first).
4. Error and empty/loading states.

**Exit criteria:** a non-developer can create, stop, and delete a VM from the browser.

### M4 – Status monitoring

Tasks:

1. Center: offline detection job (3 missed heartbeats), mark VMs `unknown`, and restore on next heartbeat.
2. Store a short history of node metrics (e.g. 24 h, downsampled) for charts.
3. SSE endpoint `/api/v1/events` for node/VM/task changes.
4. Frontend: live status badges, node resource bars, small CPU/memory sparkline.
5. Agent restart scenario: re-scan local VMs, report, and center reconciles (adopt unknown VMs as `orphan`, or mark missing ones `error`).

**Exit criteria:** kill the agent → UI shows node offline and VMs unknown within \~20 s; restart → states recover with no manual action.

### M5 – Command execution

Tasks:

1. Agent: exec endpoint with timeout, output size cap, working dir, and either an allowlist or admin-only gate.
2. Center: exec task type, store result, audit entry.
3. Frontend: command panel on node detail with history and output viewer.
4. Decide on guest execution (vsock agent / SSH). If deferred, document it as a known limit.

**Exit criteria:** run a command on a node from the UI, see output, see an audit record; over-limit commands are cut off safely.

### M6 – Hardening and demo

Tasks:

- Per-node token rotation, TLS on center, secrets via env/file not code.
- Input validation review, failure-injection tests (network drop, agent kill, disk full).
- Structured logs everywhere; `/healthz`; optional `/metrics`.
- Packaging: agent as a release binary + systemd install script; center + DB via docker compose.
- README with quick start, plus a 10-minute demo script.

**Exit criteria:** a fresh host can be added in under 10 minutes following the README.

## 4. Work breakdown by component

| Component | M0 | M1 | M2 | M3 | M4 | M5 | M6 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Agent | contract | **main** | – | – | rescan/report | exec | harden |
| Center | contract | stub | **main** | – | monitor/SSE | exec task | harden |
| Frontend | contract | – | mock | **main** | live UI | console | polish |

## 5. Testing strategy

| Level | What | How |
| --- | --- | --- |
| Unit | Validation, scheduler, state mapping, task retry logic | Standard test framework per language |
| Component | Center with a **fake agent**; agent with a **fake runtime** | HTTP stubs / interface mocks |
| Integration | Center + real agent + real microVM | Dedicated KVM test host, scripted flow |
| UI | Smoke tests of key flows | Playwright or Cypress against mock API |
| Failure | Agent down mid-task, duplicate requests, center restart | Scripted chaos checks |

**Rule:** every bug found on the real host gets a regression test at the lowest level that can reproduce it.

## 6. Environments

- **Dev:** laptop for center/frontend with a fake agent; one Linux box with KVM for the real agent.
- **Staging/POC:** 1–2 KVM hosts + one center host.
- Configuration only via env/config files; no environment-specific code.

## 7. Risks and mitigations

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Create routine has hidden assumptions (paths, root, network) | Hard to wrap as a service | Do the M1 refactor first; document required host setup |
| State drift between DB and reality | Wrong UI, leaked VMs | Observed state from agent + rescan on restart + orphan detection |
| Long-running operations time out | Stuck tasks | Async tasks, timeouts, retry, and a manual "mark failed" admin action |
| Command exec abused | Security incident | Off by default, admin-only, audited, limits |
| Scope creep (migration, images, SDN) | Never finishes the POC | Keep a "later" list; review at the end of each milestone |
| Nested virtualization unavailable in dev | Can't test | Reserve one bare-metal or nested-capable host early |

## 8. Definition of done (POC)

- [ ] Create, start, stop, delete a microVM from the UI on at least 2 hosts
- [ ] Node and VM status visible within \~10 s, offline detection within \~20 s
- [ ] Command execution on a node with audit trail
- [ ] Survives agent restart and center restart without manual repair
- [ ] README + demo script; all three components build via CI

## 9. Post-POC backlog

- Update/resize VM, snapshots, live migration
- Image/kernel registry and caching
- Network management (IP pool, security groups)
- Multi-user RBAC, SSO
- Agent → center streaming channel (gRPC) and mTLS
- Metrics/alerts integration (Prometheus + Alertmanager)
- Center HA (multiple instances with leader-elected reconciler)
