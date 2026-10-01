package manager

import (
	"context"
	"fmt"
	"net"
)

func macFromIP(ip net.IP) string {
	ipv4 := ip.To4()
	return fmt.Sprintf("06:00:%02x:%02x:%02x:%02x", ipv4[0], ipv4[1], ipv4[2], ipv4[3])
}

func (s *Server) allocateNetwork(ctx context.Context, vmID, agentID string) (NetworkAllocation, error) {
	s.networkMu.Lock()
	defer s.networkMu.Unlock()

	one := make(net.IP, len(s.network.IP))
	copy(one, s.network.IP)
	first := new(bigInt).SetBytes(one.To4())
	first.Add(bigOne)
	for candidate := first; s.network.Contains(candidate.Bytes()); candidate.Add(bigOne) {
		ip := net.IP(candidate.Bytes()).To4()
		if ip.Equal(s.gatewayIP) || ip[3] == 255 {
			continue
		}
		ipText := ip.String()
		allocated, err := s.store.isIPAllocated(ctx, ipText)
		if err != nil {
			return NetworkAllocation{}, err
		}
		if allocated {
			continue
		}
		allocation := NetworkAllocation{GuestIP: ipText, GuestMAC: macFromIP(ip), GatewayIP: s.gatewayIP.String(), Netmask: net.IP(s.network.Mask).String()}
		if err := s.store.createIPAllocation(ctx, allocation, vmID, agentID); err != nil {
			continue
		}
		return allocation, nil
	}
	return NetworkAllocation{}, fmt.Errorf("network %s has no available IP addresses", s.network.String())
}

// The small wrapper keeps the allocator readable while using integer IP math.
type bigInt struct{ value uint32 }

var bigOne = &bigInt{value: 1}

func (i *bigInt) SetBytes(b []byte) *bigInt {
	i.value = uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return i
}
func (i *bigInt) Add(other *bigInt) *bigInt { i.value += other.value; return i }
func (i *bigInt) Bytes() []byte {
	return []byte{byte(i.value >> 24), byte(i.value >> 16), byte(i.value >> 8), byte(i.value)}
}
