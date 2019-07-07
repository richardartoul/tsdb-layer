package rawblock

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommitlogBootstrapLastIndex(t *testing.T) {
	db, cleanup := newTestFDB()
	defer cleanup()

	cl := NewCommitlog(db, NewCommitlogOptions())
	require.NoError(t, cl.Open())

	clImpl := cl.(*commitlog)
	// Verify it starts at -1.
	require.Equal(t, int64(-1), clImpl.lastIdx)
	// Issue two writes sequentially so it will increase by 2 (+1 for each flush).
	require.NoError(t, cl.Write([]byte("some-data")))
	require.Equal(t, int64(0), clImpl.lastIdx)

	require.NoError(t, cl.Write([]byte("some-data")))
	require.Equal(t, int64(1), clImpl.lastIdx)

	require.NoError(t, cl.Close())

	// Ensure correct value is bootstrapped.
	cl = NewCommitlog(db, NewCommitlogOptions())
	require.NoError(t, cl.Open())
	require.Equal(t, int64(1), clImpl.lastIdx)
	require.NoError(t, cl.Close())
}
