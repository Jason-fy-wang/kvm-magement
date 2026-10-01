package agent

import (
	"errors"
	"sync"
)

var ErrNotFound = errors.New("not found")

type store struct {
	mu         sync.RWMutex
	vms        map[string]*VM
	operations map[string]*Operation
}

func newStore() *store {
	return &store{vms: make(map[string]*VM), operations: make(map[string]*Operation)}
}

func (s *store) listVMs() []*VM {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*VM, 0, len(s.vms))
	for _, vm := range s.vms {
		copy := *vm
		result = append(result, &copy)
	}
	return result
}

func (s *store) getVM(id string) (*VM, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vm, ok := s.vms[id]
	if !ok {
		return nil, ErrNotFound
	}
	copy := *vm
	return &copy, nil
}

func (s *store) putVM(vm *VM) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *vm
	s.vms[vm.ID] = &copy
}

func (s *store) deleteVM(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.vms[id]; !ok {
		return ErrNotFound
	}
	delete(s.vms, id)
	return nil
}

func (s *store) putOperation(op *Operation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *op
	s.operations[op.ID] = &copy
}

func (s *store) getOperation(id string) (*Operation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	op, ok := s.operations[id]
	if !ok {
		return nil, ErrNotFound
	}
	copy := *op
	return &copy, nil
}
