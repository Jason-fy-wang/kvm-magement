# KVM MicroVM Management – Center Service Design

**Status:** Draft v0.1 (POC) · **Component:** `kvm-manager` · **Talks to:** Frontend (north), Agents (south)

> **\[Assumption\]** `kvm-manager` is the center service. Language and framework are not confirmed, so the design is stack-neutral; module names are logical, not package names.

## 1. Responsibilities

1. **API gateway for users:** REST API + SSE for the frontend, with authentication and authorization.
2. **Source of truth:** persist nodes, VMs, tasks, audit records.
3. **Node registry:** register agents, track liveness, store capacity.
4. **Scheduling:** choose a node for a new VM.
5. **Task orchestration:** turn user intent into tasks, dispatch to agents, handle retries/timeouts, record results.
6. **Status aggregation:** ingest heartbeats, derive node/VM state, detect drift, publish events.
7. **Audit:** record every mutating action.

The center never touches KVM directly.

## 2. Internal modules

```
+------------------------------------------------------------+
|                       Center Service                       |
|                                                            |
|  HTTP layer:  User API | Agent API | SSE events | Health   |
|        |            |         |          ^                  |
|  Auth middleware / request-id / error mapping              |
|        v            v         v          |                  |
|  VM service   Node service   Heartbeat ingest   Event bus   |
|        |            |             |               ^         |
|        v            |             v               |         |
|  Scheduler     Task service  <-> State reconciler           |
|                     |                                       |
|              Task worker pool --> Agent client (HTTP)       |
|                     |                                       |
|              Repository layer  -->  Database               |
+------------------------------------------------------------+
```

| Module | Role |
| --- | --- |
| User API | Handlers for `/api/v1/nodes`, `/vms`, `/tasks`, `/exec`, `/events`, auth |
| Agent API | Handlers for register, heartbeat, events; authenticates by node token |
| VM service | Validation, create/start/stop/delete business rules, state transitions |
| Node service | Registration, token issue, drain/undrain, capacity view |
| Scheduler | Pure function: (request, online nodes with free capacity) → node or "no capacity" |
| Task service + worker pool | Persisted queue; workers claim tasks, call agent, update results |
| Agent client | HTTP client with timeout, retry, auth token, request-id propagation |
| Heartbeat ingest | Upsert node, compare reported VM states with DB, update `observed_state` |
| Reconciler / monitor | Periodic jobs: offline detection, stuck-task sweeper, orphan detection |
| Event bus | In-process pub/sub feeding SSE subscribers |
| Repository | All DB access; transactions live here |

## 3. Data model

### 3.1 Tables

| Table | Columns (key ones) | Indexes / constraints |
| --- | --- | --- |
| `nodes` | id (uuid), name (unique), address, port, status, agent\_version, cpu\_total, mem\_total\_mb, disk\_total\_gb, cpu\_used, mem\_used\_mb, disk\_used\_gb, labels (json), token\_hash, last\_heartbeat\_at, created\_at | unique(name); idx(status) |
| `vms` | id (uuid), name (unique), node\_id (fk), desired\_state, observed\_state, vcpus, mem\_mb, disk\_gb, image, kernel, ip, mac, spec (json), last\_error, created\_by, created\_at, updated\_at, deleted\_at | unique(name) where not deleted; idx(node\_id); idx(observed\_state) |
| `tasks` | id (uuid), type, target\_type, target\_id, node\_id, status, params (json), result (json), error, attempts, max\_attempts, timeout\_s, created\_by, created\_at, started\_at, finished\_at | idx(status, created\_at); idx(target\_id) |
| `node_metrics` | node\_id, ts, cpu\_pct, mem\_used\_mb, disk\_used\_gb | idx(node\_id, ts); retention job |
| `audit_log` | id, user, action, target\_type, target\_id, detail (json), ts | idx(ts) |
| `users` | id, name, role, token\_hash / password\_hash | unique(name) |

Use soft delete (`deleted_at`) for VMs so history and task references stay valid.

### 3.2 State machines

**VM `observed_state`:** `creating → running ↔ stopped`, any → `error`, any → `unknown` (node offline), `deleting →` removed.

