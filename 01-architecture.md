# KVM MicroVM Management – Architecture

**Status:** Draft v0.2 (POC) · **Based on:** repo `Jason-fy-wang/kvm-magement` at commit `95bf803` (`kvm-agent/`, `kvm-manager/`)

This document describes the system as it is built today and the target architecture for the POC. It is self-contained: it does not depend on the other design documents.

How findings are labelled: **verified** = reproduced by running a small test; **from code** = read in the source but not run.

---

## 1. Purpose and scope

An internal system to manage Firecracker microVMs on a small fleet of Linux KVM hosts from one web UI.

**In scope (POC)**

- VM lifecycle: create, start, stop, reboot, delete (update is limited to metadata)
- Host (agent) registration and status monitoring
- VM status events pushed to the UI
- A VM console in the browser
- Command execution on a host (not built yet)

**Out of scope (POC)**

- Live migration, snapshots, resize
- Image registry
- High availability, multi-tenancy
- Creating the host TAP device and bridge (done by the operator)

---

## 2. System overview

Three layers: **frontend → center service (`kvm-manager`) → agent (`kvm-agent`, one per host)**.

```
+-----------+  REST /api/v1  +  WS /api/v1/events        +-------------------+
| Frontend  | <----------------------------------------> | kvm-manager       |
| (React,   |  WS /api/v1/vms/{id}/console (proxied)      | REST + WS hub     |
|  to build)|                                             | SQLite            |
+-----------+                                             +----+---------+----+
                                                               |         ^
                       REST /v1/...   (manager -> agent)       |         | WS /ws/agents
                       WS   /v1/vms/{id}/console               v         | (agent -> manager:
                                                          +----+---------+----+  register, events)
                                                          | kvm-agent         |
                                                          | REST, in-memory   |
                                                          +---------+---------+
                                                                    | unix socket API
                                                              +-----v------+
                                                              | Firecracker|  one process per VM
                                                              +------------+
```

| Layer | Folder | Responsibility |
|---|---|---|
| Frontend | none yet | UI for agents, VMs, operations, console |
| Center service | `kvm-manager` | REST API for the UI, SQLite state, agent selection, IP/MAC allocation, forwards lifecycle calls to agents, tracks operations, relays events, proxies the console |
| Agent | `kvm-agent` | Runs on each host; starts one Firecracker process per VM and configures it over its unix socket; runs asynchronous operations; publishes events; bridges the VM console |

**Stack (as built):** Go (`go 1.27.1` in `go.mod`), standard `net/http` mux, `gorilla/websocket`, `log/slog`, `modernc.org/sqlite` (manager), `swaggo` Swagger UI (agent). Planned frontend: React + TypeScript.

---

## 3. Communication

| Path | Initiator | Transport | Content |
|---|---|---|---|
| UI → manager | UI | REST `/api/v1/...` | agents, VMs, operations |
| Manager → UI | manager | WebSocket `/api/v1/events` | operation and VM events |
| Agent → manager | **agent dials out** | WebSocket `/ws/agents` | `agent.register`, then events |
| Manager → agent | manager | REST `/v1/...` at the agent's `-public-url` | create, start, stop, reboot, delete |
| UI → console | UI | WebSocket `/api/v1/vms/{id}/console`, proxied to the agent | raw console text |

The agent opens the event connection, but the manager still needs network access to the agent's REST address. Both directions must be reachable.

**Async model.** Every lifecycle call returns `202 Accepted` with an `operation_id`. The manager stores its own operation row and links it to the agent's operation id. The agent's `operation.updated` event completes it.

---

## 4. VM lifecycle (as built)

| Action | What happens | VM state after |
|---|---|---|
| Create | Agent starts a Firecracker process and configures machine, kernel, root disk, optional data disk and network interface. **It does not boot the VM.** | `stopped` |
| Start | `InstanceStart`. If the process is gone, a new one is started and configured first | `running` |
| Stop | Ctrl-Alt-Del, wait 15 s, SIGTERM, then kill | `stopped` |
| Reboot | Ctrl-Alt-Del | `running` |
| Delete | Kill the process, delete the record. Kernel, rootfs and disk files stay on the host | `deleted` (manager keeps the row) |

| Where | States used |
|---|---|
| Agent | `creating`, `stopped`, `running`, `stopping` (defined, never set), `error` |
| Manager | `creating`, `stopped`, `running`, `error`, `deleted` |

The manager changes VM state only from agent events (create/stop → `stopped`, start/reboot → `running`, delete → `deleted`) or to `error` when an agent call fails.

---

## 5. Networking (as built)

- The manager owns one CIDR (`-network-cidr`, default `172.20.0.0/16`). The first address is the gateway. Each VM gets the next free address, recorded in `ip_allocations`.
- MAC address is derived from the IP: `06:00:<four IP bytes>`.
- The TAP device must already exist on the host and is named by the caller. The agent passes its name to Firecracker; it does not create or bridge it.
- The IP is released when a delete succeeds or a create fails.
- **The guest is not told its IP.** The agent stores `guest_ip`, `gateway_ip`, `netmask` but the kernel boot args contain no `ip=` option, so the guest image must configure networking itself (DHCP or static).

---

## 6. Core flows

**Create**

1. UI `POST /api/v1/vms`.
2. Manager validates, picks an agent (the requested one, or the online agent with the best score), allocates IP and MAC, inserts the VM (`creating`) and an operation.
3. Manager `POST {agent}/v1/vms`; agent replies `202` with its operation id.
4. Manager stores the agent operation id and returns `202` to the UI.
5. Agent configures Firecracker and emits `operation.updated: succeeded`.
6. Manager sets the VM to `stopped` and publishes the event to the UI.

