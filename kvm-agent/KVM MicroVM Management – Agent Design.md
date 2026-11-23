# KVM MicroVM Management – Agent Design

**Status:** Draft v0.1 (POC) · **Component:** `kvm-agent` · **Runs on:** every KVM host

> **\[Assumption\]** `kvm-agent` already contains a working VM *create* routine (manually tested). I could not read the code, so this design wraps that routine and defines what must be added around it. The runtime is written as "microVM runtime" (Firecracker, Cloud Hypervisor, QEMU microvm, etc.); adapt the probing details in section 5 to whichever one you use.

## 1. Responsibilities

1. **Execute VM lifecycle** on the local host: create, start, stop, delete, query.
2. **Report** host facts, metrics, and the real state of every local VM to the center.
3. **Execute commands** on the host (and later in guests) when the center asks, within strict limits.
4. **Keep local state** so it can recover after a restart.
5. **Stay thin:** no scheduling, no cluster-wide decisions, no user management.

## 2. Internal structure

```
+---------------------------------------------------------------+
|                          kvm-agent                            |
|                                                               |
|  HTTP server (/v1)  <--- token auth, request-id, validation   |
|        |                                                      |
|  Command handlers  -->  Idempotency cache                    |
|        |                                                      |
|  VM manager  <-->  Local VM store (metadata on disk)          |
|     |   |                                                     |
|     |   +--> Runtime driver (existing create code, start/stop)|
|     |   +--> Resource helpers: disk/image, network (TAP/bridge)|
|     |                                                         |
|  State prober  <-- reads runtime / process / sockets          |
|        |                                                      |
|  Reporter: register + heartbeat + events  ----> Center        |
|  Exec runner (limits, allowlist)                              |
|  Host stats collector (CPU/mem/disk)                          |
+---------------------------------------------------------------+
```

| Module | Role |
| --- | --- |
| HTTP server | Exposes the agent API; rejects calls without a valid node token |
| VM manager | Orchestrates one VM operation at a time per VM (per-VM lock) |
| Runtime driver | Thin interface over the microVM runtime; the existing create code moves behind it |
| Local VM store | One directory per VM with `spec.json`, `state.json`, sockets, logs, disk |
| State prober | Computes real state from the process/runtime, not from stored flags |
| Reporter | Registers once, then heartbeats; retries and buffers events when the center is down |
| Exec runner | Runs commands with timeout, output cap, and safe argument handling |
| Stats collector | Reads `/proc` or equivalent for CPU/memory, filesystem stats for disk |

### 2.1 Runtime driver interface (suggested)

```
Create(spec)  -> VMInfo        // wraps the existing, working create code
Start(id)     -> error
Stop(id, force bool) -> error  // graceful first, then kill after timeout
Delete(id)    -> error        // stop if running, remove files, TAP, sockets
Probe(id)     -> State        // running / stopped / error / missing
List()        -> []VMInfo
```

Keeping this interface lets you unit-test the manager with a fake driver and swap runtimes later.

## 3. Local state and layout

```
/var/lib/kvm-agent/
  agent.json            # node_id, token reference, center url
  idem/                 # recent request_id -> result (TTL 1 h)
  vms/
    <vm_id>/
      spec.json         # what was requested
      state.json        # last known state + timestamps
      disk.img          # root disk (or overlay)
      api.sock          # runtime control socket
      vm.log            # runtime output
/var/log/kvm-agent/     # agent logs (or journald)
```

- Writes to `spec.json` / `state.json` are atomic (write temp + rename).
- On startup the agent **rebuilds its view from disk + live probing**, so it never depends on in-memory state.

## 4. Agent API (center → agent)

Base path `/v1`. JSON. Header `Authorization: Bearer <node-token>`; header `X-Request-Id` is the idempotency key.

