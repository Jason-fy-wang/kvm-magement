package manager

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

var errNotFound = errors.New("not found")

type store struct {
	db        *sql.DB
	networkMu sync.Mutex
}

func openStore(path string) (*store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS agents (
 id TEXT PRIMARY KEY, hostname TEXT NOT NULL, url TEXT NOT NULL, status TEXT NOT NULL,
 vcpu_total INTEGER NOT NULL DEFAULT 0, vcpu_available INTEGER NOT NULL DEFAULT 0,
 disk_total_bytes INTEGER NOT NULL DEFAULT 0, disk_free_bytes INTEGER NOT NULL DEFAULT 0,
 last_seen TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS vms (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, agent_id TEXT NOT NULL, state TEXT NOT NULL,
 vcpu_count INTEGER NOT NULL, memory_mib INTEGER NOT NULL, kernel_path TEXT NOT NULL,
 rootfs_path TEXT NOT NULL, disk_path TEXT NOT NULL DEFAULT '', tap_device TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 FOREIGN KEY(agent_id) REFERENCES agents(id)
);
CREATE TABLE IF NOT EXISTS ip_allocations (
 ip TEXT PRIMARY KEY, vm_id TEXT UNIQUE NOT NULL, agent_id TEXT NOT NULL,
 mac TEXT UNIQUE NOT NULL, gateway_ip TEXT NOT NULL, netmask TEXT NOT NULL,
 status TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS operations (
 id TEXT PRIMARY KEY, vm_id TEXT NOT NULL, agent_id TEXT NOT NULL, agent_operation_id TEXT NOT NULL DEFAULT '',
 type TEXT NOT NULL, status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_operations_agent_operation ON operations(agent_id, agent_operation_id);
`)
	if err != nil {
		return err
	}
	// Keep the migration compatible with databases created by the first MVP.
	for _, column := range []string{
		"guest_ip TEXT NOT NULL DEFAULT ''",
		"guest_mac TEXT NOT NULL DEFAULT ''",
		"gateway_ip TEXT NOT NULL DEFAULT ''",
		"netmask TEXT NOT NULL DEFAULT ''",
	} {
		if err := s.addColumnIfMissing(ctx, "vms", column); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) addColumnIfMissing(ctx context.Context, table, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	name := definition[:len(definition)-len(" TEXT NOT NULL DEFAULT ''")]
	if name == definition {
		name = definition[:len(definition)-len(" INTEGER NOT NULL DEFAULT 0")]
	}
	for rows.Next() {
		var cid int
		var existing, columnType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &existing, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if existing == name {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+definition)
	return err
}

func (s *store) close() error { return s.db.Close() }

func (s *store) upsertAgent(ctx context.Context, a Agent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO agents (id,hostname,url,status,vcpu_total,vcpu_available,disk_total_bytes,disk_free_bytes,last_seen)
VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET hostname=excluded.hostname,url=excluded.url,status=excluded.status,
vcpu_total=excluded.vcpu_total,vcpu_available=excluded.vcpu_available,disk_total_bytes=excluded.disk_total_bytes,
disk_free_bytes=excluded.disk_free_bytes,last_seen=excluded.last_seen`, a.ID, a.Hostname, a.URL, a.Status, a.VCPUTotal, a.VCPUAvailable, a.DiskTotalBytes, a.DiskFreeBytes, a.LastSeen.Format(time.RFC3339Nano))
	return err
}

func scanAgent(row interface{ Scan(...any) error }) (Agent, error) {
	var a Agent
	var last string
	err := row.Scan(&a.ID, &a.Hostname, &a.URL, &a.Status, &a.VCPUTotal, &a.VCPUAvailable, &a.DiskTotalBytes, &a.DiskFreeBytes, &last)
	if err != nil {
		return a, err
	}
	a.LastSeen, _ = time.Parse(time.RFC3339Nano, last)
	return a, nil
}

func (s *store) listAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,hostname,url,status,vcpu_total,vcpu_available,disk_total_bytes,disk_free_bytes,last_seen FROM agents ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Agent{}
	for rows.Next() {
		a, scanErr := scanAgent(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func (s *store) getAgent(ctx context.Context, id string) (Agent, error) {
	a, err := scanAgent(s.db.QueryRowContext(ctx, `SELECT id,hostname,url,status,vcpu_total,vcpu_available,disk_total_bytes,disk_free_bytes,last_seen FROM agents WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, errNotFound
	}
	return a, err
}

func (s *store) setAgentStatus(ctx context.Context, id, status string, seen time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE agents SET status=?,last_seen=? WHERE id=?`, status, seen.Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return errNotFound
	}
	return nil
}

func (s *store) createVM(ctx context.Context, vm VM) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO vms (id,name,agent_id,state,vcpu_count,memory_mib,kernel_path,rootfs_path,disk_path,tap_device,guest_ip,guest_mac,gateway_ip,netmask,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, vm.ID, vm.Name, vm.AgentID, vm.State, vm.VCPUCount, vm.MemoryMiB, vm.KernelPath, vm.RootFSPath, vm.DiskPath, vm.TapDevice, vm.GuestIP, vm.GuestMAC, vm.GatewayIP, vm.Netmask, vm.CreatedAt.Format(time.RFC3339Nano), vm.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func scanVM(row interface{ Scan(...any) error }) (VM, error) {
	var vm VM
	var created, updated string
	err := row.Scan(&vm.ID, &vm.Name, &vm.AgentID, &vm.State, &vm.VCPUCount, &vm.MemoryMiB, &vm.KernelPath, &vm.RootFSPath, &vm.DiskPath, &vm.TapDevice, &vm.GuestIP, &vm.GuestMAC, &vm.GatewayIP, &vm.Netmask, &created, &updated)
	if err != nil {
		return vm, err
	}
	vm.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	vm.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return vm, nil
}

func (s *store) listVMs(ctx context.Context) ([]VM, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,agent_id,state,vcpu_count,memory_mib,kernel_path,rootfs_path,disk_path,tap_device,guest_ip,guest_mac,gateway_ip,netmask,created_at,updated_at FROM vms ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []VM{}
	for rows.Next() {
		vm, scanErr := scanVM(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, vm)
	}
	return result, rows.Err()
}

func (s *store) getVM(ctx context.Context, id string) (VM, error) {
	vm, err := scanVM(s.db.QueryRowContext(ctx, `SELECT id,name,agent_id,state,vcpu_count,memory_mib,kernel_path,rootfs_path,disk_path,tap_device,guest_ip,guest_mac,gateway_ip,netmask,created_at,updated_at FROM vms WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return VM{}, errNotFound
	}
	return vm, err
}

func (s *store) updateVMState(ctx context.Context, id, state string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE vms SET state=?,updated_at=? WHERE id=?`, state, at.Format(time.RFC3339Nano), id)
	return err
}

func (s *store) createOperation(ctx context.Context, op Operation) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO operations (id,vm_id,agent_id,agent_operation_id,type,status,error,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, op.ID, op.VMID, op.AgentID, op.AgentOperationID, op.Type, op.Status, op.Error, op.CreatedAt.Format(time.RFC3339Nano), op.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *store) getOperation(ctx context.Context, id string) (Operation, error) {
	var op Operation
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,vm_id,agent_id,agent_operation_id,type,status,error,created_at,updated_at FROM operations WHERE id=?`, id).Scan(&op.ID, &op.VMID, &op.AgentID, &op.AgentOperationID, &op.Type, &op.Status, &op.Error, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return op, errNotFound
	}
	op.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	op.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return op, err
}

func (s *store) bindAgentOperation(ctx context.Context, id, agentOperationID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE operations SET agent_operation_id=?,status='running',updated_at=? WHERE id=?`, agentOperationID, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *store) findOperationByAgent(ctx context.Context, agentID, agentOperationID string) (Operation, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM operations WHERE agent_id=? AND agent_operation_id=?`, agentID, agentOperationID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Operation{}, errNotFound
	}
	if err != nil {
		return Operation{}, err
	}
	return s.getOperation(ctx, id)
}

func (s *store) updateOperation(ctx context.Context, id, status, operationError string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE operations SET status=?,error=?,updated_at=? WHERE id=?`, status, operationError, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *store) isIPAllocated(ctx context.Context, ip string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM ip_allocations WHERE ip=?`, ip).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *store) createIPAllocation(ctx context.Context, allocation NetworkAllocation, vmID, agentID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO ip_allocations (ip,vm_id,agent_id,mac,gateway_ip,netmask,status) VALUES (?,?,?,?,?,?,?)`, allocation.GuestIP, vmID, agentID, allocation.GuestMAC, allocation.GatewayIP, allocation.Netmask, "allocated")
	return err
}

func (s *store) releaseIPAllocation(ctx context.Context, vmID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ip_allocations WHERE vm_id=?`, vmID)
	return err
}
