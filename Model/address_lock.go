package model

import "sync"

// addrLocks stores one *sync.Mutex per address.
// Use getAddrLock(addr) to obtain the mutex for an address.
var addrLocks sync.Map // map[string]*sync.Mutex

// getAddrLock returns a pointer to a mutex for the given address.
// It will create the mutex on-demand in a thread-safe manner.
func GetAddrLock(addr string) *sync.Mutex {
	if v, ok := addrLocks.Load(addr); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := addrLocks.LoadOrStore(addr, mu)
	return actual.(*sync.Mutex)
}
