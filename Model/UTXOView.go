package model

import "fmt"

type UTXOView struct {
	// shadow copy: "txid:vout" -> UTXO
	utxos map[string]UTXO
}

func NewUTXOViewFromSet(utxoSet *UTXOSet) *UTXOView {
	utxoSet.mu.RLock()
	defer utxoSet.mu.RUnlock()

	viewMap := make(map[string]UTXO, len(utxoSet.utxos))
	for k, v := range utxoSet.utxos {
		viewMap[k] = v
	}

	return &UTXOView{
		utxos: viewMap,
	}
}

func (v *UTXOView) Get(txid string, vout int) (UTXO, bool) {
	utxo, ok := v.utxos[string(utxoKey(txid, vout))]
	return utxo, ok
}

// Put dùng khi add output mới trong quá trình verify block
func (v *UTXOView) Put(txid string, vout int, voutData VOUT) error {
	key := string(utxoKey(txid, vout))

	if _, exists := v.utxos[key]; exists {
		return fmt.Errorf("utxo already exists in view: %s", key)
	}

	v.utxos[key] = UTXO{
		Txid:  txid,
		Index: vout,
		Vout:  voutData,
	}
	return nil
}

// Delete dùng khi spend input trong block
func (v *UTXOView) Delete(txid string, vout int) error {
	key := string(utxoKey(txid, vout))

	if _, exists := v.utxos[key]; !exists {
		return fmt.Errorf("utxo not found in view: %s", key)
	}

	delete(v.utxos, key)
	return nil
}
