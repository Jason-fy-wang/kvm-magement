import { HardDrive, Server } from "lucide-react";
import type { Agent } from "../api/types";
import { formatBytes, formatTime } from "../utils/format";
import { StatusBadge } from "./StatusBadge";

export function AgentPanel({ agents }: { agents: Agent[] }) {
  return (
    <div className="side-panel">
      <div className="side-panel-header">
        <div>
          <h2>Physical hosts</h2>
          <p>Agent heartbeat overview</p>
        </div>
        <Server size={16} className="muted-icon" />
      </div>
      <div className="agent-list">
        {agents.length === 0 ? (
          <div className="side-empty">No agents connected</div>
        ) : (
          agents.map((agent) => {
            const used = Math.max(agent.vcpu_total - agent.vcpu_available, 0);
            const percentage = agent.vcpu_total
              ? Math.min(Math.round((used / agent.vcpu_total) * 100), 100)
              : 0;
            return (
              <div className="agent-row" key={agent.id}>
                <div className="agent-avatar">
                  <Server size={15} />
                </div>
                <div className="agent-copy">
                  <strong>{agent.hostname || agent.id}</strong>
                  <span>
                    {agent.id} · {agent.url}
                  </span>
                  <span>
                    {agent.vcpu_available} vCPU free ·{" "}
                    {formatBytes(agent.disk_free_bytes)} /{" "}
                    {formatBytes(agent.disk_total_bytes)} disk · Seen{" "}
                    {formatTime(agent.last_seen)}
                  </span>
                  <div className="mini-bar">
                    <span style={{ width: `${percentage}%` }} />
                  </div>
                </div>
                <StatusBadge state={agent.status} />
              </div>
            );
          })
        )}
      </div>
      <div className="agent-panel-footer">
        <HardDrive size={14} />
        Disk capacity reflects the latest agent registration.
      </div>
    </div>
  );
}
