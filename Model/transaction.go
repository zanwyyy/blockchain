// Example: Serialize method for VIN
package model

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"project/helper"
	"project/metrics"
	"time"

	"github.com/minio/sha256-simd"
	"golang.org/x/crypto/ripemd160"
)

func (vin *VIN) Serialize() map[string]interface{} {
	return map[string]interface{}{
		"txid":      vin.Txid,
		"vout":      vin.Vout,
		"scriptSig": vin.ScriptSig.Serialize(),
	}
}

// Example: Serialize method for VOUT
func (vout *VOUT) Serialize() map[string]interface{} {
	return map[string]interface{}{
		"value":        vout.Value,
		"n":            vout.N,
		"scriptPubKey": vout.ScriptPubKey.Serialize(),
	}
}

// Example: Serialize method for ScriptSig
func (sig *ScriptSig) Serialize() map[string]interface{} {
	return map[string]interface{}{
		"asm": sig.ASM,
		"hex": sig.Hex,
	}
}

// Example: Serialize method for ScriptPubKey
func (spk *ScriptPubKey) Serialize() map[string]interface{} {
	return map[string]interface{}{
		"asm":       spk.ASM,
		"hex":       spk.Hex,
		"addresses": spk.Addresses,
	}
}

// HashPubKey = SHA256(pubkey) then RIPEMD160 (like Bitcoin)
func HashPubKey(pubkey []byte) []byte {
	sha := sha256.Sum256(pubkey)
	rip := ripemd160.New()
	_, _ = rip.Write(sha[:])
	return rip.Sum(nil)
}

// computeTxID serializes transaction (json) and returns sha256(txJson) hex

// MakeP2PKHScriptPubKey builds scriptPubKey fields for a given address (pubKeyHashHex)
func MakeP2PKHScriptPubKey(addr string) ScriptPubKey {
	pubKeyHash, _ := hex.DecodeString(addr)

	script := []byte{
		0x76, // OP_DUP
		0xa9, // OP_HASH160
		0x14, // PUSHDATA 20 bytes
	}
	script = append(script, pubKeyHash...)
	script = append(script, 0x88, 0xac) // OP_EQUALVERIFY OP_CHECKSIG

	return ScriptPubKey{
		ASM:       "",
		Hex:       hex.EncodeToString(script),
		Addresses: []string{addr},
	}
}

// SignTransaction: for each input, create signature over a "sighash" computed by
// - making a copy of tx where for the input being signed we set its ScriptSig.Hex = prevOutput.ScriptPubKey.Hex
// - other inputs' ScriptSig.Hex = ""
// - hash that copy and sign with priv
// Then set original tx.Vin[i].ScriptSig.Hex = sigHex and ASM = sigHex + " " + pubkeyHex
func (t *Transaction) SignEd25519(
	priv ed25519.PrivateKey,
	utxoSet *RedisCache,
	mempool *RedisMempool,
) error {
	start := time.Now()
	defer func() {
		metrics.FnDuration.
			WithLabelValues("tx_sign_duration").
			Observe(float64(time.Since(start).Milliseconds()))
	}()

	if len(t.Vin) == 0 {
		return errors.New("no inputs to sign")
	}

	pub := priv.Public().(ed25519.PublicKey)

	for inIdx := range t.Vin {
		vin := &t.Vin[inIdx]

		// -----------------------------
		// 1) Find referenced output (UTXO or mempool output)
		// -----------------------------
		var prevOut VOUT
		var ok bool

		// (a) canonical UTXO
		utxo, found := utxoSet.Get(vin.Txid, vin.Vout)
		if found {
			prevOut = utxo.Vout
			ok = true
		} else {
			// (b) mempool output (chained tx)
			prevOut, ok = mempool.GetOutput(vin.Txid, vin.Vout)
			if !ok {
				return fmt.Errorf(
					"cannot sign: missing input %s[%d]",
					vin.Txid,
					vin.Vout,
				)
			}
		}

		// -----------------------------
		// 2) Create txCopy with empty scripts
		// -----------------------------
		txCopy := t.ShallowCopyEmptySigs()

		// -----------------------------
		// 3) Inject ScriptPubKey for THIS input
		// -----------------------------
		txCopy.Vin[inIdx].ScriptSig.Hex = prevOut.ScriptPubKey.Hex

		// -----------------------------
		// 4) Serialize + double SHA256
		// -----------------------------
		raw := txCopy.Serialize()
		h1 := sha256.Sum256(raw)
		h2 := sha256.Sum256(h1[:])
		sighash := h2[:]

		// -----------------------------
		// 5) Sign with Ed25519
		// -----------------------------
		sig := ed25519.Sign(priv, sighash) // 64 bytes

		// -----------------------------
		// 6) Build scriptSig = sig || pubkey
		// -----------------------------
		script := append(sig, pub...) // 96 bytes

		vin.ScriptSig.Hex = hex.EncodeToString(script)
		vin.ScriptSig.ASM = fmt.Sprintf("%x %x", sig, pub)
	}

	// -----------------------------
	// 7) Update txid AFTER signing
	// -----------------------------
	t.Txid = t.ComputeTxID()
	return nil
}

