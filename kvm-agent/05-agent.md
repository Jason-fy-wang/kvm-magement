# KVM MicroVM Management – Agent Design (`kvm-agent`)

**Status:** Draft v0.2 (POC) · **Based on:** `kvm-agent` at commit `95bf803` of `Jason-fy-wang/kvm-magement`

Self-contained design for the host agent. Findings are labelled **verified** (reproduced by running a test) or **from code** (read in the source, not run).

**Context.** The system has three layers: frontend → center service (`kvm-manager`) → agent. One agent runs on every KVM host and manages that host's Firecracker microVMs. The agent makes no cluster-wide decisions; scheduling, IP allocation and users stay in the manager.

---

## 1. Responsibilities

| Responsibility | Status |
|---|---|
| VM lifecycle on this host: create, start, stop, reboot, delete | Built |
| Asynchronous operations with ids readable over REST | Built |
| Event stream (REST WebSocket `/v1/events`, and outbound to the manager) | Built |
| VM console over WebSocket | Built |
| Registration with the manager | Built; sends only id and URL |
| Heartbeat and host capacity reporting | Missing |
| Persistent VM records and recovery after restart | Missing (in memory) |
| Detection of Firecracker exiting on its own | Missing |
| Guest IP configuration | Missing |
| Command execution | Missing |
| Authentication | Missing |

---

## 2. Code layout

| File | Contents |
|---|---|
| `cmd/kvm-agent/main.go` | Flags: `-listen :9090`, `-agent-id` (default hostname), `-manager-url`, `-public-url`, `-firecracker`, `-data-dir /var/lib/kvm-agent`, `-log-level` |
| `internal/agent/server.go` (~600 lines) | Routes, handlers, operations, event hub, console bridge, request logging, Swagger annotations |
| `internal/agent/store.go` | In-memory maps for VMs and operations, with a mutex |
| `internal/agent/model.go` | `VM`, `CreateVMRequest`, `Operation`, `Event`, `VMState` |
| `internal/agent/manager_client.go` | Dials the manager WebSocket, registers, forwards hub events, retries every 3 s |
| `internal/firecracker/client.go` | Starts the process, calls the Firecracker API over its unix socket, graceful shutdown, console pipes |
| `cmd/kvm-agent/docs` | Generated Swagger (`make docs`) |

Stack: Go, standard `net/http` mux, `gorilla/websocket`, `log/slog`, `swaggo`.

---

## 3. How a VM is run

- One Firecracker process per VM, started with `--api-sock <data-dir>/<vm-id>.sock --enable-pci`. Its stdin and stdout are pipes held by the agent and used as the console.
- **Create** calls, in order: `PUT /logger` (file `/tmp/log-<id>`, level Debug), `/machine-config`, `/boot-source` (boot args `console=ttyS0 reboot=k panic=1`), `/drives/rootfs` (read-write root), optional `/drives/data`, and `/network-interfaces/eth0` with the TAP name and MAC. The VM ends in `stopped`.
- **Start** sends `InstanceStart` → `running`. If no process exists for the VM, one is started and configured first.
- **Stop** sends Ctrl-Alt-Del, waits up to 15 s, then SIGTERM, then kills after 3 s. The process handle is dropped.
- **Reboot** sends Ctrl-Alt-Del.
- **Delete** kills the process and deletes the record. Disk files, socket file and TAP device are untouched.

VM states: `creating`, `stopped`, `running`, `stopping` (defined, never set), `error`.

---

## 4. HTTP API (as built)

| Method | Path | Result |
|---|---|---|
| GET | `/v1/healthz` | `{"status":"ok"}` (the README says `/healthz`) |
| GET | `/v1/agent` | id, hostname (same as id), version `0.1.0` |
| GET | `/v1/vms`, `/v1/vms/{id}` | VM records |
| POST | `/v1/vms` | `202 {operation_id, vm_id, status}`; `400` if vcpu < 1, memory < 128 MiB, or kernel or rootfs missing; `409` if the id exists |
| POST | `/v1/vms/{id}/start`, `/stop`, `/reboot` | `202` + operation; `404`; `409` only when the process handle is missing (stop, reboot) |
| DELETE | `/v1/vms/{id}` | `202` + operation |
| GET | `/v1/operations/{id}` | `status` `queued` / `running` / `succeeded` / `failed`, `error` |
| WS | `/v1/vms/{id}/console` | `409` if no console pipes |
| WS | `/v1/events` | Event stream |
| GET | `/swagger/` | Swagger UI |

