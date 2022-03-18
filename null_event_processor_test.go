package ldevents

import (
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldreason"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"

	"github.com/stretchr/testify/require"
)

func TestNullEventProcessor(t *testing.T) {
	// Just verifies that these methods don't panic
	n := NewNullEventProcessor()
	n.RecordFeatureRequestEvent(defaultEventFactory.NewUnknownFlagEvent("x", basicContext(), ldvalue.Null(),
		ldreason.EvaluationReason{}))
	n.RecordIdentifyEvent(defaultEventFactory.NewIdentifyEvent(basicContext()))
	n.RecordCustomEvent(defaultEventFactory.NewCustomEvent("x", basicContext(), ldvalue.Null(), false, 0))
	n.Flush()
	require.NoError(t, n.Close())
}
