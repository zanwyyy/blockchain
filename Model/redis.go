package model

import (
	"context"
	"encoding/json"
	"fmt"

	"project/helper"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	ctx context.Context
	rdb *redis.Client
	// NOTE: no bdb reference here — Redis-only cache layer.
}

func NewRedisCache(addr string) *RedisCache {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &RedisCache{
		ctx: context.Background(),
		rdb: rdb,
	}
}

func (r *RedisCache) Close() error {
	return r.rdb.Close()
}

func redisUtxoKey(txid string, idx int) string {
	return fmt.Sprintf("utxo:%s:%d", txid, idx)
}

func redisAddrKey(addr string) string {
	return fmt.Sprintf("addr:%s", addr)
}

// ----------------------------------
// GET (Redis only)
// ----------------------------------
func (r *RedisCache) Get(txid string, idx int) (UTXO, bool) {
	key := redisUtxoKey(txid, idx)

	raw, err := r.rdb.Get(r.ctx, key).Bytes()
	if err == nil {
		var out VOUT
		_ = json.Unmarshal(raw, &out)
		return UTXO{Txid: txid, Index: idx, Vout: out}, true
	}
	// not found or other redis error
	return UTXO{}, false
}

// ----------------------------------
// Put (Redis only) — used when adding outputs to UTXO set
// ----------------------------------
func (r *RedisCache) Put(txid string, idx int, out VOUT) error {
	key := redisUtxoKey(txid, idx)
	b, _ := json.Marshal(out)

	if err := r.rdb.Set(r.ctx, key, b, 0).Err(); err != nil {
		return err
	}
	if len(out.ScriptPubKey.Addresses) > 0 {
		addr := out.ScriptPubKey.Addresses[0]
		if err := r.rdb.SAdd(r.ctx, redisAddrKey(addr), key).Err(); err != nil {
			return err
		}
	}
	return nil
}

// ----------------------------------
// Delete (Redis only) — used when removing spent UTXO
// ----------------------------------
func (r *RedisCache) Delete(txid string, idx int) error {
	key := redisUtxoKey(txid, idx)

	raw, err := r.rdb.Get(r.ctx, key).Bytes()
	if err == nil {
		var out VOUT
		_ = json.Unmarshal(raw, &out)
		if len(out.ScriptPubKey.Addresses) > 0 {
			addr := out.ScriptPubKey.Addresses[0]
			_ = r.rdb.SRem(r.ctx, redisAddrKey(addr), key)
		}
	}
	_ = r.rdb.Del(r.ctx, key)
	return nil
}

// ----------------------------------
// FindUTXOsByAddress (Redis set lookup)
// ----------------------------------
func (r *RedisCache) FindUTXOsByAddress(addr string) []UTXO {
	keys, err := r.rdb.SMembers(r.ctx, redisAddrKey(addr)).Result()
	if err != nil {
		// if Redis error or empty, return nil
		return nil
	}

	var res []UTXO
	for _, k := range keys {
		txid, idx := helper.ParseUTXOKey([]byte(k))
		u, ok := r.Get(txid, idx)
		if ok {
			res = append(res, u)
		}
	}
	return res
}

// ----------------------------------
// UpdateWithTransaction (Redis-only, atomic pipeline)
// This function will NOT touch Badger. Persisting to Badger should be done
// by your DB-writer (consumer #3) after block commit.
// ----------------------------------
func (r *RedisCache) UpdateWithTransaction(tx Transaction) error {
	pipe := r.rdb.TxPipeline()

	// delete inputs
	for _, vin := range tx.Vin {
		if vin.Txid == "" {
			continue
		}
		key := redisUtxoKey(vin.Txid, vin.Vout)

		// try get address to remove from set
		raw, err := r.rdb.Get(r.ctx, key).Bytes()
		if err == nil {
			var out VOUT
			_ = json.Unmarshal(raw, &out)
			if len(out.ScriptPubKey.Addresses) > 0 {
				pipe.SRem(r.ctx, redisAddrKey(out.ScriptPubKey.Addresses[0]), key)
			}
		}

		pipe.Del(r.ctx, key)
	}

	// add outputs
	for _, out := range tx.Vout {
		key := redisUtxoKey(tx.Txid, out.N)
		b, _ := json.Marshal(out)
		pipe.Set(r.ctx, key, b, 0)
		if len(out.ScriptPubKey.Addresses) > 0 {
			pipe.SAdd(r.ctx, redisAddrKey(out.ScriptPubKey.Addresses[0]), key)
		}
	}

	_, err := pipe.Exec(r.ctx)
	return err
}
