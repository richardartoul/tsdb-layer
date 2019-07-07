package rawblock

import (
	"testing"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/stretchr/testify/require"
)

func TestCommitlogBootstrapLastIndex(t *testing.T) {
	cl := NewCommitlog(fdb.Database{}, NewCommitlogOptions())
	require.NoError(t, cl.Open())
	require.NoError(t, cl.Close())
}
