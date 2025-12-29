package model

import (
	"fmt"
	"sync"
)

type UTXOSet struct {
	mu sync.RWMutex

	// primary storage: "txid:vout" -> UTXO
	utxos map[string]UTXO

	// secondary index: address -> set("txid:vout")
	addrIndex map[string]map[string]struct{}
}

func NewUTXOSet() *UTXOSet {
	return &UTXOSet{
		utxos:     make(map[string]UTXO),
		addrIndex: make(map[string]map[string]struct{}),
	}
}

func (u *UTXOSet) Get(txid string, vout int) (UTXO, bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()

	utxo, ok := u.utxos[string(utxoKey(txid, vout))]
	return utxo, ok
}

func (u *UTXOSet) Add(txid string, vout int, voutData VOUT) {
	u.mu.Lock()
	defer u.mu.Unlock()

	key := string(utxoKey(txid, vout))

	utxo := UTXO{
		Txid:  txid,
		Index: vout,
		Vout:  voutData,
	}

	u.utxos[key] = utxo

	// ✅ index cho TẤT CẢ addresses
	for _, addr := range voutData.ScriptPubKey.Addresses {
		if _, ok := u.addrIndex[addr]; !ok {
			u.addrIndex[addr] = make(map[string]struct{})
		}
		u.addrIndex[addr][key] = struct{}{}
	}
}

func (u *UTXOSet) Remove(txid string, vout int) {
	u.mu.Lock()
	defer u.mu.Unlock()

	key := string(utxoKey(txid, vout))

	utxo, ok := u.utxos[key]
	if !ok {
		return
	}

	delete(u.utxos, key)

	// ✅ remove khỏi TẤT CẢ addresses
	for _, addr := range utxo.Vout.ScriptPubKey.Addresses {
		if set, ok := u.addrIndex[addr]; ok {
			delete(set, key)
			if len(set) == 0 {
				delete(u.addrIndex, addr)
			}
		}
	}
}

func (u *UTXOSet) FindUTXOsByAddress(addr string) []UTXO {
	u.mu.RLock()
	defer u.mu.RUnlock()

	keys, ok := u.addrIndex[addr]
	if !ok {
		return nil
	}

	var res []UTXO
	for key := range keys {
		if utxo, ok := u.utxos[key]; ok {
			res = append(res, utxo)
		}
	}
	return res
}

func (u *UTXOSet) Delete(txid string, vout int) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	key := string(utxoKey(txid, vout))

	utxo, exists := u.utxos[key]
	if !exists {
		return fmt.Errorf("utxo not found: %s", key)
	}

	delete(u.utxos, key)

	// remove from ALL address indexes
	for _, addr := range utxo.Vout.ScriptPubKey.Addresses {
		if set, ok := u.addrIndex[addr]; ok {
			delete(set, key)
			if len(set) == 0 {
				delete(u.addrIndex, addr)
			}
		}
	}

	return nil
}

func (u *UTXOSet) Put(txid string, vout int, voutData VOUT) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	key := string(utxoKey(txid, vout))

	// prevent overwrite
	if _, exists := u.utxos[key]; exists {
		return fmt.Errorf("utxo already exists: %s", key)
	}

	utxo := UTXO{
		Txid:  txid,
		Index: vout,
		Vout:  voutData,
	}

	u.utxos[key] = utxo

	// index by ALL addresses (multisig-safe)
	for _, addr := range voutData.ScriptPubKey.Addresses {
		if _, ok := u.addrIndex[addr]; !ok {
			u.addrIndex[addr] = make(map[string]struct{})
		}
		u.addrIndex[addr][key] = struct{}{}
	}

	return nil
}
