# KVM MicroVM Management – Frontend Design

**Status:** Draft v0.2 (POC) · **Based on:** `kvm-manager` at commit `95bf803` of `Jason-fy-wang/kvm-magement`

Self-contained design for the web UI. The frontend does not exist in the repo yet; this document is written against the manager's real API.

**Context.** The system has three layers: frontend → center service (`kvm-manager`) → agent (`kvm-agent`, one per KVM host, runs Firecracker microVMs). The frontend talks **only** to the manager.

---

## 1. Responsibilities

- Show agents (hosts), VMs and operations with live status.
- Create, start, stop, reboot and delete VMs; open a VM console.
- Later: run commands on a host, show host metrics.
- No business rules beyond form validation; the manager decides.

---

## 2. Manager API used by the UI (as built)

Base path `/api/v1`. No authentication yet.

| Method | Path | Notes |
|---|---|---|
| GET | `/agents` | `id, hostname, url, status, vcpu_total, vcpu_available, disk_total_bytes, disk_free_bytes, last_seen` |
| GET | `/vms` | All VMs, newest first. **Includes `deleted` rows today** |
| POST | `/vms` | Body: `name, agent_id?, vcpu_count, memory_mib (≥128), kernel_path, rootfs_path, disk_path?, tap_device`. Returns `202 {operation_id, vm_id, status}` |
| GET | `/vms/{id}` | Detail incl. `guest_ip, guest_mac, gateway_ip, netmask` |
| POST | `/vms/{id}/start`, `/stop`, `/reboot` | `202 {operation_id, vm_id, status}` |
| DELETE | `/vms/{id}` | `202` + operation |
| GET | `/operations/{id}` | `status`: `queued`, `running`, `succeeded`, `failed`; `error` text |
| WS | `/events` | JSON events: `operation.created`, `operation.updated`, `vm.status_changed`; each has `vm_id`, optionally `operation` and `vm` |
| WS | `/vms/{id}/console` | Raw text frames to and from the VM console |
| GET | `/healthz` | Liveness |

**VM fields:** `id, name, agent_id, state, vcpu_count, memory_mib, kernel_path, rootfs_path, disk_path, tap_device, guest_ip, guest_mac, gateway_ip, netmask, created_at, updated_at`.

**Behaviours the UI must handle**

- Errors are **plain text** bodies, not JSON. Show the text and key off the status code: `400` validation, `404` not found, `409` no capacity or not available, `502` agent unreachable.
- **Create does not boot.** A new VM ends in `stopped`. *Create and start* means: wait for the create operation to succeed, then call `start`.
- There is **no CORS handling** in the manager. In development use the Vite dev proxy; in production serve the built files from the same origin (manager or reverse proxy).
- The VM list contains `deleted` rows; filter them out until the manager does.
- Stop may not stop a guest that ignores Ctrl-Alt-Del. Show the operation result, never an assumed state.
- Events are not replayed after a disconnect.

---

## 3. Stack

| Concern | Choice |
|---|---|
| Framework | React + TypeScript + Vite |
| UI kit | Ant Design or MUI (tables, forms, modals) |
| Server state | TanStack Query; events patch the cache |
| Live updates | One native `WebSocket` to `/api/v1/events`, auto-reconnect with backoff |
| Console | xterm.js attached to the console WebSocket |
| API types | Hand-written until the manager publishes OpenAPI; then generated |
| Tests | Vitest + Testing Library; Playwright smoke test |

---

## 4. Pages

| Route | Page | Content |
|---|---|---|
| `/` | Dashboard | Agents online/offline, VMs by state, operations in progress, recent failures |
| `/agents` | Agents | Hostname, URL, status badge, vCPU free/total, disk free/total, last seen |
| `/vms` | VMs | Table with filters (state, agent, name) and row actions |
| `/vms/new` | Create VM | Form below |
| `/vms/:id` | VM detail | Config, network, state, operations, actions, console tab |
| `/operations` | Operations | Recent operations with status and errors (needs a list endpoint) |

### Create VM form

