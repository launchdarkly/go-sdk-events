package ldevents

import (
	"encoding/json"
	"testing"
	"time"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldtime"
	"gopkg.in/launchdarkly/go-sdk-common.v2/lduser"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"

	m "github.com/launchdarkly/go-test-helpers/v2/matchers"
	"gopkg.in/launchdarkly/go-jsonstream.v1/jwriter"

	"github.com/stretchr/testify/assert"
)

const testUserKey = "userKey"

var epDefaultConfig = EventsConfiguration{
	Capacity:              1000,
	FlushInterval:         1 * time.Hour,
	UserKeysCapacity:      1000,
	UserKeysFlushInterval: 1 * time.Hour,
}

var epDefaultUser = User(lduser.NewUserBuilder(testUserKey).Name("Red").Build())

var userJson = json.RawMessage(`{
	"key": "userKey",
	"name": "Red"
}`)
var filteredUserJson = json.RawMessage(`{
	"key": "userKey",
	"privateAttrs": ["name"]
}`)

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

	assertNextEventMatches(t, es, expectedIdentifyEvent(ie, userJson))
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

	assertNextEventMatches(t, es, expectedIdentifyEvent(ie, filteredUserJson))
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

	assertNextEventMatches(t, es, expectedIndexEvent(fe, userJson))
	assertNextEventMatches(t, es, summaryEventWithFlag(flag, summaryCounterProps(2, value, 1)))
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

	assertNextEventMatches(t, es, expectedIndexEvent(fe, userJson))
	assertNextEventMatches(t, es, expectedFeatureEvent(fe, flag, value, false, nil))
	assertNextEventMatches(t, es, summaryEventWithFlag(flag, summaryCounterProps(2, value, 1)))
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

	assertNextEventMatches(t, es, expectedIndexEvent(fe, filteredUserJson))
	assertNextEventMatches(t, es, expectedFeatureEvent(fe, flag, value, false, nil))
	assertNextEventMatches(t, es, summaryEventWithFlag(flag, summaryCounterProps(2, value, 1)))
	es.assertNoMoreEvents(t)
}

func TestUserDetailsAreScrubbedInDebugEvent(t *testing.T) {
	config := epDefaultConfig
	config.AllAttributesPrivate = true
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: ldtime.UnixMillisNow() + 1000000}
	value := ldvalue.String("value")
	fe := defaultEventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assertNextEventMatches(t, es, expectedIndexEvent(fe, filteredUserJson))
	assertNextEventMatches(t, es, expectedFeatureEvent(fe, flag, value, true, &filteredUserJson))
	assertNextEventMatches(t, es, summaryEventWithFlag(flag, summaryCounterProps(2, value, 1)))
	es.assertNoMoreEvents(t)
}

