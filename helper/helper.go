package helper

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

func WriteVarInt(buf *bytes.Buffer, n uint64) {
	if n < 0xfd {
		buf.WriteByte(byte(n))
	} else if n <= 0xffff {
		buf.WriteByte(0xfd)
		binary.Write(buf, binary.LittleEndian, uint16(n))
	} else if n <= 0xffffffff {
		buf.WriteByte(0xfe)
		binary.Write(buf, binary.LittleEndian, uint32(n))
	} else {
		buf.WriteByte(0xff)
		binary.Write(buf, binary.LittleEndian, uint64(n))
	}
}

func HexToBytesFixed32(hexStr string) ([]byte, error) {
	raw, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, err
	}
	if len(raw) > 32 {
		return nil, errors.New("txid too long")
	}
	// pad left with zeros (Bitcoin always uses 32 bytes)
	padded := make([]byte, 32)
	copy(padded[32-len(raw):], raw)
	return padded, nil
}

func ReverseBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i := 0; i < len(b); i++ {
		out[i] = b[len(b)-1-i]
	}
	return out
}

func ParseUTXOKey(b []byte) (string, int) {
	parts := strings.Split(string(b), ":")
	// ["utxo", txid, index]
	idx, _ := strconv.Atoi(parts[2])
	return parts[1], idx
}
