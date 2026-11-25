# KVM MicroVM Management – Frontend Design

**Status:** POC implementation · **Based on:** the current `kvm-manager` API in this repository

Frontend implementation notes for the manager API. The UI talks only to `kvm-manager`.

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
| Styling | Tailwind CSS plus the dashboard component stylesheet |
| Server state | Typed fetch client and React state |
| Live updates | Native `WebSocket` to `/api/v1/events`, reconnect with backoff and refresh on reconnect |
| Operation status | Poll `GET /operations/{id}` every 2 seconds until terminal |
| Console | Raw text sent and received over the console WebSocket |
| API types | Hand-written in `src/api/types.ts` until the manager publishes OpenAPI |
| Validation | `npm run build` (TypeScript check and Vite production build) |

---

## 4. Pages

| View | Page | Content |
|---|---|---|
| Overview | Dashboard | Agent health, VM inventory, capacity, and current operations |
| All VMs | VM view | Filter by state and search by VM name, ID, host, or guest IP |
| Physical hosts | Agent view | Host status, vCPU availability, disk free space, and last heartbeat |
| Operations | Operation view | Operations created in this browser session; the manager has no list endpoint |
| VM detail | Drawer | Configuration, network, status, lifecycle actions, and console |

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
| Start after create | checkbox | Wait for create operation success, then call `start` |

Guest IP, MAC, gateway and netmask are assigned by the manager and shown after creation; the form does not ask for them.

---

## 5. State display

| State | Badge | Meaning |
|---|---|---|
| `creating` | amber | Operation running |
| `stopped` | grey | Configured, not booted |
| `running` | green | Booted |
| `error` | red | Operation failed or process died; show the operation error |
| `unknown` | amber | Manager-reported unknown state |
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

1. An action returns `{operation_id, vm_id, status}`; show a pending notification.
2. Poll `GET /operations/{id}` every 2 s until `succeeded` or `failed`. Events trigger inventory refreshes; they do not reliably include the manager's operation ID.
3. On failure, show the operation's plain-text `error`.

### Live updates

- Open the events WebSocket once per session; patch VM data from events.
- On every reconnect refetch `/vms` and `/agents`, since missed events are not replayed.
- Show a *live updates disconnected* banner while reconnecting.

### Console

- Offer it only when the VM is `running`. The agent answers `409` if it no longer holds the VM process (for example after an agent restart); show "console not available".
- Raw text frames in and out. Close the socket when the drawer closes.
- The console is the raw Firecracker stdin/stdout. A login prompt appears only if the guest image runs one on the serial port `ttyS0`.

---

## 7. Code structure

```
kvm-front/
  src/
    api/         # typed manager client and API models
    component/   # reusable VM, agent, dialog, status, and console UI
    utils/       # shared formatting helpers
    App.tsx      # screen composition, live events, and operation tracking
    styles.css
```

---

## 8. Backend changes the UI needs

| Need | Today | Future request to the manager |
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

1. Typed API client and Vite dev proxy.
2. VM inventory, details, and lifecycle actions.
3. Create form with optional *Start after create*.
4. Reconnecting events WebSocket and operation polling.
5. Agent overview and dashboard.
6. Raw-text console.
7. Operation history and user authentication require manager API support.

Until the manager is ready for a feature, develop against a small mock server returning the shapes in section 2.

---

## 11. Open questions

1. Pick kernels and root filesystems from a list (needs a catalogue API) or type paths?
2. Single admin, or roles in the POC?
3. Charts of agent metrics, or only current values?
4. Is the console enough for "execute commands", or is a separate exec panel required?
