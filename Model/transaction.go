// Example: Serialize method for VIN
package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"project/helper"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
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

// NewKeyPair returns secp256k1 priv/pub
func NewKeyPair() (*btcec.PrivateKey, *btcec.PublicKey) {
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		log.Panic(err)
	}
	return priv, priv.PubKey()
}

// AddressFromPub returns the pubKeyHash hex as "address" (simplified)
func AddressFromPub(pub *btcec.PublicKey) string {
	h := HashPubKey(pub.SerializeCompressed())
	return hex.EncodeToString(h)
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
func (t *Transaction) Sign(priv *btcec.PrivateKey, utxoSet *RedisCache) error {
	if len(t.Vin) == 0 {
		return errors.New("no inputs to sign")
	}

	for inIdx := range t.Vin {
		vin := &t.Vin[inIdx]

		// 1. Find referenced UTXO
		utxo, ok := utxoSet.Get(vin.Txid, vin.Vout)
		if !ok {
			return fmt.Errorf("cannot sign: missing UTXO %s[%d]", vin.Txid, vin.Vout)
		}

		// 2. Create txCopy with empty scriptSig
		txCopy := t.ShallowCopyEmptySigs()

		// 3. Replace THIS input's scriptSig with ScriptPubKey (raw bytes)
		txCopy.Vin[inIdx].ScriptSig.Hex = utxo.Vout.ScriptPubKey.Hex

		// 4. Serialize txCopy
		raw := txCopy.Serialize()

		// 5. Double SHA256
		h1 := sha256.Sum256(raw)
		h2 := sha256.Sum256(h1[:])
		sighash := h2[:]

		// 6. Sign with ECDSA
		sig := ecdsa.Sign(priv, sighash)
		sigDER := sig.Serialize()

		// 7. Compressed pubkey
		pubBytes := priv.PubKey().SerializeCompressed()

		// 8. Build scriptSig = [sigLen][sigDER][pubLen][pubBytes]
		buf := new(bytes.Buffer)
		buf.WriteByte(byte(len(sigDER)))
		buf.Write(sigDER)
		buf.WriteByte(byte(len(pubBytes)))
		buf.Write(pubBytes)

		// store into input
		vin.ScriptSig.Hex = hex.EncodeToString(buf.Bytes())
		vin.ScriptSig.ASM = fmt.Sprintf("%x %x", sigDER, pubBytes)
	}

	// update txid
	t.Txid = t.ComputeTxID()
	return nil
}

// VerifyTransaction: for each input, extract signature and pubkey from ScriptSig.ASM,
// verify pubKeyHash matches prev output addresses[0], then compute sighash same as signing and verify signature.

func VerifyUTXO(t *Transaction, utxoSet *RedisCache) bool {

	if len(t.Vin) == 0 || len(t.Vout) == 0 {
		return false
	}

	// No duplicate inputs
	seen := make(map[string]bool)
	for _, vin := range t.Vin {
		key := fmt.Sprintf("%s_%d", vin.Txid, vin.Vout)
		if seen[key] {
			return false
		}
		seen[key] = true
	}

	inputSum := int64(0)

	for inIdx, vin := range t.Vin {

		// UTXO must exist
		utxo, ok := utxoSet.Get(vin.Txid, vin.Vout)
		if !ok {
			log.Println("input UTXO not found")
			return false
		}

		// -----------------------------
		// 1) PARSE SCRIPTSIG (raw bytes)
		// -----------------------------
		scriptBytes, err := hex.DecodeString(vin.ScriptSig.Hex)
		if err != nil {
			log.Println("invalid scriptSig hex")
			return false
		}

		if len(scriptBytes) < 2 {
			log.Println("scriptSig too short")
			return false
		}

		// script format:
		// [sigLen][sigBytes...][pubLen][pubBytes...]

		sigLen := int(scriptBytes[0])
		if len(scriptBytes) < 1+sigLen+1 {
			log.Println("scriptSig malformed (sig)")
			return false
		}

		sigBytes := scriptBytes[1 : 1+sigLen]

		pubLenIndex := 1 + sigLen
		pubLen := int(scriptBytes[pubLenIndex])

		if len(scriptBytes) < pubLenIndex+1+pubLen {
			log.Println("scriptSig malformed (pub)")
			return false
		}

		pubBytes := scriptBytes[pubLenIndex+1 : pubLenIndex+1+pubLen]

		// -----------------------------
		// 2) Compare pubkeyHash to ScriptPubKey
		// -----------------------------
		pubKeyHashCalc := HashPubKey(pubBytes)

		// ScriptPubKey.Hex = raw P2PKH bytes:
		// [76 a9 14] [20-byte hash] [88 ac]
		spk, err := hex.DecodeString(utxo.Vout.ScriptPubKey.Hex)
		if err != nil || len(spk) < 25 {
			log.Println("invalid scriptPubKey")
			return false
		}

		// get embedded pubkeyhash from scriptPubKey
		expectedHash := spk[3 : 3+20] // 20 bytes after 0xa9 0x14

		if !bytes.Equal(pubKeyHashCalc, expectedHash) {
			log.Println("pubkey hash mismatch")
			return false
		}

		// -----------------------------
		// 3) Compute SIGHASH
		// -----------------------------
		// txCopy with scriptSig replaced by ScriptPubKey for THIS input
		txCopy := t.ShallowCopyEmptySigs()

		// Set scriptSig = ScriptPubKey.raw (P2PKH)
		scriptPubKeyRaw := spk // already decoded
		txCopy.Vin[inIdx].ScriptSig.Hex = hex.EncodeToString(scriptPubKeyRaw)

		// Serialize txCopy
		raw := txCopy.Serialize()

		h1 := sha256.Sum256(raw)
		h2 := sha256.Sum256(h1[:])
		sighash := h2[:]

		// -----------------------------
		// 4) Verify ECDSA signature
		// -----------------------------
		sigParsed, err := ecdsa.ParseDERSignature(sigBytes)
		if err != nil {
			log.Println("parse DER failed")
			return false
		}

		pubKey, err := btcec.ParsePubKey(pubBytes)
		if err != nil {
			log.Println("parse pubkey failed")
			return false
		}

		if !sigParsed.Verify(sighash, pubKey) {
			log.Println("signature invalid")
			return false
		}

		// sum input value
		inputSum += utxo.Vout.Value
	}

	// -----------------------------
	// 5) Check outputs
	// -----------------------------
	outputSum := int64(0)
	for _, out := range t.Vout {
		if out.Value <= 0 {
			return false
		}
		outputSum += out.Value
	}

	if inputSum < outputSum {
		log.Println("output > input")
		return false
	}

	return true
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
	priv *btcec.PrivateKey,
	fromAddr string,
	toAddr string,
	amount int64,
	utxoSet *RedisCache,
) (Transaction, error) {

	// --- STEP 1: Chọn UTXOs ---
	utxos := utxoSet.FindUTXOsByAddress(fromAddr)
	if len(utxos) == 0 {
		return Transaction{}, errors.New("no UTXOs available for this address")
	}

	var selected []UTXO
	var total int64 = 0

	for _, u := range utxos {
		selected = append(selected, u)
		total += u.Vout.Value
		if total >= amount {
			break
		}
	}

	if total < amount {
		return Transaction{}, errors.New("insufficient funds")
	}

	// --- STEP 2: Tạo VIN từ selected UTXOs ---
	vins := make([]VIN, len(selected))
	for i, u := range selected {
		vins[i] = VIN{
			Txid: u.Txid,
			Vout: u.Index,
			ScriptSig: ScriptSig{
				ASM: "",
				Hex: "",
			},
		}
	}

	// --- STEP 3: Tạo VOUTs ---
	vouts := []VOUT{
		{
			Value:        amount,
			N:            0,
			ScriptPubKey: MakeP2PKHScriptPubKey(toAddr),
		},
	}

	// Change nếu dư
	if total > amount {
		changeVal := total - amount

		vouts = append(vouts, VOUT{
			Value:        changeVal,
			N:            1,
			ScriptPubKey: MakeP2PKHScriptPubKey(fromAddr),
		})
	}

	tx := Transaction{
		Version:  1,
		Vin:      vins,
		Vout:     vouts,
		LockTime: 0,
	}

	// --- STEP 4: Ký transaction ---
	signErr := tx.Sign(priv, utxoSet)
	if signErr != nil {
		return Transaction{}, signErr
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
