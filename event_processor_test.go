package ldevents

import (
	"encoding/json"
	"testing"
	"time"

	"gopkg.in/launchdarkly/go-sdk-common.v2/ldreason"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldtime"
	"gopkg.in/launchdarkly/go-sdk-common.v2/lduser"
	"gopkg.in/launchdarkly/go-sdk-common.v2/ldvalue"

	"github.com/launchdarkly/go-test-helpers/v2/jsonhelpers"
	m "github.com/launchdarkly/go-test-helpers/v2/matchers"
	"gopkg.in/launchdarkly/go-jsonstream.v1/jwriter"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withAndWithoutPrivateAttrs(t *testing.T, action func(*testing.T, EventsConfiguration)) {
	t.Run("without private attributes", func(t *testing.T) {
		action(t, basicConfigWithoutPrivateAttrs())
	})

	t.Run("with private attributes", func(t *testing.T) {
		config := basicConfigWithoutPrivateAttrs()
		config.AllAttributesPrivate = true
		action(t, config)
	})
}

func TestIdentifyEventProperties(t *testing.T) {
	withAndWithoutPrivateAttrs(t, func(t *testing.T, config EventsConfiguration) {
		ep, es := createEventProcessorAndSender(config)
		defer ep.Close()

		user := basicUser()
		ie := defaultEventFactory.NewIdentifyEvent(user)
		ep.RecordIdentifyEvent(ie)
		ep.Flush()
		ep.waitUntilInactive()

		assertEventsReceived(t, es, m.JSONEqual(map[string]interface{}{
			"kind":         "identify",
			"creationDate": ie.CreationDate,
			"key":          user.GetKey(),
			"user":         userJSON(user, config),
		}))
		es.assertNoMoreEvents(t)
	})
}

func TestFeatureEventIsSummarizedAndNotTrackedByDefault(t *testing.T) {
	withAndWithoutPrivateAttrs(t, func(t *testing.T, config EventsConfiguration) {
		ep, es := createEventProcessorAndSender(config)
		defer ep.Close()

		flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11}
		fe := defaultEventFactory.NewEvalEvent(flag, basicUser(), testEvalDetailWithoutReason, ldvalue.Null(), "")
		ep.RecordFeatureRequestEvent(fe)
		ep.Flush()

		assertEventsReceived(t, es,
			anyIndexEvent(),
			summaryEventWithFlag(flag, summaryCounterPropsFromEval(testEvalDetailWithoutReason, 1)),
		)
		es.assertNoMoreEvents(t)
	})
}

func TestIndividualFeatureEventIsQueuedWhenTrackEventsIsTrue(t *testing.T) {
	withAndWithoutPrivateAttrs(t, func(t *testing.T, config EventsConfiguration) {
		ep, es := createEventProcessorAndSender(config)
		defer ep.Close()

		flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true}
		fe := defaultEventFactory.NewEvalEvent(flag, basicUser(), testEvalDetailWithoutReason, ldvalue.Null(), "")
		ep.RecordFeatureRequestEvent(fe)
		ep.Flush()

		assertEventsReceived(t, es,
			anyIndexEvent(),
			featureEventWithAllProperties(fe, flag),
			// Here we also check that the summary count is still the same regardless of TrackEvents
			summaryEventWithFlag(flag,
				summaryCounterPropsFromEval(testEvalDetailWithoutReason, 1)),
		)
		es.assertNoMoreEvents(t)
	})
}

