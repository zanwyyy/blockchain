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
	utxoSet *model.RedisCache, // canonical UTXO (read-only here)
	mempool *model.RedisMempool, // mempool overlay
	bc *model.Blockchain,
	walletManager *model.WalletManager,
) error {

	return sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		defer msg.Ack()

		var req events.TxCreateRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			fmt.Println("ERROR parsing tx.create:", err)
			return
		}

		// -----------------------------
		// 1) Build private key
		// -----------------------------
		seedBytes, err := hex.DecodeString(req.PrivateKeyHex)
		if err != nil || len(seedBytes) != ed25519.SeedSize {
			fmt.Println("ERROR invalid private key")
			return
		}
		privKey := ed25519.NewKeyFromSeed(seedBytes)

		// -----------------------------
		// 2) Per-address lock
		// -----------------------------
		mu := model.GetAddrLock(req.FromAddr)
		mu.Lock()
		defer mu.Unlock()

		wallet := walletManager.GetWallet(req.FromAddr, utxoSet)
		if wallet == nil {
			fmt.Println("ERROR wallet not found for address:", req.FromAddr)
			return
		}

		// -----------------------------
		// 3) Create transaction
		// (uses canonical UTXO + mempool outputs internally)
		// -----------------------------
		tx, err := model.CreateTransaction(
			privKey,
			req.FromAddr,
			req.ToAddr,
			req.Amount,
			utxoSet,
			mempool,
			wallet,
		)
		if err != nil {
			fmt.Println("ERROR creating tx:", err)
			return
		}

		// -----------------------------
		// 4) Verify for mempool
		// -----------------------------
		if ok := model.VerifyForMempool(&tx, utxoSet, mempool); !ok {
			fmt.Println("ERROR verifying tx:", tx.Txid)
			return
		}

		// -----------------------------
		// 5) Add to mempool (NOT UTXO)
		// -----------------------------

		if err := mempool.AddTransaction(tx); err != nil {
			fmt.Println("ERROR adding tx to mempool:", err)
			return
		}

		walletManager.ApplyUnconfirmedTx(tx)
		// -----------------------------
		// 6) Notify block builder
		// -----------------------------
		if err := bc.AddTransactionToBlock(tx); err != nil {
			if err.Error() == "current block full, must finalize first" {
				// finalize current block and start a new one
				err = bc.FinalizeCurrentBlock(utxoSet)
				if err != nil {
					fmt.Println("ERROR finalizing block:", err)
					return
				}
				fmt.Println("Block finalized. New block started.")
				if err := bc.AddTransactionToBlock(tx); err != nil {
					fmt.Println("ERROR adding tx to new block:", err)
					return
				}
			} else {
				fmt.Println("ERROR adding tx to block builder:", err)
				return
			}
		}

	})
}
