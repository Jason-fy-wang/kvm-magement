# kvm-manager

The Go control-plane service for the Firecracker KVM platform. It provides the
React-facing REST API, stores manager state in SQLite, chooses an online agent,
and forwards lifecycle requests to that agent.

## Run

```sh
go run ./cmd/kvm-manager -listen :8080 -db ./kvm-manager.db -network-cidr 172.20.0.0/16
```

Agents connect to `GET /ws/agents` and register with a message containing
`agent_id`, `hostname`, and `url`. The `url` is the agent's HTTP address, for
example `http://10.0.0.11:9090`.

Frontend WebSocket events are available at:

```text
ws://manager-host:8080/api/v1/events
```

The main frontend REST endpoints are:

```text
GET    /api/v1/agents
GET    /api/v1/vms
POST   /api/v1/vms
GET    /api/v1/vms/{id}
DELETE /api/v1/vms/{id}
POST   /api/v1/vms/{id}/start
POST   /api/v1/vms/{id}/stop
POST   /api/v1/vms/{id}/reboot
GET    /api/v1/operations/{id}
GET    /api/v1/vms/{id}/console
```

The service currently has no authentication. SQLite is intentionally used for
the MVP and can later be replaced behind the store boundary.

The Manager allocates one IP from the shared bridge CIDR for each VM and
derives its MAC as `06:00:<IPv4 bytes>`. The first usable address is reserved
as the bridge gateway. The per-VM TAP device does not receive an IP; it is
attached to the host bridge.

Set the Manager log level through the configured `slog` handler when embedding
the server. Request completion, agent registration, agent API calls, scheduling
decisions, operation transitions, and failures are emitted as structured log
fields.
