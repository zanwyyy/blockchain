package main

import (
	"fmt"
	"log"
	"runtime"
	"time"

	model "project/Model"
	mining "project/mining"
	storage "project/storage"
)

func main() {
	// -------------------------------
	// 0) CPU
	// -------------------------------
	runtime.GOMAXPROCS(runtime.NumCPU())

	fmt.Println("=== Blockchain Demo (In-Memory UTXO + BadgerDB + Mempool) ===")

	// -------------------------------
	// 1) OPEN BADGER DB
	// -------------------------------
	db, err := storage.OpenBadger("./data/utxo")
	if err != nil {
		log.Fatal("Open Badger failed:", err)
	}
	defer db.Close()

	// -------------------------------
	// 2) INIT IN-MEMORY STATE
	// -------------------------------
	utxoSet := model.NewUTXOSet()
	mempool := model.NewInMemoryMempool()
	blockchain := model.NewBlockchain()
	walletManager := model.NewWalletManager()

	// -------------------------------
	// 3) LOAD UTXO FROM DB
	// -------------------------------
	if err := utxoSet.LoadFromBadger(db); err != nil {
		log.Fatal("Load UTXO from DB failed:", err)
	}

	fmt.Println("Loaded confirmed UTXOs from DB")

	// -------------------------------
	// 4) CREATE KEYS
	// -------------------------------
	_, alicePub := model.NewKeyPair()
	bobPriv, bobPub := model.NewKeyPair()

	aliceAddr := model.AddressFromPub(alicePub)
	bobAddr := model.AddressFromPub(bobPub)

	fmt.Println("Alice Address:", aliceAddr)
	fmt.Println("Bob   Address:", bobAddr)

	// -------------------------------
	// 5) GENESIS (ONLY IF DB EMPTY)
	// -------------------------------
	if len(utxoSet.FindUTXOsByAddress(aliceAddr)) == 0 &&
		len(utxoSet.FindUTXOsByAddress(bobAddr)) == 0 {

		fmt.Println("\n== Insert genesis UTXOs ==")

		genesisAlice := model.Transaction{
			Version: 1,
			Vin:     nil,
			Vout: []model.VOUT{
				{
					Value:        500000,
					N:            0,
					ScriptPubKey: model.MakeP2PKHScriptPubKey(aliceAddr),
				},
			},
		}
		genesisAlice.Txid = genesisAlice.ComputeTxID()

		for _, out := range genesisAlice.Vout {
			if err := utxoSet.PutWithDB(db, genesisAlice.Txid, out.N, out); err != nil {
				log.Fatal(err)
			}
		}

		genesisBob := model.Transaction{
			Version: 1,
			Vin:     nil,
			Vout: []model.VOUT{
				{
					Value:        10000000,
					N:            0,
					ScriptPubKey: model.MakeP2PKHScriptPubKey(bobAddr),
				},
			},
		}
		genesisBob.Txid = genesisBob.ComputeTxID()

		for _, out := range genesisBob.Vout {
			if err := utxoSet.PutWithDB(db, genesisBob.Txid, out.N, out); err != nil {
				log.Fatal(err)
			}
		}

		fmt.Println("Genesis inserted")
	}

	// -------------------------------
	// 6) INIT WALLETS
	// -------------------------------
	aliceWallet := walletManager.GetWallet(aliceAddr, utxoSet)
	bobWallet := walletManager.GetWallet(bobAddr, utxoSet)

	fmt.Println("Alice spendable:", len(aliceWallet.GetSpendableUTXOs(mempool)))
	fmt.Println("Bob   spendable:", len(bobWallet.GetSpendableUTXOs(mempool)))

	// -------------------------------
	// 7) STRESS TEST: BOB → ALICE (10 000 TX)
	// -------------------------------
	fmt.Println("\n== Stress test: Bob → Alice (10,000 txs) ==")

	for i := 0; i < 10000; i++ {
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
			fmt.Printf("[tx %d] create failed: %v\n", i, err)
			break
		}

		if !model.VerifyForMempool(&tx, utxoSet, mempool) {
			fmt.Printf("[tx %d] verify failed\n", i)
			break
		}

		if err := mempool.AddTransaction(&tx); err != nil {
			fmt.Printf("[tx %d] mempool add failed: %v\n", i, err)
			break
		}

		walletManager.ApplyUnconfirmedTx(tx)

		if (i+1)%1000 == 0 {
			fmt.Printf("  submitted %d txs\n", i+1)
		}
	}

	fmt.Println(
		"Mempool size after stress:",
		mempool.Size(),
	)

	// -------------------------------
	// 8) START MINER (WITH DB)
	// -------------------------------
	fmt.Println("\n== Starting miner ==")
	miner := mining.NewMiner(blockchain, mempool, utxoSet, db)
	miner.StartMiner()

	// -------------------------------
	// 9) LOOP
	// -------------------------------
	for {
		fmt.Println(
			"[Tick]",
			"Mempool:", mempool.Size(),
			"Blocks:", len(blockchain.Blocks),
		)
		time.Sleep(2 * time.Second)
	}
}
