package storage

import (
	"github.com/dgraph-io/badger/v4"
)

func OpenBadger(path string) (*badger.DB, error) {
	opts := badger.DefaultOptions(path).
		WithLogger(nil) // tắt log cho đỡ nhiễu
	return badger.Open(opts)
}