// VerifyTransaction: for each input, extract signature and pubkey from ScriptSig.ASM,
// verify pubKeyHash matches prev output addresses[0], then compute sighash same as signing and verify signature.
func VerifyForMempool(
	t *Transaction,
	utxoSet *RedisCache,
	mempool *RedisMempool,
) bool {
	start := time.Now()
	defer func() {
		metrics.FnDuration.
			WithLabelValues("tx_verify_duration").
			Observe(float64(time.Since(start).Milliseconds()))
	}()

	// -----------------------------
	// 0) Basic sanity checks
	// -----------------------------
	if len(t.Vin) == 0 || len(t.Vout) == 0 {
		return false
	}

	// No duplicate inputs inside tx
	seen := make(map[string]bool)
	for _, vin := range t.Vin {
		key := fmt.Sprintf("%s_%d", vin.Txid, vin.Vout)
		if seen[key] {
			return false
		}
		seen[key] = true
	}

	inputSum := int64(0)

	// -----------------------------
	// 1) Verify each input
	// -----------------------------
	for inIdx, vin := range t.Vin {

		// Coinbase is NOT allowed in mempool
		if vin.Txid == "" {
			return false
		}

		// 1.1 Double-spend check (mempool)
		if mempool.IsSpent(vin.Txid, vin.Vout) {
			return false
		}

		// 1.2 Fetch referenced output
		var prevOut VOUT
		var ok bool

		// (a) canonical UTXO
		utxo, found := utxoSet.Get(vin.Txid, vin.Vout)
		if found {
			prevOut = utxo.Vout
			ok = true
		} else {
			// (b) mempool output (chained tx)
			prevOut, ok = mempool.GetOutput(vin.Txid, vin.Vout)
			if !ok {
				return false
			}
		}

		// -----------------------------
		// 2) SCRIPT & SIGNATURE VERIFY
		// -----------------------------

		// scriptSig = sig(64) || pubkey(32)
		scriptBytes, err := hex.DecodeString(vin.ScriptSig.Hex)
		if err != nil || len(scriptBytes) != 96 {
			return false
		}

		sigBytes := scriptBytes[:64]
		pubBytes := scriptBytes[64:96]

		// Compare pubKeyHash with ScriptPubKey
		pubKeyHashCalc := HashPubKey(pubBytes)

		spk, err := hex.DecodeString(prevOut.ScriptPubKey.Hex)
		if err != nil || len(spk) < 25 {
			return false
		}

		expectedHash := spk[3 : 3+20]
		if !bytes.Equal(pubKeyHashCalc, expectedHash) {
			return false
		}

		// -----------------------------
		// 3) Compute sighash
		// -----------------------------
		txCopy := t.ShallowCopyEmptySigs()
		txCopy.Vin[inIdx].ScriptSig.Hex = hex.EncodeToString(spk)

		raw := txCopy.Serialize()
		h1 := sha256.Sum256(raw)
		h2 := sha256.Sum256(h1[:])
		sighash := h2[:]

		// -----------------------------
		// 4) Verify signature
		// -----------------------------
		if !ed25519.Verify(
			ed25519.PublicKey(pubBytes),
			sighash,
			sigBytes,
		) {
			return false
		}

		inputSum += prevOut.Value
	}

	// -----------------------------
	// 5) Verify outputs
	// -----------------------------
	outputSum := int64(0)
	for _, out := range t.Vout {
		if out.Value <= 0 {
			return false
		}
		outputSum += out.Value
	}

	return inputSum >= outputSum
}

