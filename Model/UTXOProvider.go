package model

type UTXOProvider interface {
	Get(txid string, index int) (UTXO, bool)
	Put(txid string, index int, out VOUT) error
	Delete(txid string, index int) error
	FindUTXOsByAddress(addr string) []UTXO
	UpdateWithTransaction(tx Transaction) error
	Close()
}
