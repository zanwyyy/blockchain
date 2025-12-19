package model

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"project/helper"
	"strings"

	badger "github.com/dgraph-io/badger/v4"
)

type UTXO struct {
	Txid  string
	Index int
	Vout  VOUT
}

type BadgerUTXOSet struct {
	db *badger.DB
}

func NewBadgerUTXOSet(path string) (*BadgerUTXOSet, error) {
	opts := badger.DefaultOptions(path).WithBypassLockGuard(true)
	opts.Logger = nil // tắt log
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	return &BadgerUTXOSet{db: db}, nil
}

func utxoKey(txid string, index int) []byte {
	return []byte(fmt.Sprintf("%s:%d", txid, index))
}

func serializeVOUT(out VOUT) ([]byte, error) {
	buf := new(bytes.Buffer)

	// 1) Value int64
	binary.Write(buf, binary.LittleEndian, out.Value)

	// 2) ScriptPubKey raw bytes (hex → raw)
	scriptBytes, err := hexToBytes(out.ScriptPubKey.Hex)
	if err != nil {
		return nil, err
	}

	// write script length
	binary.Write(buf, binary.LittleEndian, uint32(len(scriptBytes)))
	// write script
	buf.Write(scriptBytes)

	// 3) Address count
	binary.Write(buf, binary.LittleEndian, uint32(len(out.ScriptPubKey.Addresses)))

	// 4) Write each address as length + bytes
	for _, addr := range out.ScriptPubKey.Addresses {
		binary.Write(buf, binary.LittleEndian, uint32(len(addr)))
		buf.Write([]byte(addr))
	}

	return buf.Bytes(), nil
}

func deserializeVOUT(data []byte) (VOUT, error) {
	var v VOUT
	buf := bytes.NewReader(data)

	// 1) Value
	binary.Read(buf, binary.LittleEndian, &v.Value)

	// 2) ScriptPubKey bytes
	var scriptLen uint32
	binary.Read(buf, binary.LittleEndian, &scriptLen)

	scriptRaw := make([]byte, scriptLen)
	buf.Read(scriptRaw)

	// store hex form
	spk := ScriptPubKey{
		Hex:       bytesToHex(scriptRaw),
		Addresses: []string{},
	}

	// 3) Address count
	var addrCount uint32
	binary.Read(buf, binary.LittleEndian, &addrCount)

	// 4) Read addresses
	for i := 0; i < int(addrCount); i++ {
		var n uint32
		binary.Read(buf, binary.LittleEndian, &n)

		addr := make([]byte, n)
		buf.Read(addr)

		spk.Addresses = append(spk.Addresses, string(addr))
	}

	v.ScriptPubKey = spk
	return v, nil
}

func (u *BadgerUTXOSet) Put(txid string, index int, out VOUT) error {
	key := utxoKey(txid, index)

	val, err := serializeVOUT(out)
	if err != nil {
		return err
	}

	return u.db.Update(func(txn *badger.Txn) error {
		if err := txn.Set(key, val); err != nil {
			return err
		}

		// index theo địa chỉ
		if len(out.ScriptPubKey.Addresses) > 0 {
			addr := out.ScriptPubKey.Addresses[0]
			akey := addrKey(addr, txid, index)
			if err := txn.Set(akey, []byte{}); err != nil {
				return err
			}
		}

		return nil
	})
}

func (u *BadgerUTXOSet) Get(txid string, index int) (UTXO, bool) {
	key := utxoKey(txid, index)

	var out VOUT

	err := u.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			v, err := deserializeVOUT(val)
			if err != nil {
				return err
			}
			out = v
			return nil
		})
	})

	if err != nil {
		if err == badger.ErrKeyNotFound {
			return UTXO{}, false
		}
		return UTXO{}, false
	}

	return UTXO{
		Txid:  txid,
		Index: index,
		Vout:  out,
	}, true
}

func (u *BadgerUTXOSet) Delete(txid string, index int) error {
	key := utxoKey(txid, index)

	return u.db.Update(func(txn *badger.Txn) error {
		// read current value first to know address (if any)
		var out VOUT
		item, err := txn.Get(key)
		if err == nil {
			_ = item.Value(func(val []byte) error {
				v, e := deserializeVOUT(val)
				if e == nil {
					out = v
				}
				return nil
			})
		} else if err != badger.ErrKeyNotFound {
			// unexpected error
			return err
		}

		// delete UTXO key (ignore ErrKeyNotFound)
		_ = txn.Delete(key)

		// delete addr index if we had address info
		if len(out.ScriptPubKey.Addresses) > 0 {
			akey := addrKey(out.ScriptPubKey.Addresses[0], txid, index)
			_ = txn.Delete(akey)
		}

		return nil
	})
}

