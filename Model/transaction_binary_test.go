package model

import (
	"testing"
)

func TestUTXOBinarySerialization(t *testing.T) {
	// Create test UTXO
	original := UTXO{
		Txid:  "abc123def456789012345678901234567890123456789012345678901234abcd",
		Index: 5,
		Vout: VOUT{
			Value: 100000,
			N:     2,
			ScriptPubKey: ScriptPubKey{
				Hex:       "76a914abcdef1234567890abcdef1234567890abcdef1288ac",
				Addresses: []string{"1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"},
			},
		},
	}

	// Serialize
	data := serializeUTXOBinary(original)
	t.Logf("Serialized size: %d bytes", len(data))

	// Deserialize
	decoded, err := deserializeUTXOBinary(data)
	if err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}

	// Verify Txid
	if decoded.Txid != original.Txid {
		t.Errorf("Txid mismatch: got %s, want %s", decoded.Txid, original.Txid)
	}

	// Verify Index
	if decoded.Index != original.Index {
		t.Errorf("Index mismatch: got %d, want %d", decoded.Index, original.Index)
	}

	// Verify Value
	if decoded.Vout.Value != original.Vout.Value {
		t.Errorf("Value mismatch: got %d, want %d", decoded.Vout.Value, original.Vout.Value)
	}

	// Verify N
	if decoded.Vout.N != original.Vout.N {
		t.Errorf("N mismatch: got %d, want %d", decoded.Vout.N, original.Vout.N)
	}

	// Verify ScriptPubKey.Hex
	if decoded.Vout.ScriptPubKey.Hex != original.Vout.ScriptPubKey.Hex {
		t.Errorf("ScriptPubKey.Hex mismatch: got %s, want %s",
			decoded.Vout.ScriptPubKey.Hex, original.Vout.ScriptPubKey.Hex)
	}

	// Verify Addresses
	if len(decoded.Vout.ScriptPubKey.Addresses) != len(original.Vout.ScriptPubKey.Addresses) {
		t.Errorf("Addresses count mismatch: got %d, want %d",
			len(decoded.Vout.ScriptPubKey.Addresses), len(original.Vout.ScriptPubKey.Addresses))
	}

	for i, addr := range original.Vout.ScriptPubKey.Addresses {
		if decoded.Vout.ScriptPubKey.Addresses[i] != addr {
			t.Errorf("Address[%d] mismatch: got %s, want %s", i, decoded.Vout.ScriptPubKey.Addresses[i], addr)
		}
	}

	t.Log("✓ Roundtrip test passed!")
}

func TestUTXOBinaryShortTxid(t *testing.T) {
	// Test with short txid (should pad to 32 bytes)
	original := UTXO{
		Txid:  "abc123",
		Index: 0,
		Vout: VOUT{
			Value: 5000,
			N:     0,
			ScriptPubKey: ScriptPubKey{
				Hex:       "76a914",
				Addresses: []string{"test"},
			},
		},
	}

	data := serializeUTXOBinary(original)
	decoded, err := deserializeUTXOBinary(data)
	if err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}

	// Txid should be padded with zeros
	if len(decoded.Txid) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("Txid length: got %d, want 64", len(decoded.Txid))
	}

	t.Log("✓ Short txid test passed!")
}