func TestIndexEventProperties(t *testing.T) {
	doTest := func(t *testing.T, prepareFn func(EventProcessor, EventUser) Event, subsequentEventMatchers ...m.Matcher) {
		withAndWithoutPrivateAttrs(t, func(t *testing.T, config EventsConfiguration) {
			ep, es := createEventProcessorAndSender(config)
			defer ep.Close()

			user := basicUser()

			event := prepareFn(ep, user)
			ep.Flush()

			allEventMatchers := append(
				[]m.Matcher{
					m.JSONEqual(map[string]interface{}{
						"kind":         "index",
						"creationDate": event.GetBase().CreationDate,
						"user":         userJSON(user, config),
					}),
				},
				subsequentEventMatchers...,
			)
			assertEventsReceived(t, es, allEventMatchers...)
			es.assertNoMoreEvents(t)
		})
	}

	t.Run("from feature event", func(t *testing.T) {
		flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true}
		doTest(t,
			func(ep EventProcessor, user EventUser) Event {
				fe := defaultEventFactory.NewEvalEvent(flag, user, testEvalDetailWithoutReason, ldvalue.Null(), "")
				ep.RecordFeatureRequestEvent(fe)
				return fe
			},
			anyFeatureEvent(),
			anySummaryEvent())
	})

	t.Run("from custom event", func(t *testing.T) {
		doTest(t,
			func(ep EventProcessor, user EventUser) Event {
				ce := defaultEventFactory.NewCustomEvent("eventkey", user, ldvalue.Null(), false, 0)
				ep.RecordCustomEvent(ce)
				return ce
			},
			anyCustomEvent())
	})
}

func TestDebugEventProperties(t *testing.T) {
	withAndWithoutPrivateAttrs(t, func(t *testing.T, config EventsConfiguration) {
		ep, es := createEventProcessorAndSender(config)
		defer ep.Close()

		user := basicUser()
		flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: ldtime.UnixMillisNow() + 1000000}
		fe := defaultEventFactory.NewEvalEvent(flag, user, testEvalDetailWithoutReason, ldvalue.Null(), "")
		ep.RecordFeatureRequestEvent(fe)
		ep.Flush()

		assertEventsReceived(t, es,
			anyIndexEvent(),
			debugEventWithAllProperties(fe, flag, userJSON(user, config)),
			anySummaryEvent(),
		)
		es.assertNoMoreEvents(t)
	})
}

func TestFeatureEventCanContainReason(t *testing.T) {
	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true}
	fe := defaultEventFactory.NewEvalEvent(flag, basicUser(), testEvalDetailWithoutReason, ldvalue.Null(), "")
	fe.Reason = ldreason.NewEvalReasonFallthrough()
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assertEventsReceived(t, es,
		anyIndexEvent(),
		featureEventWithAllProperties(fe, flag),
		anySummaryEvent(),
	)
	es.assertNoMoreEvents(t)
}

func TestDebugEventIsAddedIfFlagIsTemporarilyInDebugMode(t *testing.T) {
	fakeTimeNow := ldtime.UnixMillisecondTime(1000000)
	config := basicConfigWithoutPrivateAttrs()
	config.currentTimeProvider = func() ldtime.UnixMillisecondTime { return fakeTimeNow }
	eventFactory := NewEventFactory(false, config.currentTimeProvider)

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	user := basicUser()
	futureTime := fakeTimeNow + 100
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: futureTime}
	fe := eventFactory.NewEvalEvent(flag, user, testEvalDetailWithoutReason, ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assertEventsReceived(t, es,
		anyIndexEvent(),
		debugEventWithAllProperties(fe, flag, userJSON(user, config)),
		summaryEventWithFlag(flag, summaryCounterPropsFromEval(testEvalDetailWithoutReason, 1)),
	)
	es.assertNoMoreEvents(t)
}

func TestEventCanBeBothTrackedAndDebugged(t *testing.T) {
	fakeTimeNow := ldtime.UnixMillisecondTime(1000000)
	config := basicConfigWithoutPrivateAttrs()
	config.currentTimeProvider = func() ldtime.UnixMillisecondTime { return fakeTimeNow }
	eventFactory := NewEventFactory(false, config.currentTimeProvider)

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	user := basicUser()
	futureTime := fakeTimeNow + 100
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, TrackEvents: true, DebugEventsUntilDate: futureTime}
	fe := eventFactory.NewEvalEvent(flag, user, testEvalDetailWithoutReason, ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	assertEventsReceived(t, es,
		anyIndexEvent(),
		featureEventWithAllProperties(fe, flag),
		debugEventWithAllProperties(fe, flag, userJSON(user, config)),
		summaryEventWithFlag(flag, summaryCounterPropsFromEval(testEvalDetailWithoutReason, 1)),
	)
	es.assertNoMoreEvents(t)
}

