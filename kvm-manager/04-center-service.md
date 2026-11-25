# KVM MicroVM Management – Center Service Design (`kvm-manager`)

**Status:** Draft v0.2 (POC) · **Based on:** `kvm-manager` at commit `95bf803` of `Jason-fy-wang/kvm-magement`

Self-contained design for the center service. Findings are labelled **verified** (reproduced by running a test) or **from code** (read in the source, not run).

**Context.** The system has three layers: frontend → center service (`kvm-manager`) → agent (`kvm-agent`, one per KVM host, runs one Firecracker microVM process per VM). The manager is the control plane: it never touches KVM or Firecracker directly.

---

## 1. Responsibilities

| Responsibility | Status |
|---|---|
| REST API for the UI (`/api/v1`) | Built |
| Agent registry over WebSocket `/ws/agents` | Built, **broken** (section 5) |
| VM, operation and IP persistence in SQLite | Built |
| Agent selection | Built, basic (section 8) |
| IP and MAC allocation from a shared CIDR | Built |
| Forward lifecycle calls to the agent, track operations | Built |
| Event fan-out to the UI over WebSocket | Built |
| Console proxy to the agent | Built |
| Liveness (heartbeat, offline detection) | Missing |
| Reconciliation with agent snapshots | Missing |
| Authentication, audit | Missing |
| Exec, VM metadata update | Missing |

---

## 2. Code layout

| File | Contents |
|---|---|
| `cmd/kvm-manager/main.go` | Flags: `-listen :8080`, `-db kvm-manager.db`, `-network-cidr 172.20.0.0/16`, `-log-level` |
| `internal/manager/server.go` (~600 lines) | Routes, handlers, agent REST client, scheduler, agent WebSocket, event hub, console proxy |
| `internal/manager/store.go` | SQLite schema, migrations, queries (single connection) |
| `internal/manager/network.go` | IP and MAC allocator |
| `internal/manager/model.go` | `Agent`, `VM`, `CreateVMRequest`, `Operation`, `Event`, `NetworkAllocation` |
| Tests | One server smoke test; one allocator test |

Stack: Go, standard `net/http` mux, `gorilla/websocket`, `log/slog`, `modernc.org/sqlite`.

**Refactor suggestion:** split `server.go` into `handlers_vm.go`, `agent_ws.go`, `agent_client.go`, `scheduler.go`, `events.go`, and add `reconcile.go`. No behaviour change.

---

## 3. Data model

### As built (SQLite)

| Table | Columns |
|---|---|
| `agents` | `id` PK, `hostname`, `url`, `status`, `vcpu_total`, `vcpu_available`, `disk_total_bytes`, `disk_free_bytes`, `last_seen` |
| `vms` | `id` PK, `name`, `agent_id` FK, `state`, `vcpu_count`, `memory_mib`, `kernel_path`, `rootfs_path`, `disk_path`, `tap_device`, `guest_ip`, `guest_mac`, `gateway_ip`, `netmask`, `created_at`, `updated_at` |
| `ip_allocations` | `ip` PK, `vm_id` unique, `agent_id`, `mac` unique, `gateway_ip`, `netmask`, `status` |
| `operations` | `id` PK, `vm_id`, `agent_id`, `agent_operation_id`, `type`, `status`, `error`, `created_at`, `updated_at`; index on `(agent_id, agent_operation_id)` |

Migrations are `CREATE TABLE IF NOT EXISTS` plus an add-column helper.

### Additions

| Change | Why |
|---|---|
| `agents`: `mem_total_mib`, `mem_available_mib`, `agent_version` | Schedule on memory; show versions |
| `vms`: `last_error`, `labels` (json), unique index on `name` where state is not `deleted` | Error display, metadata, no duplicates |
| `operations`: `timeout_at` | Timeout sweeper |
| `audit_log(id, user, action, target, detail, ts)` | Who did what; needed for exec |
| Token or `users` table | Authentication |

---

## 4. State model

**VM state today:** `creating`, `stopped`, `running`, `error`, `deleted`. State changes only from agent events (`create` and `stop` → `stopped`; `start` and `reboot` → `running`; `delete` → `deleted`) or to `error` when an agent call fails.

**Target additions**

- `unknown` when the owning agent is offline; restored from the next snapshot.
- `orphan` flag for VMs the agent reports but the manager does not know.
- `deleted` rows hidden from lists, or removed once the IP is released.
- Operation states: `queued`, `running`, `succeeded`, `failed`, `timeout`.

