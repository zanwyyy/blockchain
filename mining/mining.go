package mining

import (
	"fmt"
	"time"

	model "project/Model"

	"github.com/dgraph-io/badger/v4"
)

const (
	MaxBlockSizeBytes = 4 * 1024 * 1024 // 1MB
	BlockInterval     = 5 * time.Second
	MinerIdleSleep    = 100 * time.Millisecond
)

type Miner struct {
	Blockchain *model.Blockchain
	Mempool    *model.InMemoryMempool
	UTXOSet    *model.UTXOSet
	DB         *badger.DB

	stopCh chan struct{}
}

func NewMiner(
	bc *model.Blockchain,
	mempool *model.InMemoryMempool,
	utxoSet *model.UTXOSet,
	db *badger.DB,

) *Miner {
	return &Miner{
		Blockchain: bc,
		Mempool:    mempool,
		UTXOSet:    utxoSet,
		DB:         db,
		stopCh:     make(chan struct{}),
	}
}

// StartMiner chạy miner loop trong goroutine
func (m *Miner) StartMiner() {
	fmt.Println("[miner] started")

	go func() {
		ticker := time.NewTicker(MinerIdleSleep)
		defer ticker.Stop()

		blockStart := time.Now()

		for {
			select {
			case <-m.stopCh:
				fmt.Println("[miner] stopped")
				return

			case <-ticker.C:
				// 1️⃣ pull snapshot
				t0 := time.Now()
				snap := m.Mempool.SnapshotUntilSize(MaxBlockSizeBytes)
				tSnapshot := time.Since(t0)

				if len(snap.TxIDs) == 0 {
					blockStart = time.Now()
					continue
				}

				if snap.Size < MaxBlockSizeBytes/2 {
					continue // Đợi thêm tx
				}

				// 3️⃣ build block
				t1 := time.Now()
				// Collect transactions from mempool
				var txs []model.Transaction
				for _, txid := range snap.TxIDs {
					tx := m.Mempool.GetTransaction(txid)
					if tx == nil {
						continue
					}
					txs = append(txs, *tx)
				}

				// Get previous block hash
				prevBlock := m.Blockchain.Blocks[len(m.Blockchain.Blocks)-1]
				block := model.NewBlock(txs, prevBlock.Hash)
				tBuild := time.Since(t1)

				fmt.Printf(
					"[miner] building block with %d txs (%d bytes)\n",
					len(block.Transactions),
					snap.Size,
				)

				// 4️⃣ verify merkle root
				t2 := time.Now()
				if err := model.VerifyMerkleRoot(block); err != nil {
					fmt.Printf("[miner] merkle verification failed: %v\n", err)
					blockStart = time.Now()
					continue
				}
				tMerkle := time.Since(t2)

				// 5️⃣ verify block using VerifyBlock (proper verification)
				t3 := time.Now()
				if err := model.VerifyBlock(block, m.UTXOSet); err != nil {
					fmt.Printf("[miner] block verification failed: %v\n", err)
					blockStart = time.Now()
					continue
				}
				tVerify := time.Since(t3)

				// 6️⃣ commit block
				t4 := time.Now()
				if err := model.CommitBlock(block, m.UTXOSet, m.DB); err != nil {
					fmt.Println("[miner] commit block failed:", err)
					blockStart = time.Now()
					continue
				}
				tCommit := time.Since(t4)

				// Add block to blockchain
				m.Blockchain.Blocks = append(m.Blockchain.Blocks, block)

				// Tính duration trước khi cleanup
				duration := time.Since(blockStart)

				// 7️⃣ remove committed txs from mempool
				t5 := time.Now()
				for _, tx := range block.Transactions {
					m.Mempool.RemoveTransaction(&tx)
				}
				tCleanup := time.Since(t5)

				fmt.Printf(
					"[miner] ✓ block committed | height=%d | txs=%d | total=%v (excluding cleanup)\n",
					len(m.Blockchain.Blocks),
					len(block.Transactions),
					duration,
				)
				fmt.Printf(
					"  timing: snapshot=%v build=%v merkle=%v verify=%v commit=%v cleanup=%v\n",
					tSnapshot, tBuild, tMerkle, tVerify, tCommit, tCleanup,
				)

				blockStart = time.Now()
			}
		}
	}()
}

func (m *Miner) Stop() {
	close(m.stopCh)
}
