package rawblock

import "github.com/apple/foundationdb/bindings/go/src/fdb"

type cleanupFn func()

func newTestFDB() (fdb.Database, cleanupFn) {
	fdb.MustAPIVersion(610)
	// TODO(rartoul): Should truncate database before and after.
	db := fdb.MustOpenDefault()
	cleanupFn := func() {}
	return db, cleanupFn
}