---

## 5. Agent WebSocket protocol

**Register (what the agent sends today)**

```json
{"type":"agent.register","agent_id":"host-1","url":"http://10.0.0.11:9090","timestamp":"..."}
```

**Bug (verified).** The manager decodes this into an anonymous struct without JSON tags (`Type, AgentID, Hostname, URL, VCPUTotal, …`). Go does not equate `agent_id` with `AgentID`, so `AgentID` stays empty; the handler then returns without registering the agent. Reproduced by running the same struct against the same message. Fix with explicit tags and add a test that dials the real handler:

```go
type agentRegistration struct {
    Type           string `json:"type"`
    AgentID        string `json:"agent_id"`
    Hostname       string `json:"hostname"`
    URL            string `json:"url"`
    AgentVersion   string `json:"agent_version"`
    VCPUTotal      int    `json:"vcpu_total"`
    VCPUAvailable  int    `json:"vcpu_available"`
    MemTotalMiB    int64  `json:"mem_total_mib"`
    MemAvailMiB    int64  `json:"mem_available_mib"`
    DiskTotalBytes int64  `json:"disk_total_bytes"`
    DiskFreeBytes  int64  `json:"disk_free_bytes"`
}
```

**Heartbeat (target, every 5 s)**

```json
{"type":"agent.heartbeat","agent_id":"host-1",
 "host":{"vcpu_total":16,"vcpu_available":10,"mem_total_mib":64000,"mem_available_mib":40000,
         "disk_total_bytes":5e11,"disk_free_bytes":3e11},
 "vms":[{"id":"vm-ab12","state":"running","pid":4321}]}
```

Handling: update capacity and `last_seen`, set the agent `online`, reconcile the VM list (section 7).

**Snapshot:** the same VM list, sent once after every (re)connect.

**Events (as built):** `operation.created`, `operation.updated`, `vm.status_changed`. The manager matches `operation.updated` to its own operation by `(agent_id, agent_operation_id)`.

**Reconnect bug (from code).** When an agent reconnects while its old socket is still open, the old handler's deferred cleanup marks the agent `offline` after the new registration. Track a connection generation per agent and mark it offline only if the closing connection is the current one.

---

## 6. Operation flow

1. A handler validates the request and loads the VM.
2. Insert a manager operation (`queued`).
3. Call the agent's REST endpoint; the agent replies `202` with its operation id.
4. Store `agent_operation_id`, set `running`, return `202` to the UI.
5. The agent's `operation.updated` event arrives; the manager sets the operation status and, on success, the VM state.

**Race (from code).** The agent can finish and send its event before step 4 stores the id; the event is then dropped as unknown. **Fix:** the manager creates the operation id and sends it to the agent, which uses it as its own, so no binding step is needed.

**Other fixes**

- Use an `http.Client` with timeouts instead of `http.DefaultClient`.
- Check errors that are currently discarded (`_ =`) on state updates.
- Apply the VM state change and the operation change in one transaction.

---

## 7. Reconciliation (target)

Run on every heartbeat or snapshot from one agent.

| Case | Action |
|---|---|
| VM in both, same state | Nothing |
| VM in both, different state, no active operation | Set the manager state to the reported one |
| VM in both, active operation | Wait; the operation result decides |
| VM in manager, missing from agent, not `creating` or `deleting` | After two consecutive misses set `error` ("missing on host") |
| VM on agent, unknown to manager | Insert as `orphan`; show in UI; never auto-delete |

**Offline detector (every 5 s):** agents with `last_seen` older than 15 s become `offline`; their VMs become `unknown`.

**Operation sweeper (every 30 s):** operations still `running` after their timeout become `failed`; the VM becomes `error`.

---

## 8. Scheduling

**As built (from code).** Use the requested agent if it is online; otherwise the online agent with the highest score `vcpu_available * 1_000_000 + disk_free_bytes`. An agent with `vcpu_available == 0` is accepted because 0 is treated as unknown. Memory is not considered, and the disk check only tests whether a disk path was supplied.

**Target**

1. Online and not draining.
2. Capacity reported and sufficient for vCPU, memory and disk.
3. Subtract resources of in-flight creates.
4. Prefer the most free memory, then fewest VMs.
5. Keep it a pure function with unit tests.

