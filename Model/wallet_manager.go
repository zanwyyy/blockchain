package model

import (
	"fmt"
	"sync"
)

type WalletManager struct {
	mu      sync.Mutex
	Wallets map[string]*Wallet
}

func NewWalletManager() *WalletManager {
	return &WalletManager{
		Wallets: make(map[string]*Wallet),
	}
}

func (wm *WalletManager) GetWallet(
	addr string,
	utxoSet *RedisCache,
) *Wallet {

	wm.mu.Lock()
	defer wm.mu.Unlock()

	// 1) wallet đã tồn tại
	if w, ok := wm.Wallets[addr]; ok {
		return w
	}

	// 2) tạo wallet mới
	w := NewWallet(addr)

	// load UTXO confirmed ban đầu
	w.LoadFromUTXOSet(utxoSet)

	wm.Wallets[addr] = w
	return w
}

func (wm *WalletManager) ApplyUnconfirmedTx(tx Transaction) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	// 1) REMOVE spent inputs from sender wallets
	for _, vin := range tx.Vin {
		for _, w := range wm.Wallets {
			w.mu.Lock()
			key := fmt.Sprintf("%s:%d", vin.Txid, vin.Vout)
			delete(w.utxos, key)
			w.mu.Unlock()
		}
	}

	// 2) ADD outputs to receiver wallets
	for i, vout := range tx.Vout {
		for _, w := range wm.Wallets {
			if IsOutputForAddress(vout, w.Address) {
				w.mu.Lock()
				key := fmt.Sprintf("%s:%d", tx.Txid, i)
				w.utxos[key] = UTXO{
					Txid:  tx.Txid,
					Index: i,
					Vout:  vout,
				}
				w.mu.Unlock()
			}
		}
	}
}
