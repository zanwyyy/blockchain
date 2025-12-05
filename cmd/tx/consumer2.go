package main

import (
	"context"
	"fmt"
	"log"
	model "project/Model"
	pubsub2 "project/pubsub"
	subscriber "project/subcriber"
)

func main() {
	_, alicePub := model.NewKeyPair()
	aliceAddr := model.AddressFromPub(alicePub)
	redisUTXO := model.NewRedisCache("localhost:6379")
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
	defer redisUTXO.Close()
	ctx := context.Background()
	ps, err := pubsub2.NewPubSubClient(ctx, "thesis")
	if err != nil {
		log.Fatal("Failed creating PubSub client:", err)
	}

	sub := ps.Client.Subscription("tx-create-sub")

	// ----------------------------------------------------
	// 2) INIT Blockchain (block builder)
	// ----------------------------------------------------
	bc := model.InitBlockchain("./blocks")
	err = subscriber.SubscribeTxCreate(ctx, sub, redisUTXO, bc)
	if err != nil {
		log.Println("SubscribeTxCreate error:", err)
	}
}
