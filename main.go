package main

import (
	"fmt"
	"log"
	"runtime"
	"time"

	model "project/Model"
	mining "project/mining"
)

func main() {
	// -------------------------------
	// 0) CPU
	// -------------------------------
	runtime.GOMAXPROCS(runtime.NumCPU())

	fmt.Println("=== Blockchain Demo (In-Memory UTXO + Mempool) ===")

	// -------------------------------
	// 1) INIT IN-MEMORY STATE
	// -------------------------------
	utxoSet := model.NewUTXOSet()
	mempool := model.NewInMemoryMempool()
	blockchain := model.NewBlockchain()
	walletManager := model.NewWalletManager()

	// -------------------------------
	// 2) CREATE KEYS
	// -------------------------------
	alicePriv, alicePub := model.NewKeyPair()
	bobPriv, bobPub := model.NewKeyPair()

	aliceAddr := model.AddressFromPub(alicePub)
	bobAddr := model.AddressFromPub(bobPub)

	fmt.Println("Alice Address:", aliceAddr)
	fmt.Println("Bob   Address:", bobAddr)

	// -------------------------------
	// 3) GENESIS UTXO (ALICE)
	// -------------------------------
	genesis := model.Transaction{
		Version: 1,
		Vin:     nil,
		Vout: []model.VOUT{
			{
				Value:        500000,
				N:            0,
				ScriptPubKey: model.MakeP2PKHScriptPubKey(aliceAddr),
			},
		},
		LockTime: 0,
	}
	genesis.Txid = genesis.ComputeTxID()

	fmt.Println("\n== Insert genesis UTXO ==")

	for _, out := range genesis.Vout {
		if err := utxoSet.Put(genesis.Txid, out.N, out); err != nil {
			log.Fatal("Insert genesis failed:", err)
		}
	}

	fmt.Println(
		"Genesis done. Alice confirmed UTXOs:",
		len(utxoSet.FindUTXOsByAddress(aliceAddr)),
	)

	// -------------------------------
	// 3b) GENESIS UTXO (BOB)
	// -------------------------------
	genesisBob := model.Transaction{
		Version: 1,
		Vin:     nil,
		Vout: []model.VOUT{
			{
				Value:        100,
				N:            0,
				ScriptPubKey: model.MakeP2PKHScriptPubKey(bobAddr),
			},
		},
		LockTime: 0,
	}
	genesisBob.Txid = genesisBob.ComputeTxID()

	fmt.Println("\n== Insert genesis UTXO for Bob ==")

	for _, out := range genesisBob.Vout {
		if err := utxoSet.Put(genesisBob.Txid, out.N, out); err != nil {
			log.Fatal("Insert genesis Bob failed:", err)
		}
	}

	fmt.Println(
		"Genesis Bob done. Bob confirmed UTXOs:",
		len(utxoSet.FindUTXOsByAddress(bobAddr)),
	)

	// -------------------------------
	// 4) INIT WALLETS
	// -------------------------------
	aliceWallet := walletManager.GetWallet(aliceAddr, utxoSet)
	bobWallet := walletManager.GetWallet(bobAddr, utxoSet)

	fmt.Println(
		"Alice wallet spendable:",
		len(aliceWallet.GetSpendableUTXOs(mempool)),
	)
	fmt.Println(
		"Bob wallet spendable:",
		len(bobWallet.GetSpendableUTXOs(mempool)),
	)

	// -------------------------------
	// 5) CREATE TX: ALICE -> BOB
	// -------------------------------
	fmt.Println("\n== Create tx: Alice -> Bob (amount = 10) ==")

	tx, err := model.CreateTransaction(
		alicePriv,
		aliceAddr,
		bobAddr,
		10,
		utxoSet,
		mempool,
		aliceWallet,
	)
	if err != nil {
		log.Fatal("Create tx failed:", err)
	}

	fmt.Println("Tx created:", tx.Txid)

	// verify for mempool
	if !model.VerifyForMempool(&tx, utxoSet, mempool) {
		log.Fatal("Tx verify failed")
	}

	// add to mempool
	if err := mempool.AddTransaction(&tx); err != nil {
		log.Fatal("Add tx to mempool failed:", err)
	}

	fmt.Println("Tx added to mempool")

	// -------------------------------
	// 6) STATE AFTER MEMPOOL UPDATE
	// -------------------------------
	fmt.Println("\n== State after mempool update ==")

	fmt.Println(
		"Alice spendable (confirmed + mempool):",
		len(aliceWallet.GetSpendableUTXOs(mempool)),
	)
	fmt.Println(
		"Bob spendable (confirmed + mempool):",
		len(bobWallet.GetSpendableUTXOs(mempool)),
	)
	fmt.Println(
		"Mempool tx count:",
		mempool.Size(),
	)

	// -------------------------------
	// 7) CREATE TX: BOB -> ALICE (10 transactions, value = 1 each)
	// -------------------------------
	fmt.Println("\n== Create txs: Bob -> Alice (10 transactions, value = 1 each) ==")

	for i := 0; i < 10; i++ {
		tx, err := model.CreateTransaction(
			bobPriv,
			bobAddr,
			aliceAddr,
			1,
			utxoSet,
			mempool,
			bobWallet,
		)
		if err != nil {
			fmt.Printf("Create tx %d failed: %v\n", i, err)
			continue
		}

		// verify for mempool
		if !model.VerifyForMempool(&tx, utxoSet, mempool) {
			fmt.Printf("Tx %d verify failed\n", i)
			continue
		}

		// add to mempool
		if err := mempool.AddTransaction(&tx); err != nil {
			fmt.Printf("Add tx %d to mempool failed: %v\n", i, err)
			continue
		}

		fmt.Printf("[%d] Tx created and added: %s\n", i+1, tx.Txid[:8])

		// Update wallet with unconfirmed tx
		walletManager.ApplyUnconfirmedTx(tx)
	}

	// -------------------------------
	// 8) STATE AFTER ALL TXES
	// -------------------------------
	fmt.Println("\n== State after all transactions ==")

	fmt.Println(
		"Alice spendable (confirmed + mempool):",
		len(aliceWallet.GetSpendableUTXOs(mempool)),
	)
	fmt.Println(
		"Bob spendable (confirmed + mempool):",
		len(bobWallet.GetSpendableUTXOs(mempool)),
	)
	fmt.Println(
		"Mempool tx count:",
		mempool.Size(),
	)

	// -------------------------------
	// 9) START MINER
	// -------------------------------
	fmt.Println("\n== Starting miner ==")
	miner := mining.NewMiner(blockchain, mempool, utxoSet)
	miner.StartMiner()

	// -------------------------------
	// 10) SIMPLE LOOP
	// -------------------------------
	for {
		fmt.Println(
			"[Tick]",
			"Mempool txs:", mempool.Size(),
			"Blocks:", len(blockchain.Blocks),
		)
		time.Sleep(2 * time.Second)
	}
}
