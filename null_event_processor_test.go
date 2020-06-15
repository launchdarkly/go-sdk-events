package ldevents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNullEventProcessor(t *testing.T) {
	// Just verifies that these methods don't panic
	n := NewNullEventProcessor()
	n.SendEvent(defaultEventFactory.NewIdentifyEvent(epDefaultUser))
	n.Flush()
	require.NoError(t, n.Close())
}