Errors are plain text. Create takes `id`, `guest_ip`, `guest_mac`, `gateway_ip`, `netmask`, `tap_device` from the manager; if no id is sent the agent generates one.

**Events** (`operation.created`, `operation.updated`, `vm.status_changed`) carry `agent_id`, `vm_id`, optionally the `operation` and `vm`, and a timestamp.

---

## 5. Findings and fixes

| # | Finding | Evidence | Impact | Fix |
|---|---|---|---|---|
| A1 | **Guest IP never applied.** `guest_ip`, `gateway_ip`, `netmask` are stored but boot args contain no `ip=` and nothing configures the guest | From code | Manager allocation has no effect unless the image configures its own network | Add `ip=<guest>::<gateway>:<netmask>::eth0:off` to boot args, or document the image requirement |
| A2 | **Console pipe may stall the VM.** Firecracker stdout is read only while a console WebSocket is open | From code | Pipe buffer can fill and block the guest | Always drain stdout into a ring buffer; replay it to new console sessions |
| A3 | **VM records live in memory** | From code | After an agent restart the agent forgets every VM | Persist records (section 6) |
| A4 | **Firecracker is a child with pipes owned by the agent** | From code | Restarting the agent can break consoles or kill guests | Start detached; console via file or FIFO under the data dir |
| A5 | **No exit watcher**; `Wait` is called only during shutdown | From code | A crashed Firecracker leaves the VM `running` | One goroutine per process; on exit set state and emit an event |
| A6 | **No per-VM lock or state checks**; handlers mutate a shared `*VM` from goroutines | From code | Races; start while running, two operations at once | Per-VM mutex, state guards returning `409`, updates through the store |
| A7 | **Registration sends only id and URL**; no heartbeat | From code | Manager cannot schedule or detect hung agents | Section 7 |
| A8 | **Stop uses Ctrl-Alt-Del**, which may reboot the guest (the source comment says so) | From code | Stop may not stop | Guest poweroff via console or guest helper first; keep fallbacks |
| A9 | Delete leaves the socket file and log; `stopping` unused | From code | Leftovers | Remove them; use `stopping` during stop |
| A10 | Event hub drops events for slow subscribers; events lost while the manager link is down | From code | Manager drifts | Snapshot after every reconnect |
| A11 | `/v1/*` and `/v1/events` are open; `CheckOrigin` accepts every origin | From code | Anyone reachable can control VMs | Token; bind to the management interface |
| A12 | Firecracker logger path `/tmp/log-<id>` hard-coded | From code | Logs mixed in `/tmp` | Log under the data dir per VM |
| A13 | Create accepts a TAP name that does not exist or is in use | From code | Late, obscure failures from Firecracker | Preflight: `/dev/kvm`, files exist, TAP exists and unused |
| A14 | **Manager-side registration bug that blocks this agent.** The manager decodes `agent.register` without JSON tags, so `agent_id` arrives empty and the agent is never registered | **Verified** (manager code, same struct and message) | Agent connects but never appears in the manager | Fixed in the manager; add a registration test that uses this agent's real message |

---

## 6. Persistence and recovery (target)

```
/var/lib/kvm-agent/
  vms/<vm-id>/
    vm.json          # request + current state, atomic write
    api.sock         # Firecracker API socket
    console.log      # serial output
    firecracker.log
  operations.json    # recent operations, bounded
```

**Startup**

1. Load every `vm.json`.
2. For each, check that the recorded pid is a Firecracker process whose socket answers. If yes, query its state and mark `running`; if the process is gone, mark `stopped` or `error` depending on the last state.
3. Re-attach console reading.
4. Connect to the manager and send register plus a **snapshot**.

---

## 7. Reporting (target)

**Register:** `agent_id`, `hostname`, `url`, `agent_version`, and capacity: `vcpu_total`, `vcpu_available`, `mem_total_mib`, `mem_available_mib`, `disk_total_bytes`, `disk_free_bytes`.