**Task `status`:** `pending → running → succeeded | failed | timeout`. A `running` task whose worker died is returned to `pending` by the sweeper (if attempts remain).

**Node `status`:** `online ↔ offline`; `draining` is set manually and blocks new scheduling.

## 4. External API (user-facing)

Base path `/api/v1`. JSON. Auth: `Authorization: Bearer <token>`.

| Method | Path | Description | Result |
| --- | --- | --- | --- |
| POST | `/auth/login` | Obtain token | 200 |
| GET | `/nodes` | List nodes | 200 |
| GET | `/nodes/{id}` | Node detail + latest metrics | 200 |
| GET | `/nodes/{id}/metrics?from&to` | History | 200 |
| POST | `/nodes/{id}/drain` / `/undrain` | Scheduling control | 200 |
| POST | `/nodes/{id}/exec` | Run command on node | 202 + task |
| GET | `/vms?state&node&q&page` | List VMs | 200 |
| POST | `/vms` | Create VM | 202 + `{vm_id, task_id}` |
| GET | `/vms/{id}` | VM detail | 200 |
| PATCH | `/vms/{id}` | Update metadata (name, labels); resize is post-POC | 200 |
| POST | `/vms/{id}/start` / `/stop` | Power operations | 202 + task |
| DELETE | `/vms/{id}` | Delete VM | 202 + task |
| GET | `/tasks`, `/tasks/{id}` | Task history / status | 200 |
| GET | `/events` | SSE stream | stream |
| GET | `/healthz` | Liveness | 200 |

**Create request example**

```json
{ "name": "web-01", "vcpus": 2, "mem_mb": 512, "disk_gb": 5,
  "image": "ubuntu-22-min", "node_id": null, "labels": {"env": "dev"} }
```

**Create response (202)**

```json
{ "vm_id": "6f1c...", "task_id": "a93b...", "state": "creating" }
```

**Error format**

```json
{ "code": "NO_CAPACITY", "message": "No online node can fit 2 vCPU / 512 MB", "request_id": "..." }
```

Common codes: `VALIDATION_ERROR` (400), `UNAUTHORIZED` (401), `FORBIDDEN` (403), `NOT_FOUND` (404), `CONFLICT` (409, name taken or invalid state transition), `NO_CAPACITY` (409), `AGENT_UNREACHABLE` (502), `INTERNAL` (500).

## 5. Agent-facing API

