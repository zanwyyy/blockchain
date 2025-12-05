package subscriber

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "project/Model"
	"project/events"

	"cloud.google.com/go/pubsub"
	"github.com/btcsuite/btcd/btcec/v2"
)

func SubscribeTxCreate(
	ctx context.Context,
	sub *pubsub.Subscription,
	utxoSet *model.RedisCache,
	bc *model.Blockchain, // in-memory block builder
) error {

	return sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		defer msg.Ack()

		var req events.TxCreateRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			fmt.Println("ERROR parsing tx.create:", err)
			return
		}

		// Decode private key
		pkBytes, _ := hex.DecodeString(req.PrivateKeyHex)
		privKey, _ := btcec.PrivKeyFromBytes(pkBytes)

		// ---------------------------------------------
		// 1) LOCK THE ADDRESS (per-address locking)
		// ---------------------------------------------
		mu := model.GetAddrLock(req.FromAddr)
		mu.Lock()
		defer mu.Unlock()

		// ---------------------------------------------
		// 2) Create the transaction
		// ---------------------------------------------
		tx, err := model.CreateTransaction(
			privKey,
			req.FromAddr,
			req.ToAddr,
			req.Amount,
			utxoSet, // RedisCache as UTXO provider
		)
		if err != nil {
			fmt.Println("ERROR creating tx:", err)
			return
		}

		// ---------------------------------------------
		// 3) Verify (signature + UTXO)
		// ---------------------------------------------
		if ok := model.VerifyUTXO(&tx, utxoSet); !ok {
			fmt.Println("ERROR verifying tx:", tx.Txid)
			return
		}

		// ---------------------------------------------
		// 4) Apply UTXO update ATOMICALLY (Redis pipeline)
		// ---------------------------------------------
		if err := utxoSet.UpdateWithTransaction(tx); err != nil {
			fmt.Println("ERROR applying UTXO update:", err)
			return
		}

		// ---------------------------------------------
		// 5) Add TX to current block (in-memory)
		// ---------------------------------------------
		if err := bc.AddTransactionToBlock(tx, utxoSet); err != nil {
			fmt.Println("ERROR adding tx to block:", err)
			// Optionally: rollback Redis here (rare)
			return
		}

	})
}
