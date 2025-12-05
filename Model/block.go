package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const MaxBlockSizeBytes = 4 * 1024 * 1024 // 4 MB

type Block struct {
	Timestamp    int64
	Transactions []Transaction
	PrevHash     []byte
	Hash         []byte
	Nonce        int
	MerkleRoot   []byte
	Size         int
}

type Blockchain struct {
	Blocks []*Block
}

func (bc *Blockchain) AddBlock(txs []Transaction) {
	prevBlock := bc.Blocks[len(bc.Blocks)-1]
	newBlock := NewBlock(txs, prevBlock.Hash)
	bc.Blocks = append(bc.Blocks, newBlock)
}

func NewBlock(txs []Transaction, prevHash []byte) *Block {
	block := &Block{
		Timestamp:    time.Now().Unix(),
		Transactions: txs,
		PrevHash:     prevHash,
		Nonce:        0,
	}
	block.MerkleRoot = ComputeMerkleRoot(txs)
	block.Hash = block.BlockHash()
	return block
}

func NewGenesisBlock() *Block {
	return NewBlock([]Transaction{}, []byte{})
}

func NewBlockchain() *Blockchain {
	genesis := NewGenesisBlock()
	return &Blockchain{Blocks: []*Block{genesis}}
}

func (bc *Blockchain) AddTransactionToBlock(tx Transaction, utxoSet *RedisCache) error {
	//STEP 1: Validate
	// if !VerifyUTXO(&tx, utxoSet) {
	// 	return fmt.Errorf("transaction validation failed")
	// }

	// STEP 2: Block size limit 4MB
	current := bc.Blocks[len(bc.Blocks)-1]
	if current.CurrentSize()+tx.Size() > MaxBlockSizeBytes {
		// finalize old block
		current.MerkleRoot = ComputeMerkleRoot(current.Transactions)
		current.Hash = current.BlockHash()

		// create new block
		newBlock := NewBlock([]Transaction{}, current.Hash)
		bc.Blocks = append(bc.Blocks, newBlock)
		current = newBlock
	}

	// STEP 3: Add TX
	current.Transactions = append(current.Transactions, tx)

	// STEP 4: UTXO update
	utxoSet.UpdateWithTransaction(tx)

	// STEP 5: update Merkle + Hash
	current.MerkleRoot = ComputeMerkleRoot(current.Transactions)
	current.Hash = current.BlockHash()

	//Update Size
	current.Size += tx.Size()

	return nil
}

func (b *Block) SerializeHeader() []byte {
	buf := new(bytes.Buffer)

	// version (hardcode 1)
	binary.Write(buf, binary.LittleEndian, uint32(1))

	// prev hash padded to 32 bytes
	prev := make([]byte, 32)
	copy(prev[32-len(b.PrevHash):], b.PrevHash)
	buf.Write(prev)

	// merkle root padded to 32 bytes
	mr := make([]byte, 32)
	copy(mr[32-len(b.MerkleRoot):], b.MerkleRoot)
	buf.Write(mr)

	// timestamp
	binary.Write(buf, binary.LittleEndian, uint32(b.Timestamp))

	// bits (difficulty compact) — set 0 for now
	binary.Write(buf, binary.LittleEndian, uint32(0))

	// nonce
	binary.Write(buf, binary.LittleEndian, uint32(b.Nonce))

	return buf.Bytes()
}

func (b *Block) CurrentSize() int {
	size := len(b.SerializeHeader())

	return size + b.Size
}

func (b *Block) ExceedsMaxSize() bool {
	return b.CurrentSize() > MaxBlockSizeBytes
}

func doubleSHA256(b []byte) []byte {
	h1 := sha256.Sum256(b)
	h2 := sha256.Sum256(h1[:])
	return h2[:]
}
func (b *Block) BlockHash() []byte {
	return doubleSHA256(b.SerializeHeader())
}

// Global blockchain instance (singleton)
var globalBlockchain *Blockchain
var blockchainFilepath string

// InitBlockchain - Initialize blockchain singleton once at startup
// Load from file if exists, otherwise create new
func InitBlockchain(filepath string) *Blockchain {
	if globalBlockchain != nil {
		return globalBlockchain // Already initialized
	}

	// blockchainFilepath = filepath

	// // Try to load from file
	// data, err := os.ReadFile(filepath)
	// if err != nil {
	// 	// File not exist, create new blockchain
	// 	globalBlockchain = NewBlockchain()
	// 	return globalBlockchain
	// }

	var blocks []*Block
	// if err := json.Unmarshal(data, &blocks); err != nil {
	// 	// Parse error, create new blockchain
	// 	globalBlockchain = NewBlockchain()
	// 	return globalBlockchain
	// }

	if len(blocks) == 0 {
		globalBlockchain = NewBlockchain()
		return globalBlockchain
	}

	globalBlockchain = &Blockchain{Blocks: blocks}
	return globalBlockchain
}

// GetBlockchain - Get the global blockchain instance
// Must call InitBlockchain first!
func GetBlockchain() *Blockchain {
	if globalBlockchain == nil {
		panic("Blockchain not initialized. Call InitBlockchain() first!")
	}
	return globalBlockchain
}

// SaveBlockchain - Save current blockchain to file
func SaveBlockchain() error {
	if globalBlockchain == nil {
		return fmt.Errorf("blockchain not initialized")
	}
	data, err := json.MarshalIndent(globalBlockchain.Blocks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(blockchainFilepath, data, 0644)
}
