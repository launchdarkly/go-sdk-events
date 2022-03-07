package ldevents

import (
	"testing"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"

	"github.com/stretchr/testify/require"
)

func TestNullEventProcessor(t *testing.T) {
	// Just verifies that these methods don't panic
	n := NewNullEventProcessor()
	n.RecordFeatureRequestEvent(defaultEventFactory.NewUnknownFlagEvent("x", basicUser(), ldvalue.Null(),
		ldreason.EvaluationReason{}))
	n.RecordIdentifyEvent(defaultEventFactory.NewIdentifyEvent(basicUser()))
	n.RecordCustomEvent(defaultEventFactory.NewCustomEvent("x", basicUser(), ldvalue.Null(), false, 0))
	n.Flush()
	require.NoError(t, n.Close())
}
