# KVM MicroVM Management – Implementation Plan

**Status:** Draft v0.2 (POC) · **Based on:** repo `Jason-fy-wang/kvm-magement` at commit `95bf803`

Self-contained plan. Estimates are for one developer; the frontend can run in parallel once the manager API is stable. Total: about 4–5 weeks sequentially.

---

## 1. Baseline: what exists today

The system has three layers: frontend → center service (`kvm-manager`) → agent (`kvm-agent`, one per host). Both backend services are Go.

| Area | Built | Missing |
|---|---|---|
| Agent | REST API for create/start/stop/reboot/delete; async operations; Firecracker control over its unix socket; WebSocket event stream; console WebSocket; Swagger UI | Persistence, heartbeat, host capacity, exec, auth, exit detection, guest IP setup |
| Manager | REST API; SQLite store; agent registry over WebSocket; scheduler v0; IP/MAC allocation; operation tracking; event relay to UI; console proxy | Working agent registration, liveness, reconcile, auth, VM update, pagination, tests |
| Frontend | – | Everything |
| Ops | Makefiles | CI, systemd units, container build, README quick start |

### Issues to fix (referenced by milestone below)

| ID | Issue | Evidence |
|---|---|---|
| B1 | **Agent registration fails:** the manager decodes `agent.register` into a struct with no JSON tags, so `agent_id` stays empty and the handler returns early | **Verified** by running the same struct and message |
| B2 | No heartbeat is sent by the agent; the manager only learns of an agent leaving when its socket closes | From code |
| B3 | Agent never reports capacity, so the scheduler accepts any agent; memory is not considered | From code |
| B4 | Agent keeps VMs and operations in memory only | From code |
| B5 | Events are lost during disconnects and dropped when a subscriber buffer is full; no snapshot after reconnect | From code |
| B6 | Possible race: agent may report `succeeded` before the manager stores the agent operation id | From code |
| B7 | Agent offline does not change its VMs' state | From code |
| B8 | A reconnecting agent can be marked offline by its old socket's cleanup | From code |
| B9 | No authentication; any WebSocket origin accepted | From code |
| B10 | Guest IP is never applied (no `ip=` boot arg) | From code |
| B11 | Firecracker stdout pipe is only read while a console is open | From code |
| B12 | No watcher for Firecracker exiting | From code |
| B13 | `deleted` VMs listed; names not unique; TAP name not validated | From code |
| B14 | Manager uses `http.DefaultClient` without timeouts | From code |
| B15 | Stop uses Ctrl-Alt-Del, which may reboot instead | From code |
| B16 | Manager test requests `/healthz` but the route is `/api/v1/healthz`; agent README says `/healthz` but the route is `/v1/healthz` | From code |

---

## 2. Milestones

| # | Milestone | Outcome | Est. |
|---|---|---|---|
| M0 | Stabilize baseline | Registration works, tests honest, one scripted end-to-end run, CI | 2–3 d |
| M1 | Agent persistence and monitoring | Heartbeat with capacity, state survives restart, crash detection, guest IP | 4–5 d |
| M2 | Manager reconcile and status | Offline detection, `unknown` state, snapshot reconcile, better scheduling, API additions | 3–4 d |
| M3 | Frontend MVP | Full VM lifecycle, live status, console from the browser | 5–7 d |
| M4 | Command execution | Exec on a host, audited, with UI panel | 3–4 d |
| M5 | Security and packaging | Tokens, origin checks, TLS, systemd, compose, README | 3–4 d |

---

## 3. Milestone details

### M0 – Stabilize baseline

Fixes: B1, B6, B13, B14, B16.

1. Add explicit JSON tags to the registration struct (`type`, `agent_id`, `hostname`, `url`, capacity fields) and add a test that dials the real `/ws/agents` handler.
2. Align health paths between code, tests and READMEs; run `go test ./...` in both modules and fix failures.
3. Remove the operation-id race: the manager generates the operation id and sends it to the agent, which uses it as its own operation id.
4. Use an `http.Client` with timeouts for agent calls.
5. Hide `deleted` VMs from list results (or filter by `?state=`); add a unique index on VM name; reject create when `tap_device` is empty or already used on that agent.
6. Write `scripts/e2e.sh`: start manager and agent, create, start, stop, delete a VM with `curl`, assert states.
7. CI: `gofmt`, `go vet`, `go test`, build both binaries.

**Exit criteria:** `scripts/e2e.sh` passes on a KVM host; CI is green.

### M1 – Agent persistence and monitoring

Fixes: B2, B3, B4, B5, B10, B11, B12, B15.

1. Persist VM records and recent operations under `-data-dir` (JSON files or SQLite) with atomic writes.
2. On startup load records and probe each one: is the Firecracker process alive and its socket answering? Set state accordingly.
3. Start Firecracker detached from the agent with its console going to a file or FIFO, so restarting the agent does not kill guests or break the console. Always drain console output into a ring buffer.
4. Register with capacity (vCPU, memory, disk) and send a heartbeat every 5 s with the same capacity and the list of local VMs (`id`, `state`, `pid`).
5. Send a full VM snapshot after every (re)connect; reconnect with exponential backoff.
6. One goroutine per Firecracker process waiting for exit; on exit set `error` or `stopped` and emit `vm.status_changed`.
7. Add `ip=<guest>::<gateway>:<netmask>::eth0:off` to the kernel boot args (or document that images must self-configure).
8. Stop: try a guest poweroff first, keep Ctrl-Alt-Del, SIGTERM and kill as fallbacks.
9. Per-VM lock and state guards returning `409`.

