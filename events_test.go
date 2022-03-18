package ldevents

import (
	"encoding/json"
	"testing"

	"github.com/launchdarkly/go-sdk-common/v3/ldcontext"
	"github.com/launchdarkly/go-sdk-common/v3/ldreason"
	"github.com/launchdarkly/go-sdk-common/v3/ldtime"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"

	"github.com/stretchr/testify/assert"
)

var defaultEventFactory = NewEventFactory(false, nil)

var noReason = ldreason.EvaluationReason{}

func TestEventFactory(t *testing.T) {
	fakeTime := ldtime.UnixMillisecondTime(100000)
	timeFn := func() ldtime.UnixMillisecondTime { return fakeTime }
	withoutReasons := NewEventFactory(false, timeFn)
	withReasons := NewEventFactory(true, timeFn)
	context := Context(ldcontext.New("key"))

	t.Run("NewSuccessfulEvalEvent", func(t *testing.T) {
		flag := FlagEventProperties{Key: "flagkey", Version: 100}

		expected := FeatureRequestEvent{
			BaseEvent: BaseEvent{
				CreationDate: fakeTime,
				Context:      context,
			},
			Key:       flag.Key,
			Version:   ldvalue.NewOptionalInt(flag.Version),
			Variation: ldvalue.NewOptionalInt(1),
			Value:     ldvalue.String("value"),
			Default:   ldvalue.String("default"),
			Reason:    ldreason.NewEvalReasonFallthrough(),
			PrereqOf:  ldvalue.NewOptionalString("pre"),
		}

		event1 := withoutReasons.NewEvalEvent(flag, context,
			ldreason.NewEvaluationDetail(expected.Value, expected.Variation.IntValue(), expected.Reason),
			false, expected.Default, "pre")
		assert.Equal(t, ldreason.EvaluationReason{}, event1.Reason)
		event1.Reason = expected.Reason
		assert.Equal(t, expected, event1)

		event2 := withReasons.NewEvalEvent(flag, context,
			ldreason.NewEvaluationDetail(expected.Value, expected.Variation.IntValue(), expected.Reason),
			false, expected.Default, "pre")
		assert.Equal(t, expected, event2)
	})

	t.Run("NewSuccessfulEvalEvent with tracking/debugging", func(t *testing.T) {
		flag := FlagEventProperties{Key: "flagkey", Version: 100}

		expected := FeatureRequestEvent{
			BaseEvent: BaseEvent{
				CreationDate: fakeTime,
				Context:      context,
			},
			Key:       flag.Key,
			Version:   ldvalue.NewOptionalInt(flag.Version),
			Variation: ldvalue.NewOptionalInt(1),
			Value:     ldvalue.String("value"),
			Default:   ldvalue.String("default"),
		}

		flag1 := flag
		flag1.RequireFullEvent = true
		expected1 := expected
		expected1.RequireFullEvent = true
		event1 := withoutReasons.NewEvalEvent(flag1, context,
			ldreason.NewEvaluationDetail(expected.Value, expected.Variation.IntValue(), ldreason.NewEvalReasonFallthrough()),
			false, expected.Default, "")
		assert.Equal(t, expected1, event1)

		flag2 := flag
		flag2.DebugEventsUntilDate = ldtime.UnixMillisecondTime(200000)
		expected2 := expected
		expected2.DebugEventsUntilDate = flag2.DebugEventsUntilDate
		event2 := withoutReasons.NewEvalEvent(flag2, context,
			ldreason.NewEvaluationDetail(expected.Value, expected.Variation.IntValue(), ldreason.NewEvalReasonFallthrough()),
			false, expected.Default, "")
		assert.Equal(t, expected2, event2)
	})

	t.Run("NewSuccessfulEvalEvent with experimentation", func(t *testing.T) {
		flag := FlagEventProperties{Key: "flagkey", Version: 100}

		expected := FeatureRequestEvent{
			BaseEvent: BaseEvent{
				CreationDate: fakeTime,
				Context:      context,
			},
			Key:              flag.Key,
			Version:          ldvalue.NewOptionalInt(flag.Version),
			Variation:        ldvalue.NewOptionalInt(1),
			Value:            ldvalue.String("value"),
			Default:          ldvalue.String("default"),
			Reason:           ldreason.NewEvalReasonFallthrough(),
			RequireFullEvent: true,
		}

		event := withoutReasons.NewEvalEvent(flag, context,
			ldreason.NewEvaluationDetail(expected.Value, expected.Variation.IntValue(), ldreason.NewEvalReasonFallthrough()),
			true, expected.Default, "")
		assert.Equal(t, expected, event)
	})

	t.Run("NewUnknownFlagEvent", func(t *testing.T) {
		expected := FeatureRequestEvent{
			BaseEvent: BaseEvent{
				CreationDate: fakeTime,
				Context:      context,
			},
			Key:     "unknown-key",
			Value:   ldvalue.String("default"),
			Default: ldvalue.String("default"),
			Reason:  ldreason.NewEvalReasonFallthrough(),
		}

		event1 := withoutReasons.NewUnknownFlagEvent(expected.Key, context, expected.Default, expected.Reason)
		assert.Equal(t, ldreason.EvaluationReason{}, event1.Reason)
		event1.Reason = expected.Reason
		assert.Equal(t, expected, event1)
		assert.Equal(t, expected.BaseEvent.CreationDate, event1.GetCreationDate())

		event2 := withReasons.NewUnknownFlagEvent(expected.Key, context, expected.Default, expected.Reason)
		assert.Equal(t, expected, event2)
	})

	t.Run("NewCustomEvent", func(t *testing.T) {
		expected := CustomEvent{
			BaseEvent: BaseEvent{
				CreationDate: fakeTime,
				Context:      context,
			},
			Key:         "event-key",
			Data:        ldvalue.String("data"),
			HasMetric:   true,
			MetricValue: 2,
		}

		event := withoutReasons.NewCustomEvent(expected.Key, context, expected.Data, true, expected.MetricValue)
		assert.Equal(t, expected, event)
		assert.Equal(t, expected.BaseEvent.CreationDate, event.GetCreationDate())
	})

	t.Run("NewIdentifyEvent", func(t *testing.T) {
		expected := IdentifyEvent{
			BaseEvent: BaseEvent{
				CreationDate: fakeTime,
				Context:      context,
			},
		}

		event := withoutReasons.NewIdentifyEvent(context)
		assert.Equal(t, expected, event)
		assert.Equal(t, expected.BaseEvent.CreationDate, event.GetCreationDate())
	})
}

func TestPropertiesOfEventTypesNotFromFactory(t *testing.T) {
	fakeTime := ldtime.UnixMillisecondTime(100000)
	context := Context(ldcontext.New("key"))

	t.Run("indexEvent", func(t *testing.T) {
		ie := indexEvent{
			BaseEvent: BaseEvent{
				CreationDate: fakeTime,
				Context:      context,
			},
		}
		assert.Equal(t, ie.BaseEvent, ie.GetBase())
		assert.Equal(t, ie.BaseEvent.CreationDate, ie.GetCreationDate())
	})

	t.Run("rawEvent", func(t *testing.T) {
		re := rawEvent{json.RawMessage("{}")}
		assert.Equal(t, ldtime.UnixMillisecondTime(0), re.GetCreationDate())
	})
}
