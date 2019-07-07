package rawblock

import (
	"fmt"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
)

type cleanupFn func()

func newTestFDB() (fdb.Database, cleanupFn) {
	fdb.MustAPIVersion(610)
	// TODO(rartoul): Should truncate database before and after.
	db := fdb.MustOpenDefault()
	truncateFDB(db)
	cleanupFn := func() { truncateFDB(db) }
	return db, cleanupFn
}

func truncateFDB(db fdb.Database) {
	_, err := db.Transact(func(tr fdb.Transaction) (interface{}, error) {
		tr.ClearRange(fdb.KeyRange{Begin: tuple.Tuple{""}, End: tuple.Tuple{0xFF}})
		return nil, nil
	})
	if err != nil {
		panic(fmt.Sprintf("error truncating fdb: %v", err))
	}
}
