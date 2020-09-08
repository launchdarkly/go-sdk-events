package ldevents

import (
	"encoding/json"
	"testing"
	"time"

	"gopkg.in/launchdarkly/go-sdk-common.v2/jsonstream"

	"github.com/stretchr/testify/assert"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldtime"
	"gopkg.in/launchdarkly/go-sdk-common.v2/lduser"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"
)

var epDefaultConfig = EventsConfiguration{
	Capacity:              1000,
	FlushInterval:         1 * time.Hour,
	UserKeysCapacity:      1000,
	UserKeysFlushInterval: 1 * time.Hour,
}

var epDefaultUser = User(lduser.NewUserBuilder("userKey").Name("Red").Build())

var userJson = ldvalue.ObjectBuild().
	Set("key", ldvalue.String("userKey")).
	Set("name", ldvalue.String("Red")).
	Build()
var filteredUserJson = ldvalue.ObjectBuild().
	Set("key", ldvalue.String("userKey")).
	Set("privateAttrs", ldvalue.ArrayOf(ldvalue.String("name"))).
	Build()

const (
	sdkKey = "SDK_KEY"
)

func TestIdentifyEventIsQueued(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	ie := defaultEventFactory.NewIdentifyEvent(epDefaultUser)
	ep.RecordIdentifyEvent(ie)
	ep.Flush()
	ep.waitUntilInactive()

	assert.Equal(t, expectedIdentifyEvent(ie, userJson), es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestUserDetailsAreScrubbedInIdentifyEvent(t *testing.T) {
	config := epDefaultConfig
	config.AllAttributesPrivate = true
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	ie := defaultEventFactory.NewIdentifyEvent(epDefaultUser)
	ep.RecordIdentifyEvent(ie)
	ep.Flush()

	assert.Equal(t, expectedIdentifyEvent(ie, filteredUserJson), es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestFeatureEventIsSummarizedAndNotTrackedByDefault(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11}
	value := ldvalue.String("value")
	fe := defaultEventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assert.Equal(t, expectedIndexEvent(fe, userJson), es.awaitEvent(t))
	assertSummaryEventHasCounter(t, flag, 2, value, 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestIndividualFeatureEventIsQueuedWhenTrackEventsIsTrue(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true}
	value := ldvalue.String("value")
	fe := defaultEventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assert.Equal(t, expectedIndexEvent(fe, userJson), es.awaitEvent(t))
	assert.Equal(t, expectedFeatureEvent(fe, flag, value, false, nil), es.awaitEvent(t))
	assertSummaryEventHasCounter(t, flag, 2, value, 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestUserDetailsAreScrubbedInIndexEvent(t *testing.T) {
	config := epDefaultConfig
	config.AllAttributesPrivate = true
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true}
	value := ldvalue.String("value")
	fe := defaultEventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assert.Equal(t, expectedIndexEvent(fe, filteredUserJson), es.awaitEvent(t))
	assert.Equal(t, expectedFeatureEvent(fe, flag, value, false, nil), es.awaitEvent(t))
	assertSummaryEventHasCounter(t, flag, 2, value, 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestFeatureEventCanContainInlineUser(t *testing.T) {
	config := epDefaultConfig
	config.InlineUsersInEvents = true
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true}
	value := ldvalue.String("value")
	fe := defaultEventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assert.Equal(t, expectedFeatureEvent(fe, flag, value, false, &userJson), es.awaitEvent(t))
	assertSummaryEventHasCounter(t, flag, 2, value, 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestUserDetailsAreScrubbedInFeatureEvent(t *testing.T) {
	config := epDefaultConfig
	config.InlineUsersInEvents = true
	config.AllAttributesPrivate = true
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true}
	value := ldvalue.String("value")
	fe := defaultEventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assert.Equal(t, expectedFeatureEvent(fe, flag, value, false, &filteredUserJson), es.awaitEvent(t))
	assertSummaryEventHasCounter(t, flag, 2, value, 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestFeatureEventCanContainReason(t *testing.T) {
	config := epDefaultConfig
	config.InlineUsersInEvents = true
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true}
	value := ldvalue.String("value")
	fe := defaultEventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	fe.Reason = ldreason.NewEvalReasonFallthrough()
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assert.Equal(t, expectedFeatureEvent(fe, flag, value, false, &userJson), es.awaitEvent(t))
	assertSummaryEventHasCounter(t, flag, 2, value, 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestIndexEventIsGeneratedForNonTrackedFeatureEventEvenIfInliningIsOn(t *testing.T) {
	config := epDefaultConfig
	config.InlineUsersInEvents = true
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11}
	value := ldvalue.String("value")
	fe := defaultEventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assert.Equal(t, expectedIndexEvent(fe, userJson), es.awaitEvent(t)) // we get this because we are *not* getting the full event
	assertSummaryEventHasCounter(t, flag, 2, value, 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestDebugEventIsAddedIfFlagIsTemporarilyInDebugMode(t *testing.T) {
	fakeTimeNow := ldtime.UnixMillisecondTime(1000000)
	config := epDefaultConfig
	config.currentTimeProvider = func() ldtime.UnixMillisecondTime { return fakeTimeNow }
	eventFactory := NewEventFactory(false, config.currentTimeProvider)

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	futureTime := fakeTimeNow + 100
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: futureTime}
	value := ldvalue.String("value")
	fe := eventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assert.Equal(t, expectedIndexEvent(fe, userJson), es.awaitEvent(t))
	assert.Equal(t, expectedFeatureEvent(fe, flag, value, true, &userJson), es.awaitEvent(t))
	assertSummaryEventHasCounter(t, flag, 2, value, 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestEventCanBeBothTrackedAndDebugged(t *testing.T) {
	fakeTimeNow := ldtime.UnixMillisecondTime(1000000)
	config := epDefaultConfig
	config.currentTimeProvider = func() ldtime.UnixMillisecondTime { return fakeTimeNow }
	eventFactory := NewEventFactory(false, config.currentTimeProvider)

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	futureTime := fakeTimeNow + 100
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true, DebugEventsUntilDate: futureTime}
	value := ldvalue.String("value")
	fe := eventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assert.Equal(t, expectedIndexEvent(fe, userJson), es.awaitEvent(t))
	assert.Equal(t, expectedFeatureEvent(fe, flag, value, false, nil), es.awaitEvent(t))
	assert.Equal(t, expectedFeatureEvent(fe, flag, value, true, &userJson), es.awaitEvent(t))
	assertSummaryEventHasCounter(t, flag, 2, value, 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestDebugModeExpiresBasedOnClientTimeIfClientTimeIsLater(t *testing.T) {
	fakeTimeNow := ldtime.UnixMillisecondTime(1000000)
	config := epDefaultConfig
	config.currentTimeProvider = func() ldtime.UnixMillisecondTime { return fakeTimeNow }
	eventFactory := NewEventFactory(false, config.currentTimeProvider)

	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	// Pick a server time that is somewhat behind the client time
	serverTime := fakeTimeNow - 20000
	es.result = EventSenderResult{Success: true, TimeFromServer: serverTime}

	// Send and flush an event we don't care about, just to set the last server time
	ie := eventFactory.NewIdentifyEvent(epDefaultUser)
	ep.RecordIdentifyEvent(ie)
	ep.Flush()
	assert.Equal(t, expectedIdentifyEvent(ie, userJson), es.awaitEvent(t))

	// Now send an event with debug mode on, with a "debug until" time that is further in
	// the future than the server time, but in the past compared to the client.
	debugUntil := serverTime + 1000
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: debugUntil}
	fe := eventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(ldvalue.Null(), 0, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	// should get a summary event only, not a debug event
	assertSummaryEventHasCounter(t, flag, 0, ldvalue.Null(), 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestDebugModeExpiresBasedOnServerTimeIfServerTimeIsLater(t *testing.T) {
	fakeTimeNow := ldtime.UnixMillisecondTime(1000000)
	config := epDefaultConfig
	config.currentTimeProvider = func() ldtime.UnixMillisecondTime { return fakeTimeNow }
	eventFactory := NewEventFactory(false, config.currentTimeProvider)

	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	// Pick a server time that is somewhat ahead of the client time
	serverTime := fakeTimeNow + 20000
	es.result = EventSenderResult{Success: true, TimeFromServer: serverTime}

	// Send and flush an event we don't care about, just to set the last server time
	ie := eventFactory.NewIdentifyEvent(epDefaultUser)
	ep.RecordIdentifyEvent(ie)
	ep.Flush()
	assert.Equal(t, expectedIdentifyEvent(ie, userJson), es.awaitEvent(t))

	// Now send an event with debug mode on, with a "debug until" time that is further in
	// the future than the client time, but in the past compared to the server.
	debugUntil := serverTime - 1000
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: debugUntil}
	fe := eventFactory.NewEvalEvent(&flag, epDefaultUser, ldreason.NewEvaluationDetail(ldvalue.Null(), 0, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	// should get a summary event only, not a debug event
	assertSummaryEventHasCounter(t, flag, 0, ldvalue.Null(), 1, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestTwoFeatureEventsForSameUserGenerateOnlyOneIndexEvent(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	flag1 := flagEventPropertiesImpl{Key: "flagkey1", Version: 11, TrackEvents: true}
	flag2 := flagEventPropertiesImpl{Key: "flagkey2", Version: 22, TrackEvents: true}
	value := ldvalue.String("value")
	fe1 := defaultEventFactory.NewEvalEvent(flag1, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	fe2 := defaultEventFactory.NewEvalEvent(flag2, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe1)
	ep.RecordFeatureRequestEvent(fe2)
	ep.Flush()

	assert.Equal(t, expectedIndexEvent(fe1, userJson), es.awaitEvent(t))
	assert.Equal(t, expectedFeatureEvent(fe1, flag1, value, false, nil), es.awaitEvent(t))
	assert.Equal(t, expectedFeatureEvent(fe2, flag2, value, false, nil), es.awaitEvent(t))
	se := es.awaitEvent(t)
	assertSummaryEventHasCounter(t, flag1, 2, value, 1, se)
	assertSummaryEventHasCounter(t, flag2, 2, value, 1, se)
	es.assertNoMoreEvents(t)
}

func TestNonTrackedEventsAreSummarized(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	flag1 := flagEventPropertiesImpl{Key: "flagkey1", Version: 11}
	flag2 := flagEventPropertiesImpl{Key: "flagkey2", Version: 22}
	value := ldvalue.String("value")
	fe1 := defaultEventFactory.NewEvalEvent(flag1, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	fe2 := defaultEventFactory.NewEvalEvent(flag2, epDefaultUser, ldreason.NewEvaluationDetail(value, 3, noReason), ldvalue.Null(), "")
	fe3 := defaultEventFactory.NewEvalEvent(flag2, epDefaultUser, ldreason.NewEvaluationDetail(value, 3, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe1)
	ep.RecordFeatureRequestEvent(fe2)
	ep.RecordFeatureRequestEvent(fe3)
	ep.Flush()

	assert.Equal(t, expectedIndexEvent(fe1, userJson), es.awaitEvent(t))

	se := es.awaitEvent(t)
	assertSummaryEventHasCounter(t, flag1, 2, value, 1, se)
	assertSummaryEventHasCounter(t, flag2, 3, value, 2, se)
	assert.Equal(t, float64(fe1.CreationDate), se.GetByKey("startDate").Float64Value())
	assert.Equal(t, float64(fe3.CreationDate), se.GetByKey("endDate").Float64Value())

	es.assertNoMoreEvents(t)
}

func TestCustomEventIsQueuedWithUser(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	data := ldvalue.ObjectBuild().Set("thing", ldvalue.String("stuff")).Build()
	ce := defaultEventFactory.NewCustomEvent("eventkey", epDefaultUser, data, false, 0)
	ep.RecordCustomEvent(ce)
	ep.Flush()

	assert.Equal(t, expectedIndexEvent(ce, userJson), es.awaitEvent(t))

	expected := ldvalue.ObjectBuild().
		Set("kind", ldvalue.String("custom")).
		Set("creationDate", ldvalue.Float64(float64(ce.CreationDate))).
		Set("key", ldvalue.String(ce.Key)).
		Set("data", data).
		Set("userKey", ldvalue.String(epDefaultUser.GetKey())).
		Build()
	assert.Equal(t, expected, es.awaitEvent(t))

	es.assertNoMoreEvents(t)
}

func TestCustomEventCanContainInlineUser(t *testing.T) {
	config := epDefaultConfig
	config.InlineUsersInEvents = true
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	data := ldvalue.ObjectBuild().Set("thing", ldvalue.String("stuff")).Build()
	ce := defaultEventFactory.NewCustomEvent("eventkey", epDefaultUser, data, false, 0)
	ep.RecordCustomEvent(ce)
	ep.Flush()

	expected := ldvalue.ObjectBuild().
		Set("kind", ldvalue.String("custom")).
		Set("creationDate", ldvalue.Float64(float64(ce.CreationDate))).
		Set("key", ldvalue.String(ce.Key)).
		Set("data", data).
		Set("user", userJsonEncoding(epDefaultUser)).
		Build()
	assert.Equal(t, expected, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestCustomEventCanHaveMetricValue(t *testing.T) {
	config := epDefaultConfig
	config.InlineUsersInEvents = true
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	data := ldvalue.ObjectBuild().Set("thing", ldvalue.String("stuff")).Build()
	metric := float64(2.5)
	ce := defaultEventFactory.NewCustomEvent("eventkey", epDefaultUser, data, true, metric)
	ep.RecordCustomEvent(ce)
	ep.Flush()

	expected := ldvalue.ObjectBuild().
		Set("kind", ldvalue.String("custom")).
		Set("creationDate", ldvalue.Float64(float64(ce.CreationDate))).
		Set("key", ldvalue.String(ce.Key)).
		Set("data", data).
		Set("metricValue", ldvalue.Float64(metric)).
		Set("user", userJsonEncoding(epDefaultUser)).
		Build()
	assert.Equal(t, expected, es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestPeriodicFlush(t *testing.T) {
	config := epDefaultConfig
	config.FlushInterval = 10 * time.Millisecond
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	ie := defaultEventFactory.NewIdentifyEvent(epDefaultUser)
	ep.RecordIdentifyEvent(ie)

	assert.Equal(t, expectedIdentifyEvent(ie, userJson), es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestClosingEventProcessorForcesSynchronousFlush(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	ie := defaultEventFactory.NewIdentifyEvent(epDefaultUser)
	ep.RecordIdentifyEvent(ie)
	ep.Close()

	assert.Equal(t, expectedIdentifyEvent(ie, userJson), es.awaitEvent(t))
	es.assertNoMoreEvents(t)
}

func TestPeriodicUserKeysFlush(t *testing.T) {
	// This test overrides the user key flush interval to a small value and verifies that a new
	// index event is generated for a user after the user keys have been flushed.
	config := epDefaultConfig
	config.UserKeysFlushInterval = time.Millisecond * 100
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	event1 := defaultEventFactory.NewCustomEvent("event1", epDefaultUser, ldvalue.Null(), false, 0)
	event2 := defaultEventFactory.NewCustomEvent("event2", epDefaultUser, ldvalue.Null(), false, 0)
	ep.RecordCustomEvent(event1)
	ep.RecordCustomEvent(event2)
	ep.Flush()

	// We're relying on the user key flush not happening in between event1 and event2, so we should get
	// a single index event for the user.
	assert.Equal(t, expectedIndexEvent(event1, userJson), es.awaitEvent(t))
	assert.Equal(t, ldvalue.String("event1"), es.awaitEvent(t).GetByKey("key"))
	assert.Equal(t, ldvalue.String("event2"), es.awaitEvent(t).GetByKey("key"))

	// Now wait long enough for the user key cache to be flushed
	<-time.After(200 * time.Millisecond)

	// Referencing the same user in a new event should produce a new index event
	event3 := defaultEventFactory.NewCustomEvent("event3", epDefaultUser, ldvalue.Null(), false, 0)
	ep.RecordCustomEvent(event3)
	ep.Flush()
	assert.Equal(t, expectedIndexEvent(event3, userJson), es.awaitEvent(t))
	assert.Equal(t, ldvalue.String("event3"), es.awaitEvent(t).GetByKey("key"))
}

func TestNothingIsSentIfThereAreNoEvents(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	ep.Flush()
	ep.waitUntilInactive()

	es.assertNoMoreEvents(t)
}

func TestEventProcessorStopsSendingEventsAfterUnrecoverableError(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	es.result = EventSenderResult{MustShutDown: true}

	ie := defaultEventFactory.NewIdentifyEvent(epDefaultUser)
	ep.RecordIdentifyEvent(ie)
	ep.Flush()
	es.awaitEvent(t)

	ep.RecordIdentifyEvent(ie)
	ep.Flush()
	ep.waitUntilInactive()

	es.assertNoMoreEvents(t)
}

func TestDiagnosticInitEventIsSent(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	startTime := time.Now()
	diagnosticsManager := NewDiagnosticsManager(id, ldvalue.Null(), ldvalue.Null(), startTime, nil)
	config := epDefaultConfig
	config.DiagnosticsManager = diagnosticsManager

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	event := es.awaitDiagnosticEvent(t)
	assert.Equal(t, "diagnostic-init", event.GetByKey("kind").StringValue())
	assert.Equal(t, float64(ldtime.UnixMillisFromTime(startTime)), event.GetByKey("creationDate").Float64Value())
	es.assertNoMoreDiagnosticEvents(t)
}

func TestDiagnosticPeriodicEventsAreSent(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	startTime := time.Now()
	diagnosticsManager := NewDiagnosticsManager(id, ldvalue.Null(), ldvalue.Null(), startTime, nil)
	config := epDefaultConfig
	config.DiagnosticsManager = diagnosticsManager
	config.forceDiagnosticRecordingInterval = 100 * time.Millisecond

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	// We use a channel for this because we can't predict exactly when the events will be sent
	initEvent := es.awaitDiagnosticEvent(t)
	assert.Equal(t, "diagnostic-init", initEvent.GetByKey("kind").StringValue())
	time0 := uint64(initEvent.GetByKey("creationDate").Float64Value())

	event1 := es.awaitDiagnosticEvent(t)
	assert.Equal(t, "diagnostic", event1.GetByKey("kind").StringValue())
	time1 := uint64(event1.GetByKey("creationDate").Float64Value())
	assert.True(t, time1-time0 >= 70, "event times should follow configured interval: %d, %d", time0, time1)

	event2 := es.awaitDiagnosticEvent(t)
	assert.Equal(t, "diagnostic", event2.GetByKey("kind").StringValue())
	time2 := uint64(event2.GetByKey("creationDate").Float64Value())
	assert.True(t, time2-time1 >= 70, "event times should follow configured interval: %d, %d", time1, time2)
}

func TestDiagnosticPeriodicEventHasEventCounters(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	config := epDefaultConfig
	config.Capacity = 3
	config.forceDiagnosticRecordingInterval = 100 * time.Millisecond
	periodicEventGate := make(chan struct{})

	diagnosticsManager := NewDiagnosticsManager(id, ldvalue.Null(), ldvalue.Null(), time.Now(), periodicEventGate)
	config.DiagnosticsManager = diagnosticsManager

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	initEvent := es.awaitDiagnosticEvent(t)
	assert.Equal(t, "diagnostic-init", initEvent.GetByKey("kind").StringValue())

	user := EventUser{lduser.NewUser("userkey"), nil}
	ep.RecordCustomEvent(defaultEventFactory.NewCustomEvent("key", user, ldvalue.Null(), false, 0))
	ep.RecordCustomEvent(defaultEventFactory.NewCustomEvent("key", user, ldvalue.Null(), false, 0))
	ep.RecordCustomEvent(defaultEventFactory.NewCustomEvent("key", user, ldvalue.Null(), false, 0))
	ep.Flush()

	periodicEventGate <- struct{}{} // periodic event won't be sent until we do this

	event1 := es.awaitDiagnosticEvent(t)
	assert.Equal(t, "diagnostic", event1.GetByKey("kind").StringValue())
	assert.Equal(t, 3, event1.GetByKey("eventsInLastBatch").IntValue()) // 1 index, 2 custom
	assert.Equal(t, 1, event1.GetByKey("droppedEvents").IntValue())     // 3rd custom event was dropped
	assert.Equal(t, 2, event1.GetByKey("deduplicatedUsers").IntValue())

	periodicEventGate <- struct{}{}

	event2 := es.awaitDiagnosticEvent(t) // next periodic event - all counters should have been reset
	assert.Equal(t, "diagnostic", event2.GetByKey("kind").StringValue())
	assert.Equal(t, 0, event2.GetByKey("eventsInLastBatch").IntValue())
	assert.Equal(t, 0, event2.GetByKey("droppedEvents").IntValue())
	assert.Equal(t, 0, event2.GetByKey("deduplicatedUsers").IntValue())
}

func TestEventsAreKeptInBufferIfAllFlushWorkersAreBusy(t *testing.T) {
	// Note that in the current implementation, although the intention was that we would cancel a flush
	// if there's not an available flush worker, instead what happens is that we will queue *one* flush
	// in that case, and then cancel the *next* flush if the workers are still busy. This is because the
	// flush payload channel has a buffer size of 1, rather than zero. The test below verifies the
	// current behavior.

	user1 := User(lduser.NewUser("user1"))
	user2 := User(lduser.NewUser("user2"))
	user3 := User(lduser.NewUser("user3"))

	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	senderGateCh := make(chan struct{}, maxFlushWorkers)
	senderWaitingCh := make(chan struct{}, maxFlushWorkers)
	es.setGate(senderGateCh, senderWaitingCh)

	for i := 0; i < maxFlushWorkers; i++ {
		ep.RecordIdentifyEvent(defaultEventFactory.NewIdentifyEvent(epDefaultUser))
		ep.Flush()
		_ = es.awaitEvent(t) // we don't need to see this payload, just throw it away
	}

	// Each of the worker goroutines should now be blocked waiting for senderGateCh. We can tell when
	// they have all gotten to that point because they have posted to senderReadyCh.
	for i := 0; i < maxFlushWorkers; i++ {
		<-senderWaitingCh
	}
	es.assertNoMoreEvents(t)
	assert.Equal(t, maxFlushWorkers, es.getPayloadCount())

	// Now, put an event in the buffer and try to flush again. In the current implementation (see
	// above) this payload gets queued in a holding area, and will be flushed after a worker
	// becomes free.
	extraEvent1 := defaultEventFactory.NewIdentifyEvent(user1)
	ep.RecordIdentifyEvent(extraEvent1)
	ep.Flush()

	// Do an additional flush with another event. This time, the event processor should see that there's
	// no space available and simply ignore the flush request. There's no way to verify programmatically
	// that this has happened, so just give it a short delay.
	extraEvent2 := defaultEventFactory.NewIdentifyEvent(user2)
	ep.RecordIdentifyEvent(extraEvent2)
	ep.Flush()
	<-time.After(100 * time.Millisecond)
	es.assertNoMoreEvents(t)

	// Enqueue a third event. The current payload should now be extraEvent2 + extraEvent3.
	extraEvent3 := defaultEventFactory.NewIdentifyEvent(user3)
	ep.RecordIdentifyEvent(extraEvent3)

	// Now allow the workers to unblock.
	for i := 0; i < maxFlushWorkers; i++ {
		senderGateCh <- struct{}{}
	}

	// The first unblocked worker should pick up the queued payload with event1.
	senderGateCh <- struct{}{}
	received1 := es.awaitEvent(t)
	assert.Equal(t, user1.GetKey(), received1.GetByKey("key").StringValue())

	// Now a flush should succeed and send the current payload.
	senderGateCh <- struct{}{}
	ep.Flush()
	received2 := es.awaitEvent(t)
	received3 := es.awaitEvent(t)
	assert.Equal(t, user2.GetKey(), received2.GetByKey("key").StringValue())
	assert.Equal(t, user3.GetKey(), received3.GetByKey("key").StringValue())
	assert.Equal(t, maxFlushWorkers+2, es.getPayloadCount())
}

func jsonEncoding(o interface{}) ldvalue.Value {
	bytes, _ := json.Marshal(o)
	var result ldvalue.Value
	if err := json.Unmarshal(bytes, &result); err != nil {
		panic(err)
	}
	return result
}

func userJsonEncoding(u EventUser) ldvalue.Value {
	filter := newUserFilter(epDefaultConfig)
	var b jsonstream.JSONBuffer
	filter.writeUser(&b, u)
	bytes, err := b.Get()
	if err != nil {
		panic(err)
	}
	var result ldvalue.Value
	if err := json.Unmarshal(bytes, &result); err != nil {
		panic(err)
	}
	return result
}

func expectedIdentifyEvent(sourceEvent Event, encodedUser ldvalue.Value) ldvalue.Value {
	return ldvalue.ObjectBuild().
		Set("kind", ldvalue.String("identify")).
		Set("key", ldvalue.String(sourceEvent.GetBase().User.GetKey())).
		Set("creationDate", ldvalue.Float64(float64(sourceEvent.GetBase().CreationDate))).
		Set("user", encodedUser).
		Build()
}

func expectedIndexEvent(sourceEvent Event, encodedUser ldvalue.Value) ldvalue.Value {
	return ldvalue.ObjectBuild().
		Set("kind", ldvalue.String("index")).
		Set("creationDate", ldvalue.Float64(float64(sourceEvent.GetBase().CreationDate))).
		Set("user", encodedUser).
		Build()
}

func expectedFeatureEvent(sourceEvent FeatureRequestEvent, flag FlagEventProperties,
	value ldvalue.Value, debug bool, inlineUser *ldvalue.Value) ldvalue.Value {
	kind := "feature"
	if debug {
		kind = "debug"
	}
	expected := ldvalue.ObjectBuild().
		Set("kind", ldvalue.String(kind)).
		Set("key", ldvalue.String(flag.GetKey())).
		Set("creationDate", ldvalue.Float64(float64(sourceEvent.GetBase().CreationDate))).
		Set("version", ldvalue.Int(flag.GetVersion())).
		Set("value", value).
		Set("default", ldvalue.Null())
	if sourceEvent.Variation.IsDefined() {
		expected.Set("variation", ldvalue.Int(sourceEvent.Variation.IntValue()))
	}
	if sourceEvent.Reason.GetKind() != "" {
		expected.Set("reason", jsonEncoding(sourceEvent.Reason))
	}
	if inlineUser == nil {
		expected.Set("userKey", ldvalue.String(sourceEvent.User.GetKey()))
	} else {
		expected.Set("user", *inlineUser)
	}
	return expected.Build()
}

func assertSummaryEventHasFlag(t *testing.T, flag FlagEventProperties, output ldvalue.Value) bool {
	if assert.Equal(t, "summary", output.GetByKey("kind").StringValue()) {
		flags := output.GetByKey("features")
		return !flags.GetByKey(flag.GetKey()).IsNull()
	}
	return false
}

func assertSummaryEventHasCounter(t *testing.T, flag flagEventPropertiesImpl, variation int, value ldvalue.Value, count int, output ldvalue.Value) {
	if assertSummaryEventHasFlag(t, flag, output) {
		f := output.GetByKey("features").GetByKey(flag.GetKey())
		assert.Equal(t, ldvalue.ObjectType, f.Type())
		expected := ldvalue.ObjectBuild().Set("value", value).Set("count", ldvalue.Int(count)).Set("version", ldvalue.Int(flag.GetVersion()))
		if variation >= 0 {
			expected.Set("variation", ldvalue.Int(variation))
		}
		counters := []ldvalue.Value{}
		f.GetByKey("counters").Enumerate(func(i int, k string, v ldvalue.Value) bool {
			counters = append(counters, v)
			return true
		})
		assert.Contains(t, counters, expected.Build())
	}
}

// used only for testing - ensures that all pending messages and flushes have completed
func (ep *defaultEventProcessor) waitUntilInactive() {
	m := syncEventsMessage{replyCh: make(chan struct{})}
	ep.inboxCh <- m
	<-m.replyCh // Now we know that all events prior to this call have been processed
}

func createEventProcessorAndSender(config EventsConfiguration) (*defaultEventProcessor, *mockEventSender) {
	sender := newMockEventSender()
	config.EventSender = sender
	ep := NewDefaultEventProcessor(config)
	return ep.(*defaultEventProcessor), sender
}