func TestFeatureEventCanContainReason(t *testing.T) {
	config := epDefaultConfig
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true}
	value := ldvalue.String("value")
	fe := defaultEventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(value, 2, noReason), ldvalue.Null(), "")
	fe.Reason = ldreason.NewEvalReasonFallthrough()
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assertNextEventMatches(t, es, expectedIndexEvent(fe, userJson))
	assertNextEventMatches(t, es, expectedFeatureEvent(fe, flag, value, false, nil))
	assertNextEventMatches(t, es, summaryEventWithFlag(flag, summaryCounterProps(2, value, 1)))
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

	assertNextEventMatches(t, es, expectedIndexEvent(fe, userJson))
	assertNextEventMatches(t, es, expectedFeatureEvent(fe, flag, value, true, &userJson))
	assertNextEventMatches(t, es, summaryEventWithFlag(flag, summaryCounterProps(2, value, 1)))
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

	assertNextEventMatches(t, es, expectedIndexEvent(fe, userJson))
	assertNextEventMatches(t, es, expectedFeatureEvent(fe, flag, value, false, nil))
	assertNextEventMatches(t, es, expectedFeatureEvent(fe, flag, value, true, &userJson))
	assertNextEventMatches(t, es, summaryEventWithFlag(flag, summaryCounterProps(2, value, 1)))
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
	assertNextEventMatches(t, es, expectedIdentifyEvent(ie, userJson))

	// Now send an event with debug mode on, with a "debug until" time that is further in
	// the future than the server time, but in the past compared to the client.
	debugUntil := serverTime + 1000
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: debugUntil}
	fe := eventFactory.NewEvalEvent(flag, epDefaultUser, ldreason.NewEvaluationDetail(ldvalue.Null(), 0, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	// should get a summary event only, not a debug event
	assertNextEventMatches(t, es, summaryEventWithFlag(flag, summaryCounterProps(0, ldvalue.Null(), 1)))
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
	assertNextEventMatches(t, es, expectedIdentifyEvent(ie, userJson))

	// Now send an event with debug mode on, with a "debug until" time that is further in
	// the future than the client time, but in the past compared to the server.
	debugUntil := serverTime - 1000
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: debugUntil}
	fe := eventFactory.NewEvalEvent(&flag, epDefaultUser, ldreason.NewEvaluationDetail(ldvalue.Null(), 0, noReason), ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	// should get a summary event only, not a debug event
	assertNextEventMatches(t, es, summaryEventWithFlag(flag, summaryCounterProps(0, ldvalue.Null(), 1)))
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

	assertNextEventMatches(t, es, expectedIndexEvent(fe1, userJson))
	assertNextEventMatches(t, es, expectedFeatureEvent(fe1, flag1, value, false, nil))
	assertNextEventMatches(t, es, expectedFeatureEvent(fe2, flag2, value, false, nil))
	assertNextEventMatches(t, es, m.AllOf(
		summaryEventWithFlag(flag1, summaryCounterProps(2, value, 1)),
		summaryEventWithFlag(flag2, summaryCounterProps(2, value, 1)),
	))
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

	assertNextEventMatches(t, es, expectedIndexEvent(fe1, userJson))

	assertNextEventMatches(t, es, m.AllOf(
		m.JSONProperty("startDate").Should(m.JSONEqual(fe1.CreationDate)), // using JSONEqual to ignore the specific Go numeric type
		m.JSONProperty("endDate").Should(m.JSONEqual(fe3.CreationDate)),
		summaryEventWithFlag(flag1, summaryCounterProps(2, value, 1)),
		summaryEventWithFlag(flag2, summaryCounterProps(3, value, 2)),
	))

	es.assertNoMoreEvents(t)
}

func TestCustomEventIsQueuedWithUser(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	data := ldvalue.ObjectBuild().Set("thing", ldvalue.String("stuff")).Build()
	ce := defaultEventFactory.NewCustomEvent("eventkey", epDefaultUser, data, false, 0)
	ep.RecordCustomEvent(ce)
	ep.Flush()

	assertNextEventMatches(t, es, expectedIndexEvent(ce, userJson))

	expected := m.JSONEqual(map[string]interface{}{
		"kind":         "custom",
		"creationDate": ce.CreationDate,
		"key":          ce.Key,
		"data":         data,
		"userKey":      epDefaultUser.GetKey(),
	})
	assertNextEventMatches(t, es, expected)

	es.assertNoMoreEvents(t)
}

func TestCustomEventCanHaveMetricValue(t *testing.T) {
	config := epDefaultConfig
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	data := ldvalue.ObjectBuild().Set("thing", ldvalue.String("stuff")).Build()
	metric := float64(2.5)
	ce := defaultEventFactory.NewCustomEvent("eventkey", epDefaultUser, data, true, metric)
	ep.RecordCustomEvent(ce)
	ep.Flush()

	assertNextEventMatches(t, es, expectedIndexEvent(ce, userJson))

	expected := m.JSONEqual(map[string]interface{}{
		"kind":         "custom",
		"creationDate": ce.CreationDate,
		"key":          ce.Key,
		"data":         data,
		"metricValue":  metric,
		"userKey":      epDefaultUser.GetKey(),
	})
	assertNextEventMatches(t, es, expected)
	es.assertNoMoreEvents(t)
}

func TestRawEventIsQueued(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	rawData := json.RawMessage(`{"kind":"alias","arbitrary":["we","don't","care","what's","in","here"]}`)
	ep.RecordRawEvent(rawData)
	ep.Flush()
	ep.waitUntilInactive()

	assertNextEventMatches(t, es, m.JSONEqual(rawData))
	es.assertNoMoreEvents(t)
}

func TestPeriodicFlush(t *testing.T) {
	config := epDefaultConfig
	config.FlushInterval = 10 * time.Millisecond
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	ie := defaultEventFactory.NewIdentifyEvent(epDefaultUser)
	ep.RecordIdentifyEvent(ie)

	assertNextEventMatches(t, es, expectedIdentifyEvent(ie, userJson))
	es.assertNoMoreEvents(t)
}

func TestClosingEventProcessorForcesSynchronousFlush(t *testing.T) {
	ep, es := createEventProcessorAndSender(epDefaultConfig)
	defer ep.Close()

	ie := defaultEventFactory.NewIdentifyEvent(epDefaultUser)
	ep.RecordIdentifyEvent(ie)
	ep.Close()

	assertNextEventMatches(t, es, expectedIdentifyEvent(ie, userJson))
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
	assertNextEventMatches(t, es, expectedIndexEvent(event1, userJson))
	assert.Equal(t, ldvalue.String("event1"), es.awaitEvent(t).GetByKey("key"))
	assert.Equal(t, ldvalue.String("event2"), es.awaitEvent(t).GetByKey("key"))

	// Now wait long enough for the user key cache to be flushed
	<-time.After(200 * time.Millisecond)

	// Referencing the same user in a new event should produce a new index event
	event3 := defaultEventFactory.NewCustomEvent("event3", epDefaultUser, ldvalue.Null(), false, 0)
	ep.RecordCustomEvent(event3)
	ep.Flush()
	assertNextEventMatches(t, es, expectedIndexEvent(event3, userJson))
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

	user := User(lduser.NewUser("userkey"))
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
	assertNextEventMatches(t, es,
		m.JSONProperty("user").Should(m.JSONProperty("key").Should(m.Equal(user1.GetKey()))))

	// Now a flush should succeed and send the current payload.
	senderGateCh <- struct{}{}
	ep.Flush()
	assertNextEventMatches(t, es,
		m.JSONProperty("user").Should(m.JSONProperty("key").Should(m.Equal(user2.GetKey()))))
	assertNextEventMatches(t, es,
		m.JSONProperty("user").Should(m.JSONProperty("key").Should(m.Equal(user3.GetKey()))))
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
	w := jwriter.NewWriter()
	filter.writeUser(&w, u)
	if err := w.Error(); err != nil {
		panic(err)
	}
	bytes := w.Bytes()
	var result ldvalue.Value
	if err := json.Unmarshal(bytes, &result); err != nil {
		panic(err)
	}
	return result
}

func expectedIdentifyEvent(sourceEvent Event, encodedUser interface{}) m.Matcher {
	return m.JSONEqual(map[string]interface{}{
		"kind":         "identify",
		"creationDate": sourceEvent.GetBase().CreationDate,
		"key":          sourceEvent.GetBase().User.GetKey(),
		"user":         encodedUser,
	})
}

func expectedIndexEvent(sourceEvent Event, encodedUser interface{}) m.Matcher {
	return m.JSONEqual(map[string]interface{}{
		"kind":         "index",
		"creationDate": sourceEvent.GetBase().CreationDate,
		"user":         encodedUser,
	})
}

func expectedFeatureEvent(sourceEvent FeatureRequestEvent, flag FlagEventProperties,
	value ldvalue.Value, debug bool, inlineUser interface{}) m.Matcher {
	props := map[string]interface{}{
		"kind":         "feature",
		"key":          flag.GetKey(),
		"creationDate": sourceEvent.GetBase().CreationDate,
		"version":      flag.GetVersion(),
		"value":        value,
		"default":      nil,
	}
	if debug {
		props["kind"] = "debug"
	}
	if sourceEvent.Variation.IsDefined() {
		props["variation"] = sourceEvent.Variation.IntValue()
	}
	if sourceEvent.Reason.GetKind() != "" {
		props["reason"] = jsonEncoding(sourceEvent.Reason)
	}
	if inlineUser == nil {
		props["userKey"] = sourceEvent.User.GetKey()
	} else {
		props["user"] = inlineUser
	}
	return m.JSONEqual(props)
}

func summaryEventWithFlag(flag flagEventPropertiesImpl, counterProps ...[]m.Matcher) m.Matcher {
	counters := make([]m.Matcher, 0, len(counterProps))
	for _, cp := range counterProps {
		counters = append(counters, m.AllOf(
			append(cp, m.JSONProperty("version").Should(m.Equal(flag.GetVersion())))...,
		))
	}
	return m.AllOf(
		m.JSONProperty("kind").Should(m.Equal("summary")),
		m.JSONProperty("features").Should(
			m.JSONProperty(flag.GetKey()).Should(
				m.JSONProperty("counters").Should(m.ItemsInAnyOrder(counters...)),
			),
		),
	)
}

func summaryCounterProps(variation int, value ldvalue.Value, count int) []m.Matcher {
	ms := []m.Matcher{
		m.JSONProperty("value").Should(m.JSONEqual(value)),
		m.JSONProperty("count").Should(m.Equal(count)),
	}
	if variation >= 0 {
		ms = append(ms, m.JSONProperty("variation").Should(m.Equal(variation)))
	} else {
		ms = append(ms, m.JSONOptProperty("variation").Should(m.BeNil()))
	}
	return ms
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

func assertNextEventMatches(t *testing.T, es *mockEventSender, jsonMatcher m.Matcher) {
	t.Helper()
	m.In(t).Assert(es.awaitEvent(t), jsonMatcher)
}