**Exit criteria:** restart the agent with running VMs and the manager shows correct states within 10 s; kill a Firecracker process and the state changes within 5 s.

### M2 – Manager reconcile and status

Fixes: B2, B3, B5, B7, B8.

1. Handle `agent.heartbeat`: update capacity and `last_seen`; a sweeper marks agents `offline` after 15 s without one.
2. When an agent goes offline set its VMs to `unknown`; when it returns, apply its snapshot.
3. Reconcile on each snapshot: update differing states, flag VMs unknown to the manager as `orphan`, mark VMs missing on the host as `error` after two consecutive misses.
4. Track a connection generation per agent so an old socket closing cannot mark a reconnected agent offline.
5. Scheduler: require reported capacity; check vCPU, memory and disk; subtract in-flight creates; prefer most free memory.
6. Operation timeout sweeper: operations stuck `running` past a limit become `failed` and the VM `error`.
7. API additions: `GET /agents/{id}`, filters and paging on `GET /vms`, `PATCH /vms/{id}` (name, labels), `GET /operations?vm_id=`, JSON error bodies, an OpenAPI/Swagger spec.

**Exit criteria:** stop the agent: UI shows agent offline and VMs `unknown` within 20 s; start it: states recover without manual action.

### M3 – Frontend MVP

React + TypeScript + Vite, TanStack Query, native WebSocket, xterm.js.

1. Scaffold, API client, dev proxy (the manager has no CORS handling).
2. VM list, detail, and actions with operation toasts.
3. Create form with a *Start after create* option (chains `start`).
4. Events WebSocket with cache patching; refetch on reconnect.
5. Agents page and dashboard.
6. Console tab.

**Exit criteria:** a non-developer can create, start, stop, reboot and delete a VM and open its console from the browser.

### M4 – Command execution

Fixes: exec requirement.

1. Agent `POST /v1/exec`: disabled by default, allow-list of commands, argv array (no shell), timeout, output cap, runs as the agent user.
2. Manager `POST /agents/{id}/exec` as an operation; result stored; audit row written.
3. Frontend command panel on the agent page.
4. Decide on in-guest exec (serial console, vsock helper or SSH) and document the choice.

**Exit criteria:** run `df -h` from the UI with output shown and an audit entry; over-limit commands are cut off.

### M5 – Security and packaging

Fixes: B9.

1. Agent token on the WebSocket handshake and on agent REST calls; user token for the UI API.
2. Restrict WebSocket origins; TLS in front of the manager.
3. Validate kernel, rootfs and disk paths against configured directories.
4. systemd units for agent and manager; container image and compose file for the manager.
5. README quick start (add a host in under 10 minutes, including TAP and bridge commands) and a demo script.

**Exit criteria:** a fresh host joins following the README; unauthenticated calls are rejected.

---

## 4. Work by component

| Component | M0 | M1 | M2 | M3 | M4 | M5 |
|---|---|---|---|---|---|---|
| Agent | small fixes | **main** | – | – | exec | token, unit |
| Manager | **main** | – | **main** | – | exec API, audit | auth, compose |
| Frontend | – | – | contract | **main** | panel | login |

---

## 5. Testing

| Level | What | How |
|---|---|---|
| Unit | Scheduler, IP allocator, reconcile table, state mapping, exec limits | Go tests (allocator and store tests exist) |
| Component | Manager with a fake agent (REST and WebSocket); agent with a fake Firecracker (HTTP over a unix socket) | `httptest`, stub socket server |
| Integration | Real agent and Firecracker, scripted lifecycle | `scripts/e2e.sh` on a KVM host |
| Failure | Kill agent, kill Firecracker, drop network, restart manager mid-operation | Scripted |
| UI | Create → start → stop → delete smoke test | Playwright |

Rule: every bug found on a real host gets a regression test at the lowest level that reproduces it.

---

## 6. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Hidden registration and event bugs behind manual testing | Manager and agent silently disagree | M0 end-to-end script and WebSocket tests first |
| Guests die or lose console on agent restart | Lost VMs on every upgrade | M1 detached Firecracker |
| Shared CIDR across hosts | Undefined routing between hosts | Decide the multi-host network model before more than one host |
| Exec abused | Host compromise | Off by default, allow-list, audit, token |
| Single SQLite writer | Throughput limit | Acceptable for the POC |
| Host prerequisites (`/dev/kvm`, TAP, bridge) | Obscure create failures | Preflight checks and a README checklist |

---

## 7. Definition of done (POC)

- [ ] Create, start, stop, reboot, delete a VM from the UI on at least two hosts
- [ ] Agent and VM status visible within 10 s; offline detected within 20 s
- [ ] State recovers after an agent restart and after a manager restart
- [ ] Exec on a host with an audit trail
- [ ] Authentication on all APIs
- [ ] README quick start; CI green

---

## 8. Backlog after the POC

Resize and snapshots · image and kernel catalogue · automatic TAP and bridge setup · volumes · roles and SSO · Prometheus metrics and alerts · manager HA with PostgreSQL · live migration