func TestDebugModeExpiresBasedOnClientTimeIfClientTimeIsLater(t *testing.T) {
	fakeTimeNow := ldtime.UnixMillisecondTime(1000000)
	config := basicConfigWithoutPrivateAttrs()
	config.currentTimeProvider = func() ldtime.UnixMillisecondTime { return fakeTimeNow }
	eventFactory := NewEventFactory(false, config.currentTimeProvider)

	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	// Pick a server time that is somewhat behind the client time
	serverTime := fakeTimeNow - 20000
	es.result = EventSenderResult{Success: true, TimeFromServer: serverTime}

	// Send and flush an event we don't care about, just to set the last server time
	ie := eventFactory.NewIdentifyEvent(basicUser())
	ep.RecordIdentifyEvent(ie)
	ep.Flush()
	assertEventsReceived(t, es, anyIdentifyEvent())

	// Now send an event with debug mode on, with a "debug until" time that is further in
	// the future than the server time, but in the past compared to the client.
	debugUntil := serverTime + 1000
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: debugUntil}
	fe := eventFactory.NewEvalEvent(flag, basicUser(), testEvalDetailWithoutReason, ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	// should get a summary event only, not a debug event
	assertEventsReceived(t, es, anySummaryEvent())
	es.assertNoMoreEvents(t)
}

func TestDebugModeExpiresBasedOnServerTimeIfServerTimeIsLater(t *testing.T) {
	fakeTimeNow := ldtime.UnixMillisecondTime(1000000)
	config := basicConfigWithoutPrivateAttrs()
	config.currentTimeProvider = func() ldtime.UnixMillisecondTime { return fakeTimeNow }
	eventFactory := NewEventFactory(false, config.currentTimeProvider)

	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	// Pick a server time that is somewhat ahead of the client time
	serverTime := fakeTimeNow + 20000
	es.result = EventSenderResult{Success: true, TimeFromServer: serverTime}

	// Send and flush an event we don't care about, just to set the last server time
	ie := eventFactory.NewIdentifyEvent(basicUser())
	ep.RecordIdentifyEvent(ie)
	ep.Flush()
	assertEventsReceived(t, es, anyIdentifyEvent())

	// Now send an event with debug mode on, with a "debug until" time that is further in
	// the future than the client time, but in the past compared to the server.
	debugUntil := serverTime - 1000
	flag := flagEventPropertiesImpl{Key: "flagkey", Version: 11, DebugEventsUntilDate: debugUntil}
	fe := eventFactory.NewEvalEvent(&flag, basicUser(), testEvalDetailWithoutReason, ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe)
	ep.Flush()

	// should get a summary event only, not a debug event
	assertEventsReceived(t, es, anySummaryEvent())
	es.assertNoMoreEvents(t)
}

func TestTwoFeatureEventsForSameUserGenerateOnlyOneIndexEvent(t *testing.T) {
	withAndWithoutPrivateAttrs(t, func(t *testing.T, config EventsConfiguration) {
		ep, es := createEventProcessorAndSender(config)
		defer ep.Close()

		user := basicUser()
		flag1 := flagEventPropertiesImpl{Key: "flagkey1", Version: 11, TrackEvents: true}
		flag2 := flagEventPropertiesImpl{Key: "flagkey2", Version: 22, TrackEvents: true}
		fe1 := defaultEventFactory.NewEvalEvent(flag1, user, testEvalDetailWithoutReason, ldvalue.Null(), "")
		fe2 := defaultEventFactory.NewEvalEvent(flag2, user, testEvalDetailWithoutReason, ldvalue.Null(), "")
		ep.RecordFeatureRequestEvent(fe1)
		ep.RecordFeatureRequestEvent(fe2)
		ep.Flush()

		assertEventsReceived(t, es,
			indexEventForUserKey(user.GetKey()),
			featureEventWithAllProperties(fe1, flag1),
			featureEventWithAllProperties(fe2, flag2),
			anySummaryEvent(),
		)
		es.assertNoMoreEvents(t)
	})
}

