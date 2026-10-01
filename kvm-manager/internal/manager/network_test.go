package manager

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAllocateNetworkEncodesIPInMAC(t *testing.T) {
	srv, err := NewServer(Config{DatabasePath: filepath.Join(t.TempDir(), "manager.db"), NetworkCIDR: "172.20.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.store.close()

	allocation, err := srv.allocateNetwork(context.Background(), "vm-1", "host-1")
	if err != nil {
		t.Fatal(err)
	}
	if allocation.GuestIP != "172.20.0.2" {
		t.Fatalf("guest IP = %s, want 172.20.0.2", allocation.GuestIP)
	}
	if allocation.GuestMAC != "06:00:ac:14:00:02" {
		t.Fatalf("guest MAC = %s, want 06:00:ac:14:00:02", allocation.GuestMAC)
	}
	if allocation.GatewayIP != "172.20.0.1" || allocation.Netmask != "255.255.0.0" {
		t.Fatalf("network allocation = %+v", allocation)
	}
}
