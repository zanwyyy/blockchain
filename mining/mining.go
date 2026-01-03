package mining

import (
	"fmt"
	"time"

	model "project/Model"

	"github.com/dgraph-io/badger/v4"
)

const (
	MaxBlockSizeBytes = 1 * 1024 * 1024 // 1MB
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
				snap := m.Mempool.SnapshotUntilSize(MaxBlockSizeBytes)

				if len(snap.TxIDs) == 0 {
					blockStart = time.Now()
					continue
				}

				// 2️⃣ check block condition
				if snap.Size < MaxBlockSizeBytes &&
					time.Since(blockStart) < BlockInterval {
					continue
				}

				// 3️⃣ build block
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

				fmt.Printf(
					"[miner] building block with %d txs (%d bytes)\n",
					len(block.Transactions),
					snap.Size,
				)

				if err := model.VerifyMerkleRoot(block); err != nil {
					fmt.Printf("[miner] block verification failed: %v\n", err)
					blockStart = time.Now()
					continue
				}

				// 4️⃣ verify block using VerifyBlock (proper verification)
				if err := model.VerifyBlock(block, m.UTXOSet); err != nil {
					fmt.Printf("[miner] block verification failed: %v\n", err)
					blockStart = time.Now()
					continue
				}

				// 5️⃣ commit block
				if err := model.CommitBlock(block, m.UTXOSet, m.DB); err != nil {
					fmt.Println("[miner] commit block failed:", err)
					blockStart = time.Now()
					continue
				}

				// Add block to blockchain
				m.Blockchain.Blocks = append(m.Blockchain.Blocks, block)

				// 6️⃣ remove committed txs from mempool
				for _, tx := range block.Transactions {
					m.Mempool.RemoveTransaction(&tx)
				}

				duration := time.Since(blockStart)
				fmt.Printf(
					"[miner] block committed | height=%d | txs=%d | time=%v\n",
					len(m.Blockchain.Blocks),
					len(block.Transactions),
					duration,
				)

				blockStart = time.Now()
			}
		}
	}()
}

func (m *Miner) Stop() {
	close(m.stopCh)
}