func (u *BadgerUTXOSet) UpdateWithTransaction(tx Transaction) error {
	// Build lists of deletes and puts
	type dentry struct {
		txid string
		idx  int
	}
	type pentry struct {
		txid string
		idx  int
		out  VOUT
	}

	var dels []dentry
	var puts []pentry

	for _, vin := range tx.Vin {
		if vin.Txid != "" {
			dels = append(dels, dentry{vin.Txid, vin.Vout})
		}
	}
	for _, out := range tx.Vout {
		puts = append(puts, pentry{tx.Txid, out.N, out})
	}

	// Perform all DB writes in a single transaction (atomic for this tx)
	err := u.db.Update(func(txn *badger.Txn) error {
		// deletes
		for _, d := range dels {
			k := utxoKey(d.txid, d.idx)
			// try to read to know address for index deletion
			item, err := txn.Get(k)
			if err == nil {
				_ = item.Value(func(val []byte) error {
					v, e := deserializeVOUT(val)
					if e == nil && len(v.ScriptPubKey.Addresses) > 0 {
						akey := addrKey(v.ScriptPubKey.Addresses[0], d.txid, d.idx)
						_ = txn.Delete(akey)
					}
					return nil
				})
			}
			// delete UTXO key (ignore not found)
			_ = txn.Delete(k)
		}

		// puts
		for _, p := range puts {
			k := utxoKey(p.txid, p.idx)
			val, serr := serializeVOUT(p.out)
			if serr != nil {
				return serr
			}
			if err := txn.Set(k, val); err != nil {
				return err
			}
			if len(p.out.ScriptPubKey.Addresses) > 0 {
				akey := addrKey(p.out.ScriptPubKey.Addresses[0], p.txid, p.idx)
				if err := txn.Set(akey, []byte{}); err != nil {
					return err
				}
			}
		}

		return nil
	})

	return err
}

// Find UTXO by address (slow scan - OK for toy blockchain)
func (u *BadgerUTXOSet) FindByAddress(addr string) []UTXO {
	prefix := []byte("addr:" + addr + ":")

	result := []UTXO{}

	_ = u.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // IMPORTANT: don't read VOUT here
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := string(it.Item().Key())
			// key format: addr:<addr>:<txid>:<index>
			parts := strings.Split(key, ":")
			txid := parts[2]

			var voutIndex int
			fmt.Sscanf(parts[3], "%d", &voutIndex)

			// now fetch real UTXO
			utxo, ok := u.Get(txid, voutIndex)
			if ok {
				result = append(result, utxo)
			}
		}

		return nil
	})

	return result
}

func (u *BadgerUTXOSet) Close() {
	u.db.Close()
}

// Helper
func hexToBytes(h string) ([]byte, error) {
	return hex.DecodeString(h)
}

func bytesToHex(b []byte) string {
	return hex.EncodeToString(b)
}
func addrKey(address, txid string, index int) []byte {
	return []byte(fmt.Sprintf("addr:%s:%s:%d", address, txid, index))
}

// Global UTXO set instance (singleton)
var globalUTXOSet *CachedUTXOSet

// InitUTXOSet - Initialize UTXO set singleton once at startup
// Only first caller opens DB, others reuse cached singleton
func InitUTXOSet(dbPath string) *CachedUTXOSet {
	if globalUTXOSet != nil {
		return globalUTXOSet
	}

	// Try open DB
	db, err := NewBadgerUTXOSet(dbPath)
	if err != nil {
		fmt.Printf("[UTXO] Cannot open DB: %v → using empty cache\n", err)
		globalUTXOSet = &CachedUTXOSet{
			db:        nil,
			Cache:     make(map[string]UTXO),
			addrIndex: make(map[string][]string),
		}
		return globalUTXOSet
	}

	// Create wrapped cache object
	c := NewCachedUTXOSet(db)

	// IMPORTANT: load DB → cache
	if err := c.LoadAllFromDB(); err != nil {
		fmt.Println("Error loading UTXO from DB:", err)
	}

	globalUTXOSet = c
	return globalUTXOSet
}

// LoadAllFromDB loads all UTXO entries from Badger into RAM cache
func (c *CachedUTXOSet) LoadAllFromDB() error {
	if c.db == nil {
		return nil
	}

	fmt.Println("[UTXO] Loading all UTXOs from DB into RAM...")

	return c.db.db.View(func(txn *badger.Txn) error {

		// Iterate prefix "utxo:"
		prefix := []byte("utxo:")
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {

			item := it.Item()
			key := item.Key() // bytes "utxo:<txid>:<index>"

			err := item.Value(func(val []byte) error {
				out, err := deserializeVOUT(val)
				if err != nil {
					return err
				}

				// Parse UTXO key
				txid, idx := helper.ParseUTXOKey(key)

				// Insert into RAM
				utxo := UTXO{
					Txid:  txid,
					Index: idx,
					Vout:  out,
				}

				cacheKey := keyOf(txid, idx)

				c.Cache[cacheKey] = utxo

				if len(out.ScriptPubKey.Addresses) > 0 {
					addr := out.ScriptPubKey.Addresses[0]
					c.addrIndex[addr] = append(c.addrIndex[addr], cacheKey)
				}

				return nil
			})

			if err != nil {
				return err
			}
		}

		return nil
	})
}

// GetUTXOSet - Get the global UTXO set instance
// Must call InitUTXOSet first!
func GetUTXOSet() *CachedUTXOSet {
	if globalUTXOSet == nil {
		panic("UTXO set not initialized. Call InitUTXOSet() first!")
	}
	return globalUTXOSet
}

// LoadUTXOSet - Deprecated: use InitUTXOSet + GetUTXOSet instead
// For backward compatibility, this tries to use global instance
func LoadUTXOSet() *CachedUTXOSet {
	if globalUTXOSet != nil {
		return globalUTXOSet
	}
	return InitUTXOSet("./utxo_db")
}

func (b *BadgerUTXOSet) IterateAll(cb func(key []byte, val []byte) error) error {
	return b.db.View(func(txn *badger.Txn) error {

		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("utxo:")

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			k := item.KeyCopy(nil)

			err := item.Value(func(v []byte) error {
				return cb(k, v)
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}
