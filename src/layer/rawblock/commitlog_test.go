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
	require.NoError(t, cl.Close())
}