---

## 9. Network allocator

- CIDR from `-network-cidr` (default `172.20.0.0/16`); gateway is the first address; addresses ending in `.255` are skipped.
- The allocator scans from the first address and checks each in the database: fine for hundreds of VMs, slow for a nearly full /16. Optimisation: ask the database for the lowest free address.
- MAC is `06:00:` plus the four IP bytes, so MAC uniqueness follows IP uniqueness.
- A mutex serialises allocation; the unique index on `ip` is the real guard.
- Released on delete success and on create failure. If the manager crashes between allocation and VM insert the IP leaks; add a startup cleanup for allocations without a VM.
- The CIDR is cluster-wide; each host needs a bridge that carries it. Decide the multi-host network model before adding hosts.

---

## 10. API

### As built (`/api/v1`)

| Method | Path | Notes |
|---|---|---|
| GET | `/healthz` | |
| GET | `/agents` | |
| GET, POST | `/vms` | POST returns `202 {operation_id, vm_id, status}` |
| GET, DELETE | `/vms/{id}` | DELETE returns `202` + operation |
| POST | `/vms/{id}/start`, `/stop`, `/reboot` | `202` + operation |
| GET | `/operations/{id}` | |
| WS | `/events` | UI event stream |
| WS | `/vms/{id}/console` | Proxied to the agent |
| WS | `/ws/agents` (no `/api/v1` prefix) | Agent connection |

**Create body:** `name, agent_id?, vcpu_count, memory_mib (≥128), kernel_path, rootfs_path, disk_path?, tap_device`. Errors are plain text with HTTP status.

### To add

| Method | Path | Purpose |
|---|---|---|
| GET | `/agents/{id}` | Detail with metrics |
| GET | `/vms?state=&agent_id=&q=&limit=&cursor=` | Filters and paging |
| PATCH | `/vms/{id}` | Name and labels |
| GET | `/operations?vm_id=&limit=` | History |
| POST | `/agents/{id}/exec` | Command execution |
| POST | `/agents/{id}/drain`, `/undrain` | Stop scheduling to a host |
| GET | `/audit` | Audit log |

Move errors to `{"code","message","request_id"}` and keep the HTTP statuses.

---

## 11. Security

- UI API: bearer token (static admin token first). Agent link: a per-agent token on the WebSocket handshake and as a header on manager → agent REST.
- Replace `CheckOrigin: true` with a configured allow-list.
- Terminate TLS in front of the manager; use `wss://`.
- Exec: admin only, audited, limits enforced again on the agent.
- Validate names (`[a-z0-9-]`), numeric ranges, and that `kernel_path`, `rootfs_path`, `disk_path` fall under configured directories so requests cannot point at arbitrary host files.

---

## 12. Configuration

| Flag | Default | Meaning |
|---|---|---|
| `-listen` | `:8080` | HTTP and WebSocket bind |
| `-db` | `kvm-manager.db` | SQLite file |
| `-network-cidr` | `172.20.0.0/16` | VM network |
| `-log-level` | `info` | slog level |
| to add: `-admin-token`, `-agent-token` | – | Authentication |
| to add: `-offline-after` | `15s` | Agent liveness |
| to add: `-allowed-origins` | – | WebSocket origins |
| to add: `-asset-dirs` | – | Allowed kernel and image directories |

---

## 13. Testing

| Level | Cases |
|---|---|
| Unit | Scheduler, allocator, reconcile table, state mapping |
| Handler | Fake agent HTTP server (accepts, fails, hangs) for create, start, stop, delete and error paths |
| WebSocket | Dial `/ws/agents`: register, heartbeat, events, reconnect with the old socket still open |
| Store | Migrations on an old database, unique names, IP release |
| Fix first | The existing smoke test requests `/healthz` but the route is `/api/v1/healthz` (from code; will fail with 404) |

---

## 14. Limitations

- Single instance, single SQLite connection; no failover.
- Manager → agent REST needs network reachability to the agent's `-public-url`.
- No image catalogue; callers pass host paths.
- Event delivery is best effort; correctness comes from snapshots.

---

## 15. Open questions

1. Keep SQLite or move to PostgreSQL now?
2. May the manager supply the operation id to the agent (requires an agent change)?
3. What is the network model across several hosts?
4. Keep deleted VMs for history, or remove them after cleanup?