Authenticated with the node token.

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/api/v1/agent/register` | First contact: node name, address, capacity, agent version → returns node\_id (and token if using enrollment tokens) |
| POST | `/api/v1/agent/heartbeat` | Every 5 s: metrics + all local VMs `{vm_id, state, pid, uptime}` |
| POST | `/api/v1/agent/events` | Optional async notifications (VM crashed, task finished) |

**Heartbeat handling (single transaction per heartbeat):**

1. Update node row, status `online`, `last_heartbeat_at`.
2. For each reported VM: match by id; update `observed_state`, `ip`, etc.
3. VMs in DB for this node but **missing** from the report: if state is `creating`/`deleting` leave alone; otherwise mark `error` ("missing on host") after 2 consecutive misses.
4. VMs reported but **not in DB**: flag as `orphan` (visible in UI; do not auto-delete).
5. Publish events for any changes.

## 6. Center → agent calls

The agent client calls the agent's `/v1` API (see Agent doc). Rules:

- Always send `X-Request-Id` = task id (idempotency key) and the node token.
- Timeouts per call type (create 120 s, start/stop 30 s, delete 60 s, exec as requested + margin).
- Retry only on network errors/5xx, with exponential backoff and jitter; never retry on 4xx.
- Connection reuse; per-node concurrency limit (default 4) so one host isn't flooded.

## 7. Task engine

**Flow:** API handler inserts task (`pending`) → worker claims it atomically → marks `running` → calls agent → on success sets `succeeded` and updates the VM → on failure retries or sets `failed` and `last_error`.

**Claiming (DB-backed):**

- PostgreSQL: `SELECT ... FOR UPDATE SKIP LOCKED`.
- SQLite: single-writer transaction `UPDATE ... WHERE status='pending' ... RETURNING`.

**Ordering:** at most one active mutating task per VM (check on enqueue; otherwise `409 CONFLICT`).

**Sweeper (every 30 s):** requeue `running` tasks whose lease expired; fail tasks past `timeout_s` and mark the VM `error` or `unknown`.

## 8. Scheduling (v0)

1. Filter: node `online`, not `draining`, matches requested node/labels, has free vCPU/memory/disk (using reported usage + a configurable **overcommit ratio**, default 1.0 memory, 2.0 CPU).
2. Account for **in-flight** creates (reserved but not yet reported) so concurrent requests don't overbook.
3. Choose the node with the most free memory (tie-break on fewest VMs).
4. None found → `NO_CAPACITY`.

Keep it a separate pure function so a better policy can replace it.

## 9. Monitoring logic

| Job | Interval | Action |
| --- | --- | --- |
| Offline detector | 5 s | Node with no heartbeat for 15 s → `offline`; its VMs → `unknown` |
| Recovery | on heartbeat | Node back → `online`; VM states replaced by reported ones |
| Stuck task sweeper | 30 s | See section 7 |
| Metrics retention | hourly | Delete `node_metrics` older than 24 h (configurable) |
| Orphan check | on heartbeat | See section 5 |

SSE events published: `node.updated`, `vm.updated`, `task.updated`, `vm.orphan_detected`.

## 10. Security

- Password/token hashes only (argon2/bcrypt); never log tokens.
- Roles: `admin` (everything, including exec and force delete), `operator` (VM lifecycle), `viewer` (read only) – POC can ship just `admin`.
- Node tokens are per node and revocable; enrollment uses a one-time token set by the admin.
- Exec endpoint: admin only, command length and timeout limits, every call audited.
- Validate and normalise all inputs (names `[a-z0-9-]{1,63}`, numeric ranges); never interpolate input into shell commands.
- CORS limited to the frontend origin; TLS termination in front of the service (reverse proxy) is acceptable.

## 11. Configuration

| Key | Default | Meaning |
| --- | --- | --- |
| `listen_addr` | `:8080` | HTTP bind |
| `db_url` | – | Database connection |
| `heartbeat_interval_s` | 5 | Expected agent interval |
| `offline_after_s` | 15 | Offline threshold |
| `task_workers` | 8 | Worker pool size |
| `per_node_concurrency` | 4 | Parallel tasks per node |
| `cpu_overcommit` / `mem_overcommit` | 2.0 / 1.0 | Scheduler ratios |
| `metrics_retention_h` | 24 | History |
| `admin_bootstrap_token` | – | First admin |

## 12. Observability

- JSON logs with `request_id`, `task_id`, `vm_id`, `node_id`, `user`.
- `/metrics` counters: tasks by status, API latency, heartbeat lag per node, scheduler rejections.
- `/healthz` (liveness) and `/readyz` (DB reachable).

## 13. Testing

| Level | Focus |
| --- | --- |
| Unit | Scheduler, state transitions, validation, retry policy |
| Repository | Against a real DB (container or temp SQLite) |
| Component | Handlers + task engine with a **fake agent server** that can delay, fail, or duplicate |
| Integration | Real agent, real VM, scripted create → stop → delete |
| Failure | Kill agent mid-task, restart center with pending tasks, duplicate create |

## 14. Limitations and future work

- Single instance; reconciler and workers are not leader-elected (add before running multiple replicas).
- HTTP pull-style center → agent; an agent-initiated gRPC stream is the planned upgrade for NAT and lower latency.
- No image registry, no network/IP management beyond what the agent reports.
- Resize/update requires stop → change → start; defer.

## 15. Open questions

1. DB choice for the POC: SQLite (zero setup) or PostgreSQL (closer to production)?
2. Who allocates VM IPs/MACs – center (IP pool table) or agent (local bridge/DHCP)?
3. Should deleted VMs' disks be kept for a grace period?
4. Do we need multi-user accounts now, or is one shared admin enough?