func TestNonTrackedEventsAreSummarized(t *testing.T) {
	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	user := basicUser()
	flag1 := flagEventPropertiesImpl{Key: "flagkey1", Version: 11}
	flag2 := flagEventPropertiesImpl{Key: "flagkey2", Version: 22}
	flag1Eval := ldreason.NewEvaluationDetail(ldvalue.String("value1"), 2, noReason)
	flag2Eval := ldreason.NewEvaluationDetail(ldvalue.String("value2"), 3, noReason)
	fe1 := defaultEventFactory.NewEvalEvent(flag1, user, flag1Eval, ldvalue.Null(), "")
	fe2 := defaultEventFactory.NewEvalEvent(flag2, user, flag2Eval, ldvalue.Null(), "")
	fe3 := defaultEventFactory.NewEvalEvent(flag2, user, flag2Eval, ldvalue.Null(), "")
	ep.RecordFeatureRequestEvent(fe1)
	ep.RecordFeatureRequestEvent(fe2)
	ep.RecordFeatureRequestEvent(fe3)
	ep.Flush()

	assertEventsReceived(t, es, anyIndexEvent())

	assertEventsReceived(t, es, m.AllOf(
		m.JSONProperty("startDate").Should(equalNumericTime(fe1.CreationDate)),
		m.JSONProperty("endDate").Should(equalNumericTime(fe3.CreationDate)),
		summaryEventWithFlag(flag1, summaryCounterPropsFromEval(flag1Eval, 1)),
		summaryEventWithFlag(flag2, summaryCounterPropsFromEval(flag2Eval, 2)),
	))

	es.assertNoMoreEvents(t)
}

func TestCustomEventProperties(t *testing.T) {
	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	user := basicUser()
	data := ldvalue.ObjectBuild().Set("thing", ldvalue.String("stuff")).Build()
	ce := defaultEventFactory.NewCustomEvent("eventkey", user, data, false, 0)
	ep.RecordCustomEvent(ce)
	ep.Flush()

	customEventMatcher := m.JSONEqual(map[string]interface{}{
		"kind":         "custom",
		"creationDate": ce.CreationDate,
		"key":          ce.Key,
		"data":         data,
		"userKey":      user.GetKey(),
	})
	assertEventsReceived(t, es,
		anyIndexEvent(),
		customEventMatcher,
	)
	es.assertNoMoreEvents(t)
}

func TestCustomEventCanHaveMetricValue(t *testing.T) {
	config := basicConfigWithoutPrivateAttrs()
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	user := basicUser()
	data := ldvalue.ObjectBuild().Set("thing", ldvalue.String("stuff")).Build()
	metric := float64(2.5)
	ce := defaultEventFactory.NewCustomEvent("eventkey", user, data, true, metric)
	ep.RecordCustomEvent(ce)
	ep.Flush()

	customEventMatcher := m.JSONEqual(map[string]interface{}{
		"kind":         "custom",
		"creationDate": ce.CreationDate,
		"key":          ce.Key,
		"data":         data,
		"metricValue":  metric,
		"userKey":      user.GetKey(),
	})
	assertEventsReceived(t, es,
		anyIndexEvent(),
		customEventMatcher,
	)
	es.assertNoMoreEvents(t)
}

func TestRawEventIsQueued(t *testing.T) {
	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	rawData := json.RawMessage(`{"kind":"alias","arbitrary":["we","don't","care","what's","in","here"]}`)
	ep.RecordRawEvent(rawData)
	ep.Flush()
	ep.waitUntilInactive()

	assertEventsReceived(t, es, m.JSONEqual(rawData))
	es.assertNoMoreEvents(t)
}

func TestPeriodicFlush(t *testing.T) {
	config := basicConfigWithoutPrivateAttrs()
	config.FlushInterval = 10 * time.Millisecond
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	user := basicUser()
	ie := defaultEventFactory.NewIdentifyEvent(user)
	ep.RecordIdentifyEvent(ie)

	assertEventsReceived(t, es, identifyEventForUserKey(user.GetKey()))
	es.assertNoMoreEvents(t)
}