| Field | Control | Rule |
|---|---|---|
| Name | text | Required; unique once the manager enforces it |
| Agent | select, default *Auto* | Lists online agents |
| vCPUs | number | ≥ 1; ≤ agent free vCPUs when known |
| Memory (MiB) | number | ≥ 128 |
| Kernel path | text or select | Path on the host (no catalogue yet) |
| Root filesystem path | text or select | Path on the host |
| Data disk path | text, optional | |
| TAP device | text | Must already exist on the host |
| Start after create | checkbox | Chains `start` |

Guest IP, MAC, gateway and netmask are assigned by the manager and shown after creation; the form does not ask for them.

---

## 5. State display

| State | Badge | Meaning |
|---|---|---|
| `creating` | blue, spinner | Operation running |
| `stopped` | grey | Configured, not booted |
| `running` | green | Booted |
| `error` | red | Operation failed or process died; show the operation error |
| `unknown` | amber | Planned: owning agent is offline |
| `deleted` | hidden | Filter out |

Agent status: `online` green, `offline` red. Use text or an icon as well as colour.

---

## 6. Behaviour

### Actions by VM state

| VM state | Start | Stop | Reboot | Delete | Console |
|---|---|---|---|---|---|
| `stopped` | yes | – | – | yes | no |
| `running` | – | yes | yes | yes | yes |
| `creating` | – | – | – | – | no |
| `error` | retry | – | – | yes | no |
| `unknown` | – | – | – | with warning | no |

Delete asks for confirmation, repeats the VM name, and states that disk files stay on the host.

### Operation feedback

1. An action returns `{operation_id}`; show a pending toast and a transitional badge.
2. `operation.updated` events update the toast. If the socket is down, poll `GET /operations/{id}` every 2 s until terminal.
3. On `failed`, show the operation's `error` text and a link to the VM detail.

### Live updates

- Open the events WebSocket once per session; patch the VM and agent caches from events.
- On every reconnect refetch `/vms` and `/agents`, since missed events are not replayed.
- Show a *live updates disconnected* banner while reconnecting.

### Console

- Offer it only when the VM is `running`. The agent answers `409` if it no longer holds the VM process (for example after an agent restart); show "console not available".
- Text frames in and out; xterm.js renders. Close the socket when the tab closes.
- The console is the raw Firecracker stdin/stdout. A login prompt appears only if the guest image runs one on the serial port `ttyS0`.

---

## 7. Code structure

```
kvm-frontend/
  src/
    api/         # fetch wrappers, types, plain-text error mapping
    hooks/       # useEvents (WebSocket), useOperation, useVms, useAgents
    components/  # StateBadge, ConfirmDialog, OperationToast, Console
    pages/       # Dashboard, Agents, Vms, VmNew, VmDetail
    utils/
  tests/
```

---

## 8. Backend changes the UI needs

| Need | Today | Request to the manager |
|---|---|---|
| Same-origin or CORS | Neither | Serve `dist/` from the manager, or add configurable CORS |
| Hide deleted VMs | Returned in lists | Filter, or support `?state=` |
| Operations list | Only `GET /operations/{id}` | `GET /operations?vm_id=&limit=` |
| JSON errors | Plain text | `{code, message}` |
| VM state when agent offline | Not reflected | `unknown` state |
| Authentication | None | Bearer token; add a login page |
| Host metrics | Not reported | Heartbeat fields from the agent, shown on the agents page |

---

## 9. Non-functional

| Area | Target |
|---|---|
| Scale | Tens of agents, hundreds of VMs; add server-side paging beyond that |
| Browsers | Current Chrome, Firefox, Edge, Safari |
| Accessibility | Status by text or icon as well as colour |
| Security | Render console and error text as plain text; keep tokens in memory |

---

## 10. Build order

1. Scaffold, API client, dev proxy.
2. VM list, detail, actions with operation toasts.
3. Create form with *Start after create*.
4. Events WebSocket and cache patching.
5. Agents page and dashboard.
6. Console tab.
7. Exec panel and login, once the manager provides them.

Until the manager is ready for a feature, develop against a small mock server returning the shapes in section 2.

---

## 11. Open questions

1. Pick kernels and root filesystems from a list (needs a catalogue API) or type paths?
2. Single admin, or roles in the POC?
3. Charts of agent metrics, or only current values?
4. Is the console enough for "execute commands", or is a separate exec panel required?
