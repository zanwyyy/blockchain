package events

type TxCreateRequest struct {
	PrivateKeyHex string `json:"private_key_hex"`
	FromAddr      string `json:"from_addr"`
	ToAddr        string `json:"to_addr"`
	Amount        int64  `json:"amount"`
}