func TestClosingEventProcessorForcesSynchronousFlush(t *testing.T) {
	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	user := basicUser()
	ie := defaultEventFactory.NewIdentifyEvent(user)
	ep.RecordIdentifyEvent(ie)
	ep.Close()

	assertEventsReceived(t, es, identifyEventForUserKey(user.GetKey()))
	es.assertNoMoreEvents(t)
}

func TestPeriodicUserKeysFlush(t *testing.T) {
	// This test overrides the user key flush interval to a small value and verifies that a new
	// index event is generated for a user after the user keys have been flushed.
	config := basicConfigWithoutPrivateAttrs()
	config.UserKeysFlushInterval = time.Millisecond * 100
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	user := basicUser()
	event1 := defaultEventFactory.NewCustomEvent("event1", user, ldvalue.Null(), false, 0)
	event2 := defaultEventFactory.NewCustomEvent("event2", user, ldvalue.Null(), false, 0)
	ep.RecordCustomEvent(event1)
	ep.RecordCustomEvent(event2)
	ep.Flush()

	// We're relying on the user key flush not happening in between event1 and event2, so we should get
	// a single index event for the user.
	assertEventsReceived(t, es,
		indexEventForUserKey(user.GetKey()),
		customEventWithEventKey("event1"),
		customEventWithEventKey("event2"),
	)

	// Now wait long enough for the user key cache to be flushed
	<-time.After(200 * time.Millisecond)

	// Referencing the same user in a new event should produce a new index event
	event3 := defaultEventFactory.NewCustomEvent("event3", user, ldvalue.Null(), false, 0)
	ep.RecordCustomEvent(event3)
	ep.Flush()
	assertEventsReceived(t, es,
		indexEventForUserKey(user.GetKey()),
		customEventWithEventKey("event3"),
	)
	es.assertNoMoreEvents(t)
}

func TestNothingIsSentIfThereAreNoEvents(t *testing.T) {
	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	ep.Flush()
	ep.waitUntilInactive()

	es.assertNoMoreEvents(t)
}

func TestEventProcessorStopsSendingEventsAfterUnrecoverableError(t *testing.T) {
	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	es.result = EventSenderResult{MustShutDown: true}

	ie := defaultEventFactory.NewIdentifyEvent(basicUser())
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
	config := basicConfigWithoutPrivateAttrs()
	config.DiagnosticsManager = diagnosticsManager

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	event := es.awaitDiagnosticEvent(t)
	m.In(t).Assert(event, m.AllOf(
		eventKindIs("diagnostic-init"),
		m.JSONProperty("creationDate").Should(equalNumericTime(ldtime.UnixMillisFromTime(startTime))),
	))
	es.assertNoMoreDiagnosticEvents(t)
}

func TestDiagnosticPeriodicEventsAreSent(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	startTime := time.Now()
	diagnosticsManager := NewDiagnosticsManager(id, ldvalue.Null(), ldvalue.Null(), startTime, nil)
	config := basicConfigWithoutPrivateAttrs()
	config.DiagnosticsManager = diagnosticsManager
	config.forceDiagnosticRecordingInterval = 100 * time.Millisecond

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	// We use a channel for this because we can't predict exactly when the events will be sent
	initEvent := es.awaitDiagnosticEvent(t)
	m.In(t).Assert(initEvent, eventKindIs("diagnostic-init"))
	time0 := uint64(initEvent.GetByKey("creationDate").Float64Value())

	event1 := es.awaitDiagnosticEvent(t)
	m.In(t).Assert(event1, eventKindIs("diagnostic"))
	time1 := uint64(event1.GetByKey("creationDate").Float64Value())
	assert.True(t, time1-time0 >= 70, "event times should follow configured interval: %d, %d", time0, time1)

	event2 := es.awaitDiagnosticEvent(t)
	m.In(t).Assert(event2, eventKindIs("diagnostic"))
	time2 := uint64(event2.GetByKey("creationDate").Float64Value())
	assert.True(t, time2-time1 >= 70, "event times should follow configured interval: %d, %d", time1, time2)
}

