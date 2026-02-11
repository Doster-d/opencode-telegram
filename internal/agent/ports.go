package agent

import (
	"sort"
	"sync"

	"opencode-telegram/internal/proxy/contracts"
)

type PortAllocator struct {
	mu sync.Mutex

	min int
	max int

	projectPort map[string]int
	used        map[int]bool
}

func NewPortAllocator(minPort, maxPort int) *PortAllocator {
	return &PortAllocator{
		min:         minPort,
		max:         maxPort,
		projectPort: make(map[string]int),
		used:        make(map[int]bool),
	}
}

func (p *PortAllocator) Allocate(projectID string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if current, ok := p.projectPort[projectID]; ok {
		return current, nil
	}
	for port := p.min; port <= p.max; port++ {
		if !p.used[port] {
			p.used[port] = true
			p.projectPort[projectID] = port
			return port, nil
		}
	}
	return 0, contracts.APIError{Code: contracts.ErrPortExhausted, Message: "port range is exhausted"}
}

func (p *PortAllocator) Release(projectID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if port, ok := p.projectPort[projectID]; ok {
		delete(p.projectPort, projectID)
		delete(p.used, port)
	}
}

func (p *PortAllocator) SnapshotUsed() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	ports := make([]int, 0, len(p.used))
	for port := range p.used {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}
