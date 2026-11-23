# KVM MicroVM Management – Frontend Design

**Status:** Draft v0.1 (POC) · **Layer:** Frontend → Center Service

> **\[Assumption\]** The repo has no frontend folder yet (only `kvm-agent` and `kvm-manager`), so this is a greenfield design. The stack below is a recommendation; swap it for your team's usual one if different.

## 1. Responsibilities

- Present nodes, VMs, tasks, and their live status.
- Let an operator create, start, stop, and delete VMs and run commands on nodes.
- Give clear feedback for asynchronous operations (pending → success/failure).
- Contain **no business logic** beyond form validation and display; the center service is authoritative.

## 2. Recommended stack

| Concern | Choice | Notes |
| --- | --- | --- |
| Framework | React + TypeScript (Vue 3 is equally fine) | Typed API models |
| Build | Vite | Fast dev loop |
| UI kit | Ant Design or MUI | Tables, forms, modals out of the box |
| Data fetching | TanStack Query | Caching, polling, invalidation |
| Routing | React Router |  |
| API client | Generated from the center's OpenAPI spec | Prevents contract drift |
| Live updates | `EventSource` (SSE) with polling fallback | Simple, one-way push is enough |
| Charts | Lightweight chart lib (e.g. ECharts/Recharts) | Sparklines, resource bars |
| Tests | Vitest + Testing Library; Playwright smoke tests |  |

## 3. Information architecture

| Route | Page | Purpose |
| --- | --- | --- |
| `/login` | Login | Token or username/password |
| `/` | Dashboard | Totals: nodes online/offline, VMs by state, recent tasks, alerts |
| `/nodes` | Nodes | Table of hosts with status and resource usage |
| `/nodes/:id` | Node detail | Facts, metrics charts, VMs on node, command console, event log |
| `/vms` | VMs | Filterable table with row actions |
| `/vms/new` | Create VM | Form |
| `/vms/:id` | VM detail | Config, state, timeline, actions, related tasks |
| `/tasks` | Tasks | History of operations with status and errors |
| `/audit` | Audit log (optional) | Who did what |

## 4. Key screens

### 4.1 Dashboard

- Cards: Nodes (online / offline), VMs (running / stopped / error / unknown), Tasks in progress, Failures in the last 24 h.
- A short "needs attention" list: offline nodes, VMs in `error`, failed tasks.

### 4.2 VM list

- Columns: Name, Node, State (badge), vCPU, Memory, Disk, IP, Created, Actions.
- Filters: state, node, name search. Sort on any column.
- Row actions: Start, Stop, Delete, Details. Actions are enabled by state (see 6.2).
- Bulk actions are post-POC.

### 4.3 Create VM form

- Fields: name, vCPUs, memory (MB/GB), disk (GB), image/kernel (dropdown from center), optional node (default "auto"), optional labels.
- Client-side validation (required, ranges, name pattern); server errors mapped back onto fields.
- Submit → `202` → navigate to VM detail with a "Creating…" state tied to the task.

### 4.4 VM detail

- Header: name, state badge, node link, action buttons.
- Tabs: **Overview** (config, IP, uptime), **Tasks** (related operations and errors), **Events**.
- `last_error` is shown prominently when state is `error`.

### 4.5 Node detail

- Facts (CPU/memory/disk totals, agent version, last heartbeat).
- Live resource bars plus short history charts.
- VMs hosted on this node.
- **Command console** (admin only): input, timeout selector, output viewer, history.

## 5. State display conventions

| State | Badge | Meaning |
| --- | --- | --- |
| `creating` | blue, spinner | Task running |
| `running` | green | Healthy |
| `stopped` | grey | Intentionally off |
| `error` | red | See `last_error` |
| `unknown` | amber | Node offline; real state not known |
| `deleting` | grey, spinner | Task running |

Node status: `online` (green), `offline` (red), `draining` (amber).

## 6. Behaviour

### 6.1 Async operation handling

1. User triggers an action → call returns `{task_id}`.
2. UI shows an optimistic transitional state and a toast "Operation submitted".
3. State changes arrive via SSE; if SSE is down, poll `GET /tasks/{id}` every 2 s (back off to 5 s) until terminal.
4. On `failed`, show the task's error message with a "View task" link.

### 6.2 Action availability

| VM state | Start | Stop | Delete |
| --- | --- | --- | --- |
| `running` | – | yes | yes (confirm) |
| `stopped` | yes | – | yes (confirm) |
| `error` | retry | – | yes (confirm) |
| `unknown` | – | – | admin only, with warning |
| `creating` / `deleting` | – | – | – |

Destructive actions require a confirmation dialog that repeats the VM name.

### 6.3 Live updates

- One SSE connection per session at `/api/v1/events`, events like `vm.updated`, `node.updated`, `task.updated`.
- Event handler patches the TanStack Query cache or invalidates the relevant query.
- Reconnect with exponential backoff; on reconnect, refetch visible lists to avoid missed events.

### 6.4 Error handling

- Center error format `{code, message, request_id}` is mapped to a toast; `request_id` is shown in a "copy details" link to ease debugging.
- `401` → redirect to login; `403` → "not allowed"; network error → banner "Cannot reach server".

## 7. Code structure

```
kvm-frontend/
  src/
    api/          # generated client + thin wrappers
    components/   # StateBadge, ResourceBar, ConfirmDialog, TaskToast ...
    features/
      nodes/  vms/  tasks/  dashboard/  auth/
    hooks/        # useEvents (SSE), usePollTask ...
    routes/
    store/        # minimal: auth/session only
    utils/
  tests/
```

## 8. Auth and security

- POC: login returns a token; keep it in memory (and an httpOnly cookie if the center sets one), not `localStorage` if avoidable.
- Send `Authorization: Bearer ...`; handle expiry.
- Hide admin-only UI (exec console, force delete) based on role, but the center enforces it.
- Escape all server-provided strings (VM names, command output); render command output as plain text.

## 9. Non-functional requirements

| Area | Target |
| --- | --- |
| Scale | Up to a few hundred VMs; use server-side pagination beyond 100 rows |
| Responsiveness | Desktop first; usable on tablet |
| Browsers | Latest Chrome, Firefox, Edge, Safari |
| Accessibility | Keyboard-operable actions; colour + text/icon for states (not colour alone) |
| Performance | First meaningful paint under 2 s on LAN |

## 10. Development approach

- Develop against a **mock server** generated from the OpenAPI spec so the UI doesn't wait for the center.
- Build order: scaffold + auth → VM list → create form → VM detail → nodes → SSE → console → dashboard.
- Playwright smoke test: login → create VM → see `running` → stop → delete.

## 11. Open questions

1. Single admin role only, or operator/viewer split in the POC?
2. Is a VM serial console / VNC view desired later? (Needs a WebSocket proxy through center + agent.)
3. Should the UI let users pick a node, or always auto-schedule?
4. How are images and kernels listed: fixed config on center or discovered from agents?
