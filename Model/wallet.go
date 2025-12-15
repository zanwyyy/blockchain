package model

import (
	"fmt"
	"sync"
)

type Wallet struct {
	Address string

	// local UTXO view (confirmed + unconfirmed)
	utxos map[string]UTXO // key = txid:vout
	mu    sync.Mutex
}

func NewWallet(addr string) *Wallet {
	return &Wallet{
		Address: addr,
		utxos:   make(map[string]UTXO),
	}
}

func (w *Wallet) GetSpendableUTXOs(
	mempool *RedisMempool,
) []UTXO {

	w.mu.Lock()
	defer w.mu.Unlock()

	var res []UTXO
	for _, u := range w.utxos {
		if mempool.IsSpent(u.Txid, u.Index) {
			continue
		}
		res = append(res, u)
	}
	return res
}

func (w *Wallet) LoadFromUTXOSet(utxoSet *RedisCache) {
	w.mu.Lock()
	defer w.mu.Unlock()

	outs := utxoSet.FindUTXOsByAddress(w.Address)
	for _, u := range outs {
		key := fmt.Sprintf("%s:%d", u.Txid, u.Index)
		w.utxos[key] = u
	}
}

func (w *Wallet) ApplyUnconfirmedTx(tx Transaction) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// remove spent inputs
	for _, vin := range tx.Vin {
		key := fmt.Sprintf("%s:%d", vin.Txid, vin.Vout)
		delete(w.utxos, key)
	}

	// add new outputs (change)
	for i, vout := range tx.Vout {
		if IsOutputForAddress(vout, w.Address) {
			key := fmt.Sprintf("%s:%d", tx.Txid, i)
			w.utxos[key] = UTXO{
				Txid:  tx.Txid,
				Index: i,
				Vout:  vout,
			}
		}
	}
}

func IsOutputForAddress(out VOUT, addr string) bool {
	expected := MakeP2PKHScriptPubKey(addr)
	return out.ScriptPubKey.Hex == expected.Hex
}
