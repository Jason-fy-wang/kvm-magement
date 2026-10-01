# kvm-agent

`kvm-agent` is the host-local Go service for the Firecracker-based KVM manager.
It exposes VM lifecycle APIs to the manager and talks to a Firecracker process
through its Unix domain socket API.

## Run

The agent is intended to run on a Linux physical server with Firecracker
installed:

```sh
go run ./cmd/kvm-agent \
  -agent-id host-1 \
  -listen :9090 \
  -public-url http://10.0.0.11:9090 \
  -manager-url ws://10.0.0.10:8080/ws/agents \
  -log-level info \
  -firecracker /usr/local/bin/firecracker \
  -data-dir /var/lib/kvm-agent
```

`-manager-url` enables the outbound WebSocket connection used for agent
registration and event delivery. `-public-url` tells the Manager where to send
REST lifecycle requests.

Use `-log-level debug` when diagnosing startup, Firecracker configuration,
operation transitions, Manager WebSocket reconnects, console connections, or
HTTP timing:

```sh
go run ./cmd/kvm-agent -agent-id host-1 -log-level debug
```

Logs are structured with fields such as `vm_id`, `operation_id`, `pid`,
`socket`, `agent_id`, HTTP status, and request duration.

## HTTP API

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/healthz` | Health check |
| GET | `/v1/agent` | Agent identity and version |
| GET | `/v1/vms` | List VMs |
| POST | `/v1/vms` | Create and configure a VM |
| GET | `/v1/vms/{id}` | Read a VM |
| DELETE | `/v1/vms/{id}` | Stop and remove the VM record |
| POST | `/v1/vms/{id}/start` | Start a VM |
| POST | `/v1/vms/{id}/stop` | Stop a VM |
| POST | `/v1/vms/{id}/reboot` | Send Ctrl-Alt-Delete |
| GET | `/v1/operations/{id}` | Read an asynchronous operation |
| GET | `/v1/vms/{id}/console` | Browser WebSocket console |
| GET | `/v1/events` | Agent WebSocket event stream |

Create, start, stop, reboot, and delete return `202 Accepted` with an
`operation_id`. The manager can poll `/v1/operations/{id}` and subscribe to
events over WebSocket.

The current store is in memory. VM image files are never deleted by the agent;
the kernel, root filesystem, and disk paths supplied during creation remain on
the host after the VM record is removed.

## Current limitations

- No authentication yet; deploy only on a trusted network during the MVP.
- TAP devices must already exist. The agent passes the configured TAP name to
  Firecracker but does not create or configure the host network device yet.
- VM records and operations are lost when the agent restarts.
- The browser console currently bridges Firecracker's standard input/output;
  production console setup should make the serial console and log paths
  explicit.
