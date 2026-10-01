package agent

import (
	"testing"
	"time"
)

func TestStoreCopiesValues(t *testing.T) {
	s := newStore()
	vm := &VM{ID: "vm-1", State: VMStateStopped, CreatedAt: time.Now()}
	s.putVM(vm)

	vm.State = VMStateRunning
	got, err := s.getVM("vm-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != VMStateStopped {
		t.Fatalf("store was mutated through caller pointer: %s", got.State)
	}
}

func TestStoreDelete(t *testing.T) {
	s := newStore()
	s.putVM(&VM{ID: "vm-1"})
	if err := s.deleteVM("vm-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.getVM("vm-1"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
