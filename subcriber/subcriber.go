package subscriber

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	model "project/Model"
	"project/events"

	"crypto/ed25519"

	"cloud.google.com/go/pubsub"
)

func SubscribeTxCreate(
	ctx context.Context,
	sub *pubsub.Subscription,
	utxoSet *model.RedisCache,
	bc *model.Blockchain,
) error {

	return sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		defer msg.Ack()

		var req events.TxCreateRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			fmt.Println("ERROR parsing tx.create:", err)
			return
		}

		// 🔐 Decode seed → Ed25519 private key
		seedBytes, err := hex.DecodeString(req.PrivateKeyHex)
		if err != nil {
			fmt.Println("ERROR decoding private key:", err)
			return
		}
		if len(seedBytes) != ed25519.SeedSize {
			fmt.Println("ERROR: invalid seed length, must be 32 bytes")
			return
		}
		privKey := ed25519.NewKeyFromSeed(seedBytes)

		// ---------------------------------------------
		// 1) Per-address lock
		// ---------------------------------------------
		mu := model.GetAddrLock(req.FromAddr)
		mu.Lock()
		defer mu.Unlock()

		// ---------------------------------------------
		// 2) Create transaction
		// ---------------------------------------------
		tx, err := model.CreateTransaction(
			privKey, // ⚡ now Ed25519
			req.FromAddr,
			req.ToAddr,
			req.Amount,
			utxoSet,
		)
		if err != nil {
			fmt.Println("ERROR creating tx:", err)
			return
		}

		// ---------------------------------------------
		// 3) Verify
		// ---------------------------------------------
		if ok := model.VerifyUTXO(&tx, utxoSet); !ok {
			fmt.Println("ERROR verifying tx:", tx.Txid)
			return
		}

		// ---------------------------------------------
		// 4) Apply UTXO update
		// ---------------------------------------------
		if err := utxoSet.UpdateWithTransaction(tx); err != nil {
			fmt.Println("ERROR applying UTXO update:", err)
			return
		}

		// ---------------------------------------------
		// 5) Add TX to block builder
		// ---------------------------------------------
		if err := bc.AddTransactionToBlock(tx, utxoSet); err != nil {
			fmt.Println("ERROR adding tx to block:", err)
			return
		}
	})
}
