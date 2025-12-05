package model

import (
	"fmt"
	"sync"
)

type CachedUTXOSet struct {
	db        *BadgerUTXOSet
	mu        sync.RWMutex
	Cache     map[string]UTXO
	addrIndex map[string][]string // address → list of "txid:index"
}

func NewCachedUTXOSet(db *BadgerUTXOSet) *CachedUTXOSet {
	return &CachedUTXOSet{
		db:        db,
		Cache:     make(map[string]UTXO),
		addrIndex: make(map[string][]string),
	}
}

// key "txid:index"
func keyOf(txid string, index int) string {
	return fmt.Sprintf("%s:%d", txid, index)
}

// =======================================================
// GET — RAM first → DB fallback → push into RAM
// =======================================================
func (c *CachedUTXOSet) Get(txid string, index int) (UTXO, bool) {
	k := keyOf(txid, index)

	// RAM first
	c.mu.RLock()
	utxo, ok := c.Cache[k]
	c.mu.RUnlock()
	if ok {
		return utxo, true
	}

	// DB fallback (skip if DB not available)
	if c.db == nil {
		return UTXO{}, false
	}

	dbUtxo, found := c.db.Get(txid, index)
	if !found {
		return UTXO{}, false
	}

	// put into Cache
	c.mu.Lock()
	c.Cache[k] = dbUtxo
	if len(dbUtxo.Vout.ScriptPubKey.Addresses) > 0 {
		addr := dbUtxo.Vout.ScriptPubKey.Addresses[0]
		c.addrIndex[addr] = appendMissing(c.addrIndex[addr], k)
	}
	c.mu.Unlock()

	return dbUtxo, true
}

// =======================================================
// PUT — DB first → update RAM
// =======================================================
func (c *CachedUTXOSet) Put(txid string, index int, out VOUT) error {
	// Try to update DB if available (for persistence)
	if c.db != nil {
		if err := c.db.Put(txid, index, out); err != nil {
			return err
		}
	}

	k := keyOf(txid, index)
	utxo := UTXO{Txid: txid, Index: index, Vout: out}

	c.mu.Lock()
	c.Cache[k] = utxo
	if len(out.ScriptPubKey.Addresses) > 0 {
		addr := out.ScriptPubKey.Addresses[0]
		c.addrIndex[addr] = appendMissing(c.addrIndex[addr], k)
	}
	c.mu.Unlock()

	return nil
}

// =======================================================
// DELETE — DB first → update RAM
// =======================================================
func (c *CachedUTXOSet) Delete(txid string, index int) error {
	// Try to delete from DB if available
	if c.db != nil {
		if err := c.db.Delete(txid, index); err != nil {
			return err
		}
	}

	k := keyOf(txid, index)

	c.mu.Lock()

	if utxo, ok := c.Cache[k]; ok {
		if len(utxo.Vout.ScriptPubKey.Addresses) > 0 {
			addr := utxo.Vout.ScriptPubKey.Addresses[0]
			c.addrIndex[addr] = removeKey(c.addrIndex[addr], k)
		}
	}

	delete(c.Cache, k)

	c.mu.Unlock()
	return nil
}

// =======================================================
// UPDATE WITH TX — atomic DB update + RAM update
// =======================================================
func (c *CachedUTXOSet) UpdateWithTransaction(tx Transaction) error {

	// 1) update DB atomically (if DB available)
	if c.db != nil {
		if err := c.db.UpdateWithTransaction(tx); err != nil {
			return err
		}
	}

	// 2) then update RAM
	c.mu.Lock()

	// remove inputs
	for _, vin := range tx.Vin {
		if vin.Txid == "" {
			continue
		}
		k := keyOf(vin.Txid, vin.Vout)
		if utxo, ok := c.Cache[k]; ok {
			if len(utxo.Vout.ScriptPubKey.Addresses) > 0 {
				addr := utxo.Vout.ScriptPubKey.Addresses[0]
				c.addrIndex[addr] = removeKey(c.addrIndex[addr], k)
			}
		}
		delete(c.Cache, k)
	}

	// add outputs
	for _, out := range tx.Vout {
		k := keyOf(tx.Txid, out.N)
		u := UTXO{Txid: tx.Txid, Index: out.N, Vout: out}
		c.Cache[k] = u

		if len(out.ScriptPubKey.Addresses) > 0 {
			addr := out.ScriptPubKey.Addresses[0]
			c.addrIndex[addr] = appendMissing(c.addrIndex[addr], k)
		}
	}

	c.mu.Unlock()
	return nil
}

// =======================================================
// FAST FIND — RAM first (with lock held), then DB fallback
// =======================================================
func (c *CachedUTXOSet) FindUTXOsByAddress(addr string) []UTXO {

	// 1) Try RAM index first (fast path)
	c.mu.RLock()
	keys, ok := c.addrIndex[addr]
	c.mu.RUnlock()

	if ok && len(keys) > 0 {
		res := make([]UTXO, 0, len(keys))

		c.mu.RLock()
		for _, k := range keys {
			if u, ok := c.Cache[k]; ok {
				res = append(res, u)
			}
		}
		c.mu.RUnlock()

		if len(res) > 0 {
			return res
		}
	}

	// 2) Fallback → Scan DB by address
	if c.db == nil {
		return nil
	}

	dbUtxos := c.db.FindByAddress(addr)
	if len(dbUtxos) == 0 {
		return nil
	}

	// 3) Insert DB results into RAM (rebuild index)
	c.mu.Lock()
	for _, u := range dbUtxos {

		// RAM key must be "txid:index"
		k := fmt.Sprintf("%s:%d", u.Txid, u.Index)

		c.Cache[k] = u

		// Extract address from ScriptPubKey (to ensure correct mapping)
		if len(u.Vout.ScriptPubKey.Addresses) > 0 {
			realAddr := u.Vout.ScriptPubKey.Addresses[0]
			c.addrIndex[realAddr] = appendMissing(c.addrIndex[realAddr], k)
		}
	}
	c.mu.Unlock()

	return dbUtxos
}

func (c *CachedUTXOSet) Close() {
	if c.db != nil {
		c.db.Close()
	}
}

// =======================================================
// HELPERS
// =======================================================
func appendMissing(slice []string, k string) []string {
	for _, s := range slice {
		if s == k {
			return slice
		}
	}
	return append(slice, k)
}

func removeKey(slice []string, k string) []string {
	for i, s := range slice {
		if s == k {
			slice[i] = slice[len(slice)-1]
			return slice[:len(slice)-1]
		}
	}
	return slice
}
