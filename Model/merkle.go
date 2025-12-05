package model

import (
	"crypto/sha256"
)

type TxMerkleItem struct {
	Txid []byte
}

func (m TxMerkleItem) CalculateHash() ([]byte, error) {
	h := sha256.Sum256(m.Txid)
	return h[:], nil
}

func (m TxMerkleItem) Equals(other interface{}) (bool, error) {
	o, ok := other.(TxMerkleItem)
	if !ok {
		return false, nil
	}
	return string(m.Txid) == string(o.Txid), nil
}

func ComputeMerkleRoot(txs []Transaction) []byte {
	if len(txs) == 0 {
		return []byte{}
	}

	var items []TxMerkleItem
	for _, tx := range txs {
		items = append(items, TxMerkleItem{Txid: []byte(tx.Txid)})
	}

	// For demo: just hash the first item
	if len(items) > 0 {
		h, _ := items[0].CalculateHash()
		return h
	}
	return []byte{}
}