**Heartbeat every 5 s:** the same capacity fields, refreshed, plus the list of local VMs with `id`, `state`, `pid`.

Capacity sources: CPU count from the host, memory from `/proc/meminfo`, disk from `statfs` of the data directory; subtract resources committed to non-deleted VMs for the `available` values.

**Rules**

- Send a snapshot immediately after every (re)connect.
- If the manager is unreachable, keep running VMs and serving local REST.
- Reconnect with exponential backoff and jitter instead of a fixed 3 s.
- Never hold two connections with the same agent id.

---

## 8. Command execution (new)

| Control | Rule |
|---|---|
| Enabled | `-exec-enabled` flag, off by default |
| Allow-list | Command names such as `uptime`, `df`, `free`, `ip`; others return `403` |
| Arguments | JSON argv array; never passed through a shell |
| Limits | Timeout default 30 s (max 300 s); stdout+stderr cap 1 MiB with a `truncated` flag; no stdin |
| Identity | Runs as the agent's user; cleared environment; fixed working directory |
| Audit | Log request id, command, exit code, duration; the manager records the caller |

`POST /v1/exec` with `{"argv":["df","-h"],"timeout_s":10}` returns `{"exit_code":0,"stdout":"…","stderr":"","truncated":false,"duration_ms":18}`. Long commands should run as operations like the others.

**Inside the guest:** the console WebSocket already gives an interactive session if the image runs a login on `ttyS0`. Programmatic exec in the guest needs a vsock helper or SSH; defer unless required.

---

## 9. Security

- Token required on every `/v1` route except health; the same token identifies the agent to the manager.
- Bind `-listen` to the management interface, not all interfaces.
- Validate VM ids (`[a-z0-9-]`) because they become file names; require `kernel_path`, `rootfs_path`, `disk_path` to be under configured directories (`-asset-dir`).
- Run as a dedicated user in the `kvm` group (the Makefile grants `/dev/kvm` through an ACL for development).
- Do not log tokens or console contents at info level.

---

## 10. Configuration

| Flag | Default | Meaning |
|---|---|---|
| `-listen` | `:9090` | REST and WebSocket bind |
| `-agent-id` | hostname | Stable id |
| `-manager-url` | – | `ws://manager:8080/ws/agents`; empty disables reporting |
| `-public-url` | – | URL the manager uses for REST and console |
| `-firecracker` | `firecracker` | Binary path |
| `-data-dir` | `/var/lib/kvm-agent` | State and sockets |
| `-log-level` | `info` | slog level |
| to add: `-token`, `-exec-enabled`, `-asset-dir`, `-heartbeat` | – | Sections 7–9 |

---

## 11. Deployment

- Linux only, with `/dev/kvm`, Firecracker installed, and the TAP device and bridge prepared (the agent does not create them).
- systemd unit with `Restart=on-failure`; Firecracker processes must survive agent restarts (A4).
- The Makefile also builds `agent.exe` for Windows; the agent cannot run there, so that target can be removed.

---

## 12. Testing

| Level | Cases |
|---|---|
| Unit | Store, state guards, per-VM lock, exec limits, heartbeat payload |
| Driver | Put an interface in front of the Firecracker client; test handlers against a fake |
| Fake Firecracker | A small HTTP server on a unix socket that records the configure sequence |
| Host integration | Real create, start, stop, reboot, delete; kill Firecracker and expect an event; restart the agent with a running VM |
| Existing | One store test; add handler tests next |

---

## 13. Limitations

- Manager → agent REST requires reachability of `-public-url`.
- No snapshots, resize or live migration.
- Disk images are used in place and nothing is copied, so two VMs must not share a writable rootfs.
- The console is raw serial; no terminal-size handling.

---

## 14. Open questions

1. Should the agent create and delete TAP devices and attach them to the bridge, or stay operator-managed?
2. Do guest images configure networking themselves, or should the agent inject `ip=` boot args (A1)?
3. Should each VM get its own copy or overlay of a base rootfs?
4. What must survive an agent restart: the records only, or running guests and their consoles too?
5. Is in-guest exec required, or is the serial console enough?
