package model

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateEd25519Keypair returns seed (32 bytes), private key (ed25519.PrivateKey) and public (ed25519.PublicKey)
func NewEd25519Keypair() (seed []byte, priv ed25519.PrivateKey, pub ed25519.PublicKey, err error) {
	seed = make([]byte, ed25519.SeedSize)
	if _, err = rand.Read(seed); err != nil {
		return nil, nil, nil, err
	}
	priv = ed25519.NewKeyFromSeed(seed) // 64 bytes
	pub = priv.Public().(ed25519.PublicKey)
	return seed, priv, pub, nil
}

// Helper to encode seed to hex for Publish
func SeedToHex(seed []byte) string {
	return hex.EncodeToString(seed)
}

// Recover private from hex seed
func PrivFromSeedHex(seedHex string) (ed25519.PrivateKey, error) {
	b, err := hex.DecodeString(seedHex)
	if err != nil {
		return nil, err
	}
	if len(b) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid seed length: %d", len(b))
	}
	return ed25519.NewKeyFromSeed(b), nil
}

// NewKeyPair returns an Ed25519 private key (64 bytes) and public key (32 bytes)
func NewKeyPair() (ed25519.PrivateKey, ed25519.PublicKey) {
	// pub:32, priv:64
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return priv, pub
}
func AddressFromPub(pub ed25519.PublicKey) string {
	h := HashPubKey(pub) // pub is []byte (32 bytes)
	return hex.EncodeToString(h)
}
func PrivToSeedHex(priv ed25519.PrivateKey) string {
	seed := priv.Seed() // 32 bytes
	return hex.EncodeToString(seed)
}