// UpdateUTXOSet: cập nhật UTXO set sau khi verify thành công
func (t *Transaction) UpdateUTXOSet(utxo map[string]map[int]VOUT) {
	// Remove spent outputs
	for _, vin := range t.Vin {
		if utxo[vin.Txid] != nil {
			delete(utxo[vin.Txid], vin.Vout)
			if len(utxo[vin.Txid]) == 0 {
				delete(utxo, vin.Txid)
			}
		}
	}
	// Add new outputs
	utxo[t.Txid] = make(map[int]VOUT)
	for idx, vout := range t.Vout {
		utxo[t.Txid][idx] = vout
	}
}

// ShallowCopyTxEmptySigs makes a shallow copy of tx but clears all ScriptSig.Hex and ASM
func (t *Transaction) ShallowCopyEmptySigs() Transaction {
	newVin := make([]VIN, len(t.Vin))
	for i := range t.Vin {
		newVin[i] = VIN{
			Txid: t.Vin[i].Txid,
			Vout: t.Vin[i].Vout,
			ScriptSig: ScriptSig{
				ASM: "",
				Hex: "",
			},
		}
	}
	newVout := make([]VOUT, len(t.Vout))
	copy(newVout, t.Vout)
	txCopy := Transaction{
		Txid: "",
		Vin:  newVin,
		Vout: newVout,
	}
	return txCopy
}

func CreateTransaction(
	priv ed25519.PrivateKey,
	fromAddr string,
	toAddr string,
	amount int64,
	utxoSet *RedisCache,
	mempool *RedisMempool,
	wallet *Wallet,

) (Transaction, error) {

	type inputCandidate struct {
		Txid  string
		Index int
		Out   VOUT
	}

	var candidates []inputCandidate

	// 1) get spendable UTXOs from wallet
	utxos := wallet.GetSpendableUTXOs(mempool)
	for _, u := range utxos {
		candidates = append(candidates, inputCandidate{
			Txid:  u.Txid,
			Index: u.Index,
			Out:   u.Vout,
		})
	}

	if len(candidates) == 0 {
		return Transaction{}, errors.New("no spendable outputs")
	}

	// 2) select inputs
	var selected []inputCandidate
	var total int64

	for _, c := range candidates {
		selected = append(selected, c)
		total += c.Out.Value
		if total >= amount {
			break
		}
	}

	if total < amount {
		return Transaction{}, errors.New("insufficient funds")
	}

	// 3) build vins
	vins := make([]VIN, len(selected))
	for i, in := range selected {
		vins[i] = VIN{
			Txid: in.Txid,
			Vout: in.Index,
			ScriptSig: ScriptSig{
				ASM: "",
				Hex: "",
			},
		}
	}

	// 4) build vouts
	vouts := []VOUT{
		{
			Value:        amount,
			N:            0,
			ScriptPubKey: MakeP2PKHScriptPubKey(toAddr),
		},
	}

	if total > amount {
		vouts = append(vouts, VOUT{
			Value:        total - amount,
			N:            1,
			ScriptPubKey: MakeP2PKHScriptPubKey(fromAddr),
		})
	}

	tx := Transaction{
		Version: 1,
		Vin:     vins,
		Vout:    vouts,
	}

	// 5) sign
	if err := tx.SignEd25519(priv, utxoSet, mempool); err != nil {
		return Transaction{}, err
	}

	return tx, nil
}

func (t *Transaction) Size() int {
	return len(t.Serialize())
}