**Start / stop / reboot / delete** follow the same steps without scheduling and IP allocation.

**Console.** The UI opens the manager WebSocket; the manager dials the agent WebSocket and copies messages both ways.

---

## 7. Known gaps

| # | Gap | Evidence | Impact | Fix |
|---|---|---|---|---|
| 1 | **Agent registration fails.** The manager decodes `agent.register` into a struct without JSON tags, so `agent_id` does not map to `AgentID` and stays empty; the handler then returns without registering | **Verified** by running the same struct and message | No agent ever appears | Add JSON tags; add a WebSocket registration test |
| 2 | No periodic heartbeat. The manager handles `agent.heartbeat` but the agent never sends it; an agent goes offline only when its socket closes | From code | Hung agents look online | Agent heartbeat every 5 s |
| 3 | Capacity never reported: register carries only id and URL, so vCPU and disk columns stay 0, and the scheduler treats 0 as "unknown" and accepts the agent. Memory is never considered | From code | Overcommit | Report host stats; schedule on vCPU, memory, disk |
| 4 | Agent state is in memory only | From code | After an agent restart the manager still lists VMs the agent forgot | Persist and recover |
| 5 | Events are lost while the link is down and dropped when a subscriber buffer (32) is full | From code | State drift | Full snapshot on every connect |
| 6 | Possible race: the agent may emit `succeeded` before the manager stores the agent operation id | From code | Operation stuck `running` | Manager supplies the operation id |
| 7 | Agent going offline does not change its VMs' state | From code | Stale `running` in UI | Mark VMs `unknown` |
| 8 | A reconnecting agent can be marked offline by its old socket's cleanup | From code | Flapping status | Track connection generation |
| 9 | No authentication; WebSocket origin check accepts every origin | From code | Anyone on the network can control VMs | Tokens, then TLS |
| 10 | Guest IP not applied (section 5) | From code | Manager allocation has no effect | Add `ip=` boot args |
| 11 | Firecracker stdout is a pipe read only while a console is open | From code | Guest may block when the pipe fills | Always drain into a buffer |
| 12 | No exit watcher: a crashed Firecracker leaves the VM `running` | From code | Wrong state | Watch process exit, emit event |
| 13 | `deleted` VMs stay in lists; VM names not unique; TAP name not validated | From code | Confusing UI, conflicts | Filter, unique index, preflight |
| 14 | Manager calls agents with `http.DefaultClient` (no timeout) | From code | Hung requests | Client with timeouts |
| 15 | Stop uses Ctrl-Alt-Del, which may reboot the guest | From code (the source comment says so) | Stop may not stop | Guest poweroff first |
| 16 | Exec and VM update are not implemented | From code | Requirements not met | New APIs |
| 17 | The manager smoke test requests `/healthz` but the route is `/api/v1/healthz`; the agent serves `/v1/healthz` while its README says `/healthz` | From code | Failing or misleading checks | Align paths |

---

## 8. Design decisions

1. **Keep the transport.** Manager → agent REST for commands; agent → manager WebSocket for registration, heartbeat and events. Do not redesign now.
2. **Reconcile by snapshot, not by event delivery.** The manager owns intent and inventory (SQLite); the agent owns what is really running. On each heartbeat or reconnect the agent sends its VM list and the manager reconciles.
3. **Create stays separate from start.** The UI offers *Create and start* by chaining the two operations.
4. **The manager allocates networks;** the agent only attaches a TAP prepared by the operator.
5. **SQLite stays** for the POC (one manager instance). The store boundary allows replacement later.
6. **One operation id on both sides.** The manager generates it and sends it to the agent.

---

## 9. Target behaviour

### 9.1 Status monitoring

- Agent heartbeat every 5 s: capacity (vCPU, memory, disk) and the list of local VMs with state and pid.
- Manager marks an agent `offline` after 15 s without a heartbeat and its VMs `unknown`.
- On reconnect or heartbeat the manager reconciles: update differing states, flag unknown VMs on the agent as `orphan`, mark VMs missing from the agent as `error` after two consecutive misses.
- Firecracker exiting unexpectedly produces an immediate `vm.status_changed` event.

### 9.2 Persistence

- Manager: SQLite (agents, VMs, operations, IP allocations; add an audit log).
- Agent: VM records and recent operations on disk under `-data-dir`; recovered and re-probed on startup.

### 9.3 Security

Minimum before use outside a lab: a token per agent (WebSocket handshake and agent REST), a user token for the UI API, restricted WebSocket origins, TLS in front of the manager, an allow-list for exec, an audit log, and path validation so requests cannot point at arbitrary host files.

---

## 10. Failure handling

| Failure | Behaviour (target) |
|---|---|
| Agent unreachable during an operation | Operation `failed`, VM `error`, IP released if create |
| Agent crashes | Agent `offline`, VMs `unknown`; on restart the agent recovers records and sends a snapshot |
| Firecracker process dies | Agent emits an event, VM `error` or `stopped` |
| Manager restarts | State is in SQLite; agents reconnect and send snapshots; stuck operations time out |
| Duplicate request | Rejected by name uniqueness and per-VM operation guard |

---

## 11. Open questions

1. Should delete also remove the VM's data disk, or keep files as today?
2. Where do kernels and root images come from, and how do they reach each host? Today the caller passes host paths.
3. One shared bridge CIDR across several hosts: is routing between hosts defined?
4. Is exec needed on the host, inside the guest, or both?
5. Expected number of hosts and VMs?
