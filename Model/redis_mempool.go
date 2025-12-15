package model

import (
	"context"
	"encoding/json"
	"fmt"
	"project/metrics"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisMempool struct {
	ctx context.Context
	rdb *redis.Client
}

func NewRedisMempool(addr string) *RedisMempool {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &RedisMempool{
		ctx: context.Background(),
		rdb: rdb,
	}
}

func (r *RedisMempool) Close() error {
	return r.rdb.Close()
}

// ---------- key helpers ----------

func mempoolTxKey(txid string) string {
	return fmt.Sprintf("mempool:tx:%s", txid)
}

func mempoolOutKey(txid string, vout int) string {
	return fmt.Sprintf("mempool:out:%s:%d", txid, vout)
}

func mempoolSpentKey(txid string, vout int) string {
	return fmt.Sprintf("mempool:spent:%s:%d", txid, vout)
}

func mempoolAddrKey(addr string) string {
	return fmt.Sprintf("mempool:addr:%s", addr)
}

func (m *RedisMempool) AddTransaction(tx Transaction) error {
	pipe := m.rdb.TxPipeline()

	// 1. Save transaction
	rawTx, _ := json.Marshal(tx)
	pipe.Set(m.ctx, mempoolTxKey(tx.Txid), rawTx, 0)
	pipe.SAdd(m.ctx, "mempool:all", tx.Txid)

	// 2. Mark inputs as spent (double-spend protection)
	for _, vin := range tx.Vin {
		if vin.Txid == "" {
			continue // coinbase
		}
		pipe.Set(
			m.ctx,
			mempoolSpentKey(vin.Txid, vin.Vout),
			tx.Txid,
			0,
		)
	}

	// 3. Store unconfirmed outputs (UTXO tạm)
	// 3. Store unconfirmed outputs (UTXO tạm)
	for _, out := range tx.Vout {
		rawOut, _ := json.Marshal(out)
		outKey := mempoolOutKey(tx.Txid, out.N)

		pipe.Set(m.ctx, outKey, rawOut, 0)

		if len(out.ScriptPubKey.Addresses) > 0 {
			addr := out.ScriptPubKey.Addresses[0]
			pipe.SAdd(m.ctx, mempoolAddrKey(addr), outKey)
		}
	}

	_, err := pipe.Exec(m.ctx)
	return err
}

func (m *RedisMempool) IsSpent(txid string, vout int) bool {
	start := time.Now()
	defer func() {
		metrics.FnDuration.
			WithLabelValues("mempool_is_spent").
			Observe(float64(time.Since(start).Milliseconds()))
	}()

	key := mempoolSpentKey(txid, vout)
	exists, _ := m.rdb.Exists(m.ctx, key).Result()
	return exists == 1
}

func (m *RedisMempool) HasOutput(txid string, vout int) bool {
	key := mempoolOutKey(txid, vout)
	exists, _ := m.rdb.Exists(m.ctx, key).Result()
	return exists == 1
}
func (m *RedisMempool) GetOutput(txid string, vout int) (VOUT, bool) {
	key := mempoolOutKey(txid, vout)
	raw, err := m.rdb.Get(m.ctx, key).Bytes()
	if err != nil {
		return VOUT{}, false
	}

	var out VOUT
	_ = json.Unmarshal(raw, &out)
	return out, true
}
func (m *RedisMempool) RemoveTransaction(tx Transaction) error {
	pipe := m.rdb.TxPipeline()

	pipe.Del(m.ctx, mempoolTxKey(tx.Txid))
	pipe.SRem(m.ctx, "mempool:all", tx.Txid)

	// remove spent marks
	for _, vin := range tx.Vin {
		if vin.Txid == "" {
			continue
		}
		pipe.Del(m.ctx, mempoolSpentKey(vin.Txid, vin.Vout))
	}

	// remove unconfirmed outputs
	for _, out := range tx.Vout {
		outKey := mempoolOutKey(tx.Txid, out.N)
		pipe.Del(m.ctx, outKey)

		if len(out.ScriptPubKey.Addresses) > 0 {
			addr := out.ScriptPubKey.Addresses[0]
			pipe.SRem(m.ctx, mempoolAddrKey(addr), outKey)
		}
	}

	_, err := pipe.Exec(m.ctx)
	return err
}

func (m *RedisMempool) FindOutputsByAddress(addr string) []UTXO {
	start := time.Now()
	defer func() {
		metrics.FnDuration.
			WithLabelValues("mempool_find_outputs").
			Observe(float64(time.Since(start).Milliseconds()))
	}()
	keys, err := m.rdb.SMembers(m.ctx, mempoolAddrKey(addr)).Result()
	if err != nil {
		return nil
	}

	var res []UTXO

	for _, k := range keys {
		// k = mempool:out:<txid>:<vout>
		parts := strings.Split(k, ":")
		if len(parts) != 4 {
			continue
		}

		txid := parts[2]
		vout, err := strconv.Atoi(parts[3])
		if err != nil {
			continue
		}

		// skip if spent in mempool
		if m.IsSpent(txid, vout) {
			continue
		}

		raw, err := m.rdb.Get(m.ctx, k).Bytes()
		if err != nil {
			continue
		}

		var out VOUT
		if err := json.Unmarshal(raw, &out); err != nil {
			continue
		}

		res = append(res, UTXO{
			Txid:  txid,
			Index: vout,
			Vout:  out,
		})
	}

	return res
}
