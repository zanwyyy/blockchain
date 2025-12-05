package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"time"

	model "project/Model"
	"project/events"
	pubsub2 "project/pubsub"
	subscriber "project/subcriber"
)

func main() {
	// Use all CPU cores
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Start pprof for performance debugging
	go func() {
		fmt.Println("pprof running at http://localhost:6060/debug/pprof/")
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			fmt.Println("pprof error:", err)
		}
	}()

	fmt.Println("=== Blockchain Demo (Redis UTXO + Per-Address Locks) ===")

	// ----------------------------------------------------
	// 1) INIT Redis UTXO Set  (NO BADGER)
	// ----------------------------------------------------
	redisUTXO := model.NewRedisCache("localhost:6379")
	defer redisUTXO.Close()

	// ----------------------------------------------------
	// 2) INIT Blockchain (block builder)
	// ----------------------------------------------------
	bc := model.InitBlockchain("./blocks")

	// ----------------------------------------------------
	// 3) Create Wallet Keys
	// ----------------------------------------------------
	alicePriv, alicePub := model.NewKeyPair()
	bobPriv, bobPub := model.NewKeyPair()

	aliceAddr := model.AddressFromPub(alicePub)
	bobAddr := model.AddressFromPub(bobPub)

	fmt.Println("Alice Address:", aliceAddr)
	fmt.Println("Bob   Address:", bobAddr)

	// ----------------------------------------------------
	// 4) Create Genesis UTXO for Alice
	// ----------------------------------------------------
	genesis := model.Transaction{
		Version: 1,
		Vin:     nil,
		Vout: []model.VOUT{
			{
				Value:        500000, // genesis money
				N:            0,
				ScriptPubKey: model.MakeP2PKHScriptPubKey(aliceAddr),
			},
		},
		LockTime: 0,
	}
	genesis.Txid = genesis.ComputeTxID()

	fmt.Println("\n== Insert genesis UTXO to Redis == ")

	for _, out := range genesis.Vout {
		err := redisUTXO.Put(genesis.Txid, out.N, out)
		if err != nil {
			log.Fatal("Insert genesis UTXO failed:", err)
		}
	}

	fmt.Println("Genesis done. UTXO count for Alice:",
		len(redisUTXO.FindUTXOsByAddress(aliceAddr)))

	// ----------------------------------------------------
	// 5) START PUBSUB CONSUMER (SubscribeTxCreate)
	// ----------------------------------------------------
	ctx := context.Background()

	ps, err := pubsub2.NewPubSubClient(ctx, "thesis")
	if err != nil {
		log.Fatal("Failed creating PubSub client:", err)
	}

	sub := ps.Client.Subscription("tx-create-sub")

	fmt.Println("\n== Subscriber: Listening tx.create ==")

	//consumer: on tx.create → create tx → verify → update UTXO → add to block
	go func() {
		err := subscriber.SubscribeTxCreate(ctx, sub, redisUTXO, bc)
		if err != nil {
			log.Println("SubscribeTxCreate error:", err)
		}
	}()

	// ----------------------------------------------------
	// 6) TEST: Publish 1 TxCreate request
	// ----------------------------------------------------
	time.Sleep(2 * time.Second)

	fmt.Println("\n== Test: Publishing tx.create event ==")

	testEvent := events.TxCreateRequest{
		PrivateKeyHex: hex.EncodeToString(alicePriv.Serialize()),
		FromAddr:      aliceAddr,
		ToAddr:        bobAddr,
		Amount:        30000,
	}

	ps.PublishTxCreate(ctx, testEvent)

	// If want stress test, uncomment:

	for i := 0; i < 5000; i++ {
		go func() {
			ev := events.TxCreateRequest{
				PrivateKeyHex: hex.EncodeToString(bobPriv.Serialize()),
				FromAddr:      bobAddr,
				ToAddr:        aliceAddr,
				Amount:        1,
			}
			err := ps.PublishTxCreate(ctx, ev)
			if err != nil {
				fmt.Printf("Published tx.create from Bob to Alice: %v\n", err)
			}
			// ev2 := events.TxCreateRequest{
			// 	PrivateKeyHex: hex.EncodeToString(alicePriv.Serialize()),
			// 	FromAddr:      aliceAddr,
			// 	ToAddr:        bobAddr,
			// 	Amount:        1,
			// }
			// ps.PublishTxCreate(ctx, ev2)
		}()
	}

	// ----------------------------------------------------
	// 7) Show blockchain state every second
	// ----------------------------------------------------
	for {
		fmt.Println("Blocks:", len(bc.Blocks),
			"Tx in block 0:", len(bc.Blocks[0].Transactions))
		time.Sleep(1 * time.Second)
	}
}