| Method | Path | Description |
| --- | --- | --- |
| GET | `/healthz` | Liveness (no auth) |
| GET | `/v1/host` | Host facts and current usage |
| GET | `/v1/vms` | List local VMs with probed state |
| POST | `/v1/vms` | Create VM (body: id, name, vcpus, mem\_mb, disk\_gb, image, kernel, net) |
| GET | `/v1/vms/{id}` | VM detail + probed state |
| POST | `/v1/vms/{id}/start` | Start |
| POST | `/v1/vms/{id}/stop` | Stop (`?force=true` allowed) |
| DELETE | `/v1/vms/{id}` | Delete VM and its resources |
| POST | `/v1/exec` | Run a command (see section 7) |

**Create request**

```json
{ "id": "6f1c...", "name": "web-01", "vcpus": 2, "mem_mb": 512,
  "disk_gb": 5, "image": "ubuntu-22-min", "kernel": "vmlinux-5.10",
  "net": { "mac": "AA:FC:00:00:00:01", "ip": "172.16.0.10/24" } }
```

**Create response**

```json
{ "id": "6f1c...", "state": "running", "ip": "172.16.0.10", "pid": 4321 }
```

**Semantics**

- The **center generates the VM id**; the agent never invents ids. A repeated create with the same id and request id returns the earlier result (idempotent); a same-id create with a different spec returns `409`.
- Operations on one VM are serialized by a per-VM lock; a second concurrent call gets `409 BUSY`.
- Errors use `{code, message, request_id}`; codes include `VALIDATION_ERROR`, `NOT_FOUND`, `ALREADY_EXISTS`, `BUSY`, `RESOURCE_EXHAUSTED`, `RUNTIME_ERROR`, `TIMEOUT`.
- POC: calls may be synchronous with timeouts (create ≤ 120 s). If creation proves slow, switch to "202 + poll" without changing the center's task model.

## 5. State probing

Reported state must come from reality:

| Reported state | How determined |
| --- | --- |
| `creating` | Operation lock held by a create |
| `running` | Runtime process alive **and** control socket answers (or runtime says running) |
| `stopped` | Directory exists, process not running, no error marker |
| `error` | Last operation failed, process died unexpectedly, or files are inconsistent; includes message |
| `missing` | Referenced but directory absent (center treats as error) |

A background watcher notices **unexpected exits** (crash/OOM-kill) and emits an event immediately instead of waiting for the next heartbeat.

## 6. Reporting to the center

**Registration (first start, or when no `node_id` stored):** `POST /api/v1/agent/register` with name, address:port, capacity, agent version, enrollment token → store `node_id` and node token.

**Heartbeat (every 5 s):** `POST /api/v1/agent/heartbeat`

```json
{ "node_id": "...", "ts": 1735000000,
  "host": { "cpu_pct": 12.5, "mem_used_mb": 8200, "mem_total_mb": 32000,
            "disk_used_gb": 120, "disk_total_gb": 500 },
  "vms": [ { "id": "6f1c...", "state": "running", "pid": 4321, "uptime_s": 3600, "ip": "172.16.0.10" } ] }
```

**Rules**

- Heartbeat includes **all** local VMs every time (simple, self-healing, no delta bugs).
- On failure: log, keep retrying on schedule with backoff capped at 30 s; **never** stop managing local VMs because the center is unreachable.
- On startup: scan + probe first, then send an immediate heartbeat so the center can reconcile.
- Events (VM crashed, operation finished) are sent to `/agent/events`; undelivered events are kept in a small on-disk queue and replayed.

## 7. Command execution

High-risk feature; defaults are conservative.

| Control | Rule |
| --- | --- |
| Enablement | `exec.enabled=false` by default; must be turned on in the agent config |
| Mode | **Allowlist** of command names/paths (e.g. `uptime`, `df`, `free`, `ip`) or `shell=true` only if explicitly configured |
| Arguments | Passed as an argv array, never concatenated into a shell string |
| Limits | Timeout (default 30 s, max 300 s), stdout+stderr cap (default 1 MB, truncated with a flag), no stdin |
| Identity | Runs as the agent user (non-root) unless a specific sudo rule is configured |
| Isolation | Fresh environment variables, fixed working directory |
| Audit | Agent logs the request id, caller, command, exit code, duration |