func (tx *Transaction) Serialize() []byte {
	buf := new(bytes.Buffer)

	// 1) version (4 bytes LE)
	binary.Write(buf, binary.LittleEndian, tx.Version)

	// 2) inputs (varint count)
	helper.WriteVarInt(buf, uint64(len(tx.Vin)))

	for _, vin := range tx.Vin {
		// prev_txid (32 bytes LE)
		prevBytes, _ := helper.HexToBytesFixed32(vin.Txid)
		buf.Write(helper.ReverseBytes(prevBytes))

		// prev_vout index (4 bytes LE)
		binary.Write(buf, binary.LittleEndian, uint32(vin.Vout))

		// scriptSig bytes
		script, _ := hex.DecodeString(vin.ScriptSig.Hex)
		helper.WriteVarInt(buf, uint64(len(script)))
		buf.Write(script)

		// sequence (4 bytes), constant
		binary.Write(buf, binary.LittleEndian, uint32(0xffffffff))
	}

	// 3) outputs (varint count)
	helper.WriteVarInt(buf, uint64(len(tx.Vout)))

	for _, vout := range tx.Vout {
		// value (8 bytes LE)
		binary.Write(buf, binary.LittleEndian, uint64(vout.Value))

		// scriptPubKey = raw script bytes
		scriptBytes, _ := hex.DecodeString(vout.ScriptPubKey.Hex)
		helper.WriteVarInt(buf, uint64(len(scriptBytes)))
		buf.Write(scriptBytes)
	}

	// 4) locktime (4 bytes)
	binary.Write(buf, binary.LittleEndian, tx.LockTime)

	return buf.Bytes()
}

func (tx *Transaction) ComputeTxID() string {
	raw := tx.Serialize()
	first := sha256.Sum256(raw)
	second := sha256.Sum256(first[:])
	return hex.EncodeToString(helper.ReverseBytes(second[:])) // Bitcoin hiển thị little-endian
}

type Transaction struct {
	Version  uint32
	Vin      []VIN
	Vout     []VOUT
	LockTime uint32

	Txid string `json:"txid"`
}

type VIN struct {
	Txid      string    `json:"txid"`      // mã giao dịch trước
	Vout      int       `json:"vout"`      // index output của giao dịch trước
	ScriptSig ScriptSig `json:"scriptSig"` // dữ liệu để mở khóa
}

type ScriptSig struct {
	ASM string `json:"asm"` // normally: "<signature_hex> <pubkey_hex>"
	Hex string `json:"hex"` // signature hex (we store signature hex here)
}

type VOUT struct {
	Value        int64        `json:"value"` // amount
	N            int          `json:"n"`     // index của output
	ScriptPubKey ScriptPubKey `json:"scriptPubKey"`
}

type ScriptPubKey struct {
	ASM       string   `json:"asm"`       // human-readable script, e.g. "OP_DUP OP_HASH160 <pubKeyHash> OP_EQUALVERIFY OP_CHECKSIG"
	Hex       string   `json:"hex"`       // script raw hex (for simplicity: hex of pubKeyHash or a template)
	Addresses []string `json:"addresses"` // addresses (we store pubKeyHash hex)
}

func BuildP2PKHScriptPubKey(pubKeyHash []byte) []byte {
	script := []byte{
		0x76, // OP_DUP
		0xa9, // OP_HASH160
		0x14, // push 20 bytes
	}
	script = append(script, pubKeyHash...)
	script = append(script, 0x88, 0xac) // OP_EQUALVERIFY, OP_CHECKSIG
	return script
}

func BuildP2PKHScriptSig(sig []byte, pub []byte) []byte {
	script := []byte{}
	script = append(script, byte(len(sig))) // push sig
	script = append(script, sig...)
	script = append(script, byte(len(pub))) // push pubkey (33)
	script = append(script, pub...)
	return script
}

type UTXOView struct {
	utxos map[string]UTXO // key = "txid:vout"
}

func NewUTXOViewFromSet(utxoSet *RedisCache) (*UTXOView, error) {
	view := &UTXOView{
		utxos: make(map[string]UTXO),
	}

	all, err := utxoSet.GetAll() // bạn có thể tự impl
	if err != nil {
		return nil, err
	}

	for _, u := range all {
		key := string(utxoKey(u.Txid, u.Index))
		view.utxos[key] = u
	}

	return view, nil
}

