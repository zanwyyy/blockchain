package model

import (
	"runtime"
	"sync"

	"github.com/minio/sha256-simd"
)

type TxMerkleItem struct {
	Txid []byte
}

func (m TxMerkleItem) CalculateHash() ([]byte, error) {
	h := sha256.Sum256(m.Txid)
	return h[:], nil
}

func (m TxMerkleItem) Equals(other interface{}) (bool, error) {
	o, ok := other.(TxMerkleItem)
	if !ok {
		return false, nil
	}
	return string(m.Txid) == string(o.Txid), nil
}

func ComputeMerkleRoot(txs []Transaction) []byte {
	n := len(txs)
	if n == 0 {
		return make([]byte, 32)
	}
	if n == 1 {
		h1 := sha256.Sum256([]byte(txs[0].Txid))
		h2 := sha256.Sum256(h1[:])
		return h2[:]
	}

	// Build first level
	hashes := make([][]byte, n)
	for i := range txs {
		hashes[i] = []byte(txs[i].Txid)
	}

	// Parallel processing cho level đầu (nhiều txs nhất)
	if n > 1000 {
		numWorkers := runtime.NumCPU()
		var wg sync.WaitGroup
		chunkSize := (n + numWorkers - 1) / numWorkers

		nextLevel := make([][]byte, (n+1)/2)

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				start := workerID * chunkSize * 2
				end := start + chunkSize*2
				if end > n {
					end = n
				}

				buffer := make([]byte, 64)

				for i := start; i < end; i += 2 {
					if i >= n {
						break
					}

					copy(buffer[:32], hashes[i])
					if i+1 < n {
						copy(buffer[32:], hashes[i+1])
					} else {
						copy(buffer[32:], hashes[i])
					}

					h1 := sha256.Sum256(buffer)
					h2 := sha256.Sum256(h1[:])

					hash := make([]byte, 32)
					copy(hash, h2[:])
					nextLevel[i/2] = hash
				}
			}(w)
		}

		wg.Wait()
		hashes = nextLevel
	}

	// Sequential cho các level còn lại
	buffer := make([]byte, 64)
	for len(hashes) > 1 {
		nextLen := (len(hashes) + 1) / 2
		nextLevel := make([][]byte, 0, nextLen)

		for i := 0; i < len(hashes); i += 2 {
			copy(buffer[:32], hashes[i])
			if i+1 < len(hashes) {
				copy(buffer[32:], hashes[i+1])
			} else {
				copy(buffer[32:], hashes[i])
			}

			h1 := sha256.Sum256(buffer)
			h2 := sha256.Sum256(h1[:])

			hash := make([]byte, 32)
			copy(hash, h2[:])
			nextLevel = append(nextLevel, hash)
		}

		hashes = nextLevel
	}

	return hashes[0]
}
