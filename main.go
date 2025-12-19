package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"

	model "project/Model"
	"project/events"
	"project/metrics"
	pubsub2 "project/pubsub"
	subscriber "project/subcriber"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// -------------------------------
	// 0) METRICS SERVER
	// -------------------------------
	go startMetricsServer()

	// Use all CPU cores
	runtime.GOMAXPROCS(runtime.NumCPU())

	fmt.Println("=== Blockchain Demo (Redis UTXO + WalletManager) ===")

	// -------------------------------
	// 1) INIT REDIS
	// -------------------------------
	redisUTXO := model.NewRedisCache("localhost:6379")
	redisMempool := model.NewRedisMempool("localhost:6380")
	defer redisUTXO.Close()
	defer redisMempool.Close()

	// -------------------------------
	// 2) INIT BLOCKCHAIN
	// -------------------------------
	bc := model.NewBlockchain()

	// -------------------------------
	// 3) INIT WALLET MANAGER
	// -------------------------------
	walletManager := model.NewWalletManager()

	// -------------------------------
	// 4) CREATE KEYS
	// -------------------------------
	alicePriv, alicePub := model.NewKeyPair()
	_, bobPub := model.NewKeyPair()

	aliceAddr := model.AddressFromPub(alicePub)
	bobAddr := model.AddressFromPub(bobPub)

	fmt.Println("Alice Address:", aliceAddr)
	fmt.Println("Bob   Address:", bobAddr)

	// -------------------------------
	// 5) GENESIS UTXO (ALICE)
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

	fmt.Println("\n== Insert genesis UTXO to Redis ==")

	for _, out := range genesis.Vout {
		if err := redisUTXO.Put(genesis.Txid, out.N, out); err != nil {
			log.Fatal("Insert genesis UTXO failed:", err)
		}
	}

	fmt.Println("Genesis done. UTXO count for Alice:",
		len(redisUTXO.FindUTXOsByAddress(aliceAddr)))

	// 👉 IMPORTANT: preload Alice wallet once
	aliceWallet := walletManager.GetWallet(aliceAddr, redisUTXO)
	fmt.Println("Alice wallet initialized with",
		len(aliceWallet.GetSpendableUTXOs(redisMempool)), "UTXOs")
	bobWallet := walletManager.GetWallet(bobAddr, redisUTXO)
	fmt.Println("Bob wallet initialized with",
		len(bobWallet.GetSpendableUTXOs(redisMempool)), "UTXOs")

	// -------------------------------
	// 6) START SUBSCRIBER
	// -------------------------------
	ctx := context.Background()

	ps, err := pubsub2.NewPubSubClient(ctx, "thesis")
	if err != nil {
		log.Fatal("Failed creating PubSub client:", err)
	}

	sub := ps.Client.Subscription("tx-create-sub")

	fmt.Println("\n== Subscriber: Listening tx.create ==")

	go func() {
		err := subscriber.SubscribeTxCreate(
			ctx,
			sub,
			redisUTXO,
			redisMempool,
			bc,
			walletManager, // 👈 NEW
		)
		if err != nil {
			log.Println("SubscribeTxCreate error:", err)
		}
	}()

	// -------------------------------
	// 7) TEST EVENTS
	// -------------------------------
	fmt.Println("\n== Test: Publishing tx.create (Alice → Bob) ==")

	time.Sleep(2 * time.Second)
	for _, wallet := range walletManager.Wallets {
		fmt.Printf("Wallet %s has %d UTXOs (including mempool)\n",
			wallet.Address,
			len(wallet.GetSpendableUTXOs(redisMempool)),
		)
	}
	fmt.Println("\n== Test: Publishing tx.create again (Alice → Bob) ==")

	// Stress test Bob → Alice
	for i := 0; i < 5000; i++ {
		go func() {
			ev := events.TxCreateRequest{
				PrivateKeyHex: model.PrivToSeedHex(alicePriv),
				FromAddr:      aliceAddr,
				ToAddr:        bobAddr,
				Amount:        1,
			}
			if err := ps.PublishTxCreate(ctx, ev); err != nil {
				fmt.Printf("Publish Alice→Bob error: %v\n", err)
			}
		}()
	}

	// -------------------------------
	// 8) LOOP
	// -------------------------------
	for {
		fmt.Println(
			"Blocks:", len(bc.Blocks),
			"Tx in current block:", len(bc.CurrentBlock.Transactions),
		)

		time.Sleep(1 * time.Second)
	}
}

func startMetricsServer() {
	metrics.Register()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	fmt.Println("Prometheus metrics running at http://localhost:2112/metrics")

	if err := http.ListenAndServe(":2112", mux); err != nil {
		fmt.Println("Metrics server error:", err)
	}
}
