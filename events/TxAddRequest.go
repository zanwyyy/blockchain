package events

type TxAddRequest struct {
	TxID string `json:"txid"`
	Raw  []byte `json:"raw"` // serialized JSON transaction
}