func TestDiagnosticPeriodicEventHasEventCounters(t *testing.T) {
	id := NewDiagnosticID("sdkkey")
	config := basicConfigWithoutPrivateAttrs()
	config.Capacity = 3
	config.forceDiagnosticRecordingInterval = 100 * time.Millisecond
	periodicEventGate := make(chan struct{})

	diagnosticsManager := NewDiagnosticsManager(id, ldvalue.Null(), ldvalue.Null(), time.Now(), periodicEventGate)
	config.DiagnosticsManager = diagnosticsManager

	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	initEvent := es.awaitDiagnosticEvent(t)
	m.In(t).Assert(initEvent, eventKindIs("diagnostic-init"))

	user := User(lduser.NewUser("userkey"))
	ep.RecordCustomEvent(defaultEventFactory.NewCustomEvent("key", user, ldvalue.Null(), false, 0))
	ep.RecordCustomEvent(defaultEventFactory.NewCustomEvent("key", user, ldvalue.Null(), false, 0))
	ep.RecordCustomEvent(defaultEventFactory.NewCustomEvent("key", user, ldvalue.Null(), false, 0))
	ep.Flush()

	periodicEventGate <- struct{}{} // periodic event won't be sent until we do this

	event1 := es.awaitDiagnosticEvent(t)
	m.In(t).Assert(event1, m.AllOf(
		eventKindIs("diagnostic"),
		m.JSONProperty("eventsInLastBatch").Should(m.Equal(3)), // 1 index, 2 custom
		m.JSONProperty("droppedEvents").Should(m.Equal(1)),     // 3rd custom event was dropped
		m.JSONProperty("deduplicatedUsers").Should(m.Equal(2)),
	))

	periodicEventGate <- struct{}{}

	event2 := es.awaitDiagnosticEvent(t) // next periodic event - all counters should have been reset
	m.In(t).Assert(event2, m.AllOf(
		eventKindIs("diagnostic"),
		m.JSONProperty("eventsInLastBatch").Should(m.Equal(0)),
		m.JSONProperty("droppedEvents").Should(m.Equal(0)),
		m.JSONProperty("deduplicatedUsers").Should(m.Equal(0)),
	))
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

	ep, es := createEventProcessorAndSender(basicConfigWithoutPrivateAttrs())
	defer ep.Close()

	senderGateCh := make(chan struct{}, maxFlushWorkers)
	senderWaitingCh := make(chan struct{}, maxFlushWorkers)
	es.setGate(senderGateCh, senderWaitingCh)

	arbitraryUser := User(lduser.NewUser("other"))
	for i := 0; i < maxFlushWorkers; i++ {
		ep.RecordIdentifyEvent(defaultEventFactory.NewIdentifyEvent(arbitraryUser))
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
	assertEventsReceived(t, es, identifyEventForUserKey(user1.GetKey()))

	// Now a flush should succeed and send the current payload.
	senderGateCh <- struct{}{}
	ep.Flush()
	assertEventsReceived(t, es,
		identifyEventForUserKey(user2.GetKey()),
		identifyEventForUserKey(user3.GetKey()),
	)
	assert.Equal(t, maxFlushWorkers+2, es.getPayloadCount())
}

func userJSON(u EventUser, config EventsConfiguration) json.RawMessage {
	filter := newUserFilter(config)
	w := jwriter.NewWriter()
	filter.writeUser(&w, u)
	if err := w.Error(); err != nil {
		panic(err)
	}
	return w.Bytes()
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

func assertEventsReceived(t *testing.T, es *mockEventSender, matchers ...m.Matcher) {
	t.Helper()
	received := make([]ldvalue.Value, 0, len(matchers))
	for range matchers {
		if event, ok := es.tryAwaitEvent(); ok {
			received = append(received, event)
		} else {
			require.Fail(t, "timed out waiting for analytics event(s)", "wanted %d event(s); got: %s",
				len(matchers), jsonhelpers.ToJSONString(received))
		}
	}
	m.In(t).Assert(received, m.ItemsInAnyOrder(matchers...))
}
