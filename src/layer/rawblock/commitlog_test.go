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

func TestCommitlogTruncation(t *testing.T) {
	db, cleanup := newTestFDB()
	defer cleanup()

	cl := NewCommitlog(db, NewCommitlogOptions()).(*commitlog)
	require.NoError(t, cl.Open())

	// Verify it starts at -1.
	require.Equal(t, int64(-1), cl.lastIdx)
	// Issue two writes sequentially so it will increase by 2 (+1 for each flush).
	require.NoError(t, cl.Write([]byte("some-data")))
	require.Equal(t, int64(0), cl.lastIdx)
	require.NoError(t, cl.Write([]byte("some-data")))
	require.Equal(t, int64(1), cl.lastIdx)

	truncToken, err := cl.WaitForRotation()
	require.NoError(t, err)
	// Use the truncation token to truncate all commitlog chunks before 2.
	require.NoError(t, cl.Truncate(truncToken))
	require.NoError(t, cl.Close())

	// Ensure all commitlog chunks were cleared.
	cl = NewCommitlog(db, NewCommitlogOptions()).(*commitlog)
	require.NoError(t, cl.Open())
	require.Equal(t, int64(-1), cl.lastIdx)

	// Issue a write before waiting for rotation (this should be cleared by the
	// call to Truncate()).
	require.NoError(t, cl.Write([]byte("some-data")))
	require.Equal(t, int64(0), cl.lastIdx)

	truncToken, err = cl.WaitForRotation()
	require.NoError(t, err)
	// Issue one write after waiting for rotation so that there is one commitlog
	// chunk that should be deleted by truncation (0) and one that should remain(1).
	require.NoError(t, cl.Write([]byte("some-data")))
	require.Equal(t, int64(1), cl.lastIdx)
	require.NoError(t, cl.Truncate(truncToken))
	require.NoError(t, cl.Close())

	// Ensure that chunk 0 (written before WaitForRotation()) was cleared but chunk 1
	// (writen after WaitForRotation()) remains.
	cl = NewCommitlog(db, NewCommitlogOptions()).(*commitlog)
	require.NoError(t, cl.Open())
	require.Equal(t, int64(1), cl.lastIdx)
}
