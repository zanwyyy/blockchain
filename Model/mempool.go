package model

import (
	"fmt"
	"sync"
)

type InMemoryMempool struct {
	mu sync.RWMutex

	// txid -> Transaction
	txs map[string]*Transaction

	// spent map: "txid:vout" -> spending txid
	spent map[string]string

	// unconfirmed outputs: "txid:vout" -> VOUT
	outputs map[string]VOUT

	// ordered txids (arrival order)
	order []string

	// txid -> tx size (cache)
	txSize map[string]int

	// total mempool size (bytes)
	totalSize int
}

func NewInMemoryMempool() *InMemoryMempool {
	return &InMemoryMempool{
		txs:     make(map[string]*Transaction),
		spent:   make(map[string]string),
		outputs: make(map[string]VOUT),
		order:   []string{},
		txSize:  make(map[string]int),
	}
}

func (m *InMemoryMempool) GetTransaction(txid string) *Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tx, ok := m.txs[txid]
	if !ok {
		return nil
	}
	return tx
}

func (m *InMemoryMempool) AddTransaction(tx *Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.txs[tx.Txid]; ok {
		return fmt.Errorf("tx already exists")
	}

	size := tx.Size()

	// 1️⃣ save tx
	m.txs[tx.Txid] = tx
	m.txSize[tx.Txid] = size
	m.order = append(m.order, tx.Txid)
	m.totalSize += size

	// 2️⃣ mark inputs as spent
	for _, vin := range tx.Vin {
		if vin.Txid == "" {
			continue
		}
		key := fmt.Sprintf("%s:%d", vin.Txid, vin.Vout)
		m.spent[key] = tx.Txid
	}

	// 3️⃣ store unconfirmed outputs
	for i, out := range tx.Vout {
		key := fmt.Sprintf("%s:%d", tx.Txid, i)
		m.outputs[key] = out
	}

	return nil
}
func (m *InMemoryMempool) IsSpent(txid string, vout int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.spent[fmt.Sprintf("%s:%d", txid, vout)]
	return ok
}
func (m *InMemoryMempool) GetOutput(txid string, vout int) (VOUT, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out, ok := m.outputs[fmt.Sprintf("%s:%d", txid, vout)]
	return out, ok
}
func (m *InMemoryMempool) RemoveTransaction(tx *Transaction) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.txs, tx.Txid)
	m.totalSize -= m.txSize[tx.Txid]
	delete(m.txSize, tx.Txid)

	// remove spent marks
	for _, vin := range tx.Vin {
		if vin.Txid == "" {
			continue
		}
		delete(m.spent, fmt.Sprintf("%s:%d", vin.Txid, vin.Vout))
	}

	// remove outputs
	for i := range tx.Vout {
		delete(m.outputs, fmt.Sprintf("%s:%d", tx.Txid, i))
	}

	// remove from order (lazy rebuild ok)
}

type MempoolSnapshot struct {
	TxIDs []string
	Size  int // tổng size của snapshot
}

func (m *InMemoryMempool) SnapshotUntilSize(maxBytes int) MempoolSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var res []string
	size := 0

	for _, txid := range m.order {
		_, ok := m.txs[txid]
		if !ok {
			continue
		}

		ts := m.txSize[txid]
		if size+ts > maxBytes {
			break
		}

		res = append(res, txid)
		size += ts
	}

	return MempoolSnapshot{
		TxIDs: res,
		Size:  size,
	}
}

func (m *InMemoryMempool) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.txs)
}