func VerifyTxWithView(
	t *Transaction,
	view *UTXOView,
) error {

	// -----------------------------
	// 0) Basic sanity checks
	// -----------------------------
	if len(t.Vin) == 0 || len(t.Vout) == 0 {
		return fmt.Errorf("empty vin or vout")
	}

	seen := make(map[string]bool)
	for _, vin := range t.Vin {
		key := fmt.Sprintf("%s_%d", vin.Txid, vin.Vout)
		if seen[key] {
			return fmt.Errorf("duplicate input")
		}
		seen[key] = true
	}

	inputSum := int64(0)

	// -----------------------------
	// 1) Verify each input
	// -----------------------------
	for inIdx, vin := range t.Vin {

		// Coinbase NOT allowed here
		if vin.Txid == "" {
			return fmt.Errorf("coinbase not allowed")
		}

		// fetch prevOut ONLY from view
		key := viewKey(vin.Txid, vin.Vout)
		utxo, ok := view.utxos[key]
		if !ok {
			return fmt.Errorf("missing utxo %s", key)
		}
		prevOut := utxo.Vout

		// -----------------------------
		// 2) SCRIPT & SIGNATURE VERIFY
		// -----------------------------
		scriptBytes, err := hex.DecodeString(vin.ScriptSig.Hex)
		if err != nil || len(scriptBytes) != 96 {
			return fmt.Errorf("invalid scriptsig")
		}

		sigBytes := scriptBytes[:64]
		pubBytes := scriptBytes[64:96]

		pubKeyHashCalc := HashPubKey(pubBytes)

		spk, err := hex.DecodeString(prevOut.ScriptPubKey.Hex)
		if err != nil || len(spk) < 25 {
			return fmt.Errorf("invalid scriptpubkey")
		}

		expectedHash := spk[3 : 3+20]
		if !bytes.Equal(pubKeyHashCalc, expectedHash) {
			return fmt.Errorf("pubkey hash mismatch")
		}

		// -----------------------------
		// 3) Compute sighash
		// -----------------------------
		txCopy := t.ShallowCopyEmptySigs()
		txCopy.Vin[inIdx].ScriptSig.Hex = hex.EncodeToString(spk)

		raw := txCopy.Serialize()
		h1 := sha256.Sum256(raw)
		h2 := sha256.Sum256(h1[:])
		sighash := h2[:]

		// -----------------------------
		// 4) Verify signature
		// -----------------------------
		if !ed25519.Verify(
			ed25519.PublicKey(pubBytes),
			sighash,
			sigBytes,
		) {
			return fmt.Errorf("invalid signature")
		}

		inputSum += prevOut.Value
	}

	// -----------------------------
	// 5) Verify outputs
	// -----------------------------
	outputSum := int64(0)
	for _, out := range t.Vout {
		if out.Value <= 0 {
			return fmt.Errorf("invalid output value")
		}
		outputSum += out.Value
	}

	if inputSum < outputSum {
		return fmt.Errorf("input < output")
	}

	return nil
}

func ApplyTxToView(tx *Transaction, view *UTXOView) {

	// remove spent inputs
	for _, vin := range tx.Vin {
		delete(view.utxos, string(utxoKey(vin.Txid, vin.Vout)))
	}

	// add new outputs
	for i, out := range tx.Vout {
		view.utxos[string(utxoKey(tx.Txid, i))] = UTXO{
			Txid:  tx.Txid,
			Index: i,
			Vout:  out,
		}
	}
}

func VerifyBlock(block *Block, utxoSet *RedisCache) error {

	// 1️⃣ init view từ UTXO set
	view, err := NewUTXOViewFromSet(utxoSet)
	if err != nil {
		return err
	}

	// 2️⃣ verify từng tx theo thứ tự trong block
	for i := range block.Transactions {
		tx := &block.Transactions[i]

		if err := VerifyTxWithView(tx, view); err != nil {
			return fmt.Errorf("tx %s invalid: %v, with index %d", tx.Txid, err, i)
		}

		// 3️⃣ apply tx lên view
		ApplyTxToView(tx, view)
	}

	return nil
}
func CommitBlock(block *Block, utxoSet *RedisCache) error {

	for _, tx := range block.Transactions {

		// remove spent
		for _, vin := range tx.Vin {
			if err := utxoSet.Delete(vin.Txid, vin.Vout); err != nil {
				return err
			}
		}

		// add new outputs
		for i, out := range tx.Vout {
			if err := utxoSet.Put(tx.Txid, i, out); err != nil {
				return err
			}
		}
	}

	return nil
}

func viewKey(txid string, vout int) string {
	return fmt.Sprintf("%s:%d", txid, vout)
}