**Request / response**

```json
{ "argv": ["df", "-h"], "timeout_s": 10 }
```

```json
{ "exit_code": 0, "stdout": "...", "stderr": "", "truncated": false, "duration_ms": 18 }
```

**Guest commands (later):** use a vsock-based guest helper or SSH with keys injected at create time; out of scope for the first POC unless required.

## 8. Resource handling

| Resource | Requirement |
| --- | --- |
| Disk | Create from base image (copy or copy-on-write overlay); check free space before create; delete removes it |
| Network | Create TAP device and attach to the bridge; free it on delete; IP/MAC come from the center's request (or a local pool – decide, see open questions) |
| CPU/Memory | Validate against host limits; refuse with `RESOURCE_EXHAUSTED` rather than overcommit silently |
| Cleanup | A failed create must roll back everything it created (disk, TAP, sockets, process). Add a startup janitor for leftovers |

## 9. Security

- Runs as a dedicated user in the `kvm` group; only the specific capabilities needed for TAP/bridge (e.g. via file capabilities or a small privileged helper) rather than full root, if feasible.
- Listens on the internal interface only; token required for all `/v1` calls except `/healthz`; TLS recommended.
- Validate VM names and ids (`[a-z0-9-]`), numeric limits, and that every file path stays inside `/var/lib/kvm-agent`.
- Never build shell command strings from request fields.
- Do not log tokens; keep token file mode `0600`.

## 10. Configuration

```yaml
node_name: kvm-host-01
listen: 0.0.0.0:9090
center_url: https://center.internal:8080
enrollment_token: <one-time>
data_dir: /var/lib/kvm-agent
heartbeat_interval_s: 5
exec:
  enabled: false
  allowlist: [uptime, df, free, ip]
limits:
  max_vms: 50
  create_timeout_s: 120
runtime:
  binary: /usr/local/bin/<runtime>
  bridge: br0
  image_dir: /var/lib/kvm-agent/images
```

## 11. Deployment

- Single binary + systemd unit (`Restart=on-failure`, `User=kvm-agent`, resource limits).
- VMs must **survive an agent restart**: start runtime processes detached from the agent's process group so restarting the agent does not kill guests.
- Install script: create user, directories, bridge check, config template, enable service.
- Version is reported on register/heartbeat; center can warn about mismatched versions.

## 12. Testing

| Level | Focus |
| --- | --- |
| Unit | Validation, per-VM lock, idempotency cache, exec limits, state mapping using a **fake runtime driver** |
| Integration (KVM host) | Full create → start → stop → delete; create failure rollback; crash detection (kill the runtime process) |
| Restart tests | Restart agent with running VMs; agent restart during create; leftover cleanup |
| Contract | Agent API checked against OpenAPI spec; center's fake-agent and real agent behave the same |
| Security | Path traversal in names, oversize output, timeout kill, unauthorized access |

## 13. Limitations and future work

- Pull-style control (center calls agent); a persistent agent-initiated channel would help with NAT.
- No snapshots, live migration, or hot resize in the POC.
- Guest command execution and serial console/VNC proxying are deferred.
- Local metrics are point-in-time; no local time series (center stores history).

## 14. Open questions

1. Which microVM runtime does the existing create function use, and how does it expose state (API socket, PID file, none)?
2. Does create need root today? What is the minimum privilege set for TAP/bridge and disk operations?
3. Is create fast enough (a few seconds) to stay synchronous, or should the agent return `202` and finish in the background?
4. IP/MAC allocation: center-assigned (recommended) or agent-local pool?
5. Where do base images and kernels come from, and who keeps them in sync across hosts?
6. Is the agent's local state best kept as files (as above) or in a small SQLite DB?
