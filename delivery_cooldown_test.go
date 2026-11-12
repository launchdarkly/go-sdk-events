package ldevents

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/launchdarkly/go-sdk-common/v3/ldlog"
	"github.com/launchdarkly/go-sdk-common/v3/ldlogtest"
	"github.com/launchdarkly/go-sdk-common/v3/ldtime"
	"github.com/launchdarkly/go-sdk-common/v3/ldvalue"

	m "github.com/launchdarkly/go-test-helpers/v3/matchers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCooldownClock is a mutable clock for cooldown tests. The dispatcher goroutine reads it while
// the test goroutine advances it, so access is locked.
type fakeCooldownClock struct {
	lock sync.Mutex
	now  time.Time
}

func newFakeCooldownClock() *fakeCooldownClock {
	return &fakeCooldownClock{now: time.Unix(100000, 0)}
}

func (c *fakeCooldownClock) nowFn() time.Time {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.now
}

func (c *fakeCooldownClock) advance(d time.Duration) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.now = c.now.Add(d)
}

func newTrackerForTest(clock *fakeCooldownClock) *deliveryCooldownTracker {
	return newDeliveryCooldownTracker(clock.nowFn)
}

// failPastWindow records an unexpected failure and then advances the clock past the window it opened.
func failPastWindow(t *testing.T, tracker *deliveryCooldownTracker, clock *fakeCooldownClock) time.Duration {
	window, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	clock.advance(window + time.Second)
	return window
}

func TestCooldownTrackerStartsOutsideCooldown(t *testing.T) {
	tracker := newTrackerForTest(newFakeCooldownClock())
	assert.False(t, tracker.inCooldown())
	assert.Equal(t, time.Duration(0), tracker.remaining())
}

func TestCooldownTrackerWindowDoublesPerFailedProbeUpToCeiling(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	expected := []time.Duration{
		5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 40 * time.Minute,
		time.Hour, time.Hour, // clamped at the ceiling
	}
	for _, want := range expected {
		assert.Equal(t, want, failPastWindow(t, tracker, clock))
	}
}

func TestCooldownTrackerInWindowFailureDoesNotLengthenWindow(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	preWindowToken := tracker.beginDelivery()
	window, opened := tracker.recordUnexpectedFailure(preWindowToken)
	require.True(t, opened)
	require.Equal(t, 5*time.Minute, window)

	// A result arriving while the window is open is from a delivery that was already in flight;
	// its token is stale, so it does not extend the window or advance the doubling sequence.
	clock.advance(time.Minute)
	_, openedAgain := tracker.recordUnexpectedFailure(preWindowToken)
	assert.False(t, openedAgain)
	assert.Equal(t, 4*time.Minute, tracker.remaining())

	// The next failed probe doubles once, to 10 minutes, not twice.
	clock.advance(4*time.Minute + time.Second)
	window, opened = tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	assert.Equal(t, 10*time.Minute, window)
}

func TestCooldownTrackerResetsAfterTwoConsecutiveSuccesses(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	failPastWindow(t, tracker, clock)
	tracker.recordSuccess(tracker.beginDelivery())
	tracker.recordSuccess(tracker.beginDelivery())

	// The retry state has reset, so a new unexpected failure starts over at the initial delay.
	window, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	assert.Equal(t, 5*time.Minute, window)
}

func TestCooldownTrackerSingleSuccessDoesNotReset(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	failPastWindow(t, tracker, clock)
	tracker.recordSuccess(tracker.beginDelivery())

	window, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	assert.Equal(t, 10*time.Minute, window)
}

func TestCooldownTrackerOrdinaryFailureBreaksSuccessStreakWithoutOpeningWindow(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	tracker.recordFailure(tracker.beginDelivery())
	assert.False(t, tracker.inCooldown())

	failPastWindow(t, tracker, clock)
	tracker.recordSuccess(tracker.beginDelivery())
	tracker.recordFailure(tracker.beginDelivery()) // breaks the streak; the two successes around it are not consecutive
	tracker.recordSuccess(tracker.beginDelivery())

	window, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	assert.Equal(t, 10*time.Minute, window)
}

func TestCooldownTrackerRemaining(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	_, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)

	clock.advance(2 * time.Minute)
	assert.Equal(t, 3*time.Minute, tracker.remaining())
	assert.True(t, tracker.inCooldown())

	clock.advance(3*time.Minute + time.Second)
	assert.Equal(t, time.Duration(0), tracker.remaining())
	assert.False(t, tracker.inCooldown())
}

// configWithCooldownClock returns the standard test configuration instrumented so that cooldown
// windows can be crossed by advancing a fake clock. The inherited 1-hour flush interval keeps the
// real-time flush ticker from ever firing within a test.
func configWithCooldownClock() (EventsConfiguration, *fakeCooldownClock) {
	config := basicConfigWithoutPrivateAttrs()
	clock := newFakeCooldownClock()
	config.timeNowFn = clock.nowFn
	return config, clock
}

// enterCooldown drives the processor into a cooldown window: the sender fails one delivery with the
// given result, and the test waits until the dispatcher has consumed that result. The first sync
// guarantees the worker has published the result; it does not guarantee the dispatcher has consumed
// it. The second sync does: the main loop drains pending results before it processes any inbox
// message, so by the time the second sync message is answered the result has been handled.
func enterCooldown(t *testing.T, ep *defaultEventProcessor, es *mockEventSender, result EventSenderResult) {
	es.setResult(result)
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["window-opener"]}`))
	ep.Flush()
	es.awaitEvent(t) // the failing delivery itself reaches the sender
	ep.waitUntilInactive()
	ep.waitUntilInactive()
	es.setResult(EventSenderResult{Success: true})
}

func TestUnexpectedErrorEntersCooldownAndDeliveryResumesAfterWindow(t *testing.T) {
	config, clock := configWithCooldownClock()
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	enterCooldown(t, ep, es, EventSenderResult{Unexpected: true, StatusCode: 401})

	// An in-window flush is a no-op: it makes no delivery attempt, and it disposes of the drained
	// events exactly as a delivered-and-failed payload would be disposed of. FlushBlocking still
	// completes rather than timing out.
	inWindowMessage := json.RawMessage(`{"kind":"raw","arbitrary":["in-window"]}`)
	ep.RecordRawEvent(inWindowMessage)
	require.True(t, ep.FlushBlocking(time.Second))
	es.assertNoMoreEvents(t)

	// Once the window has elapsed, delivery resumes: a fresh event is delivered by the probe, and
	// the discarded in-window event is not resurrected.
	clock.advance(cooldownInitialDelay + time.Second)
	postWindowMessage := json.RawMessage(`{"kind":"raw","arbitrary":["post-window"]}`)
	ep.RecordRawEvent(postWindowMessage)
	ep.Flush()
	assertEventsReceived(t, es, m.JSONEqual(postWindowMessage))
	es.assertNoMoreEvents(t)
}

func TestLegacyMustShutDownResultEntersCooldownInsteadOfDisabling(t *testing.T) {
	config, clock := configWithCooldownClock()
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	// A custom EventSender written before the Unexpected field existed sets only MustShutDown.
	enterCooldown(t, ep, es, EventSenderResult{MustShutDown: true, StatusCode: 403})

	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["in-window"]}`))
	require.True(t, ep.FlushBlocking(time.Second))
	es.assertNoMoreEvents(t)

	// Delivery is suppressed only for the window, not permanently.
	clock.advance(cooldownInitialDelay + time.Second)
	postWindowMessage := json.RawMessage(`{"kind":"raw","arbitrary":["post-window"]}`)
	ep.RecordRawEvent(postWindowMessage)
	ep.Flush()
	assertEventsReceived(t, es, m.JSONEqual(postWindowMessage))
}

func TestRecoverableFailureDoesNotEnterCooldown(t *testing.T) {
	config, _ := configWithCooldownClock()
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	es.setResult(EventSenderResult{Success: false, StatusCode: 503})
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["first"]}`))
	ep.Flush()
	es.awaitEvent(t)
	ep.waitUntilInactive()

	// Delivery continues immediately; no window was opened.
	es.setResult(EventSenderResult{Success: true})
	secondMessage := json.RawMessage(`{"kind":"raw","arbitrary":["second"]}`)
	ep.RecordRawEvent(secondMessage)
	ep.Flush()
	assertEventsReceived(t, es, m.JSONEqual(secondMessage))
}

func TestInWindowDiscardRecordsFailedSendMetric(t *testing.T) {
	config, _ := configWithCooldownClock()
	metrics := &mockEventMetrics{}
	config.EventMetrics = metrics
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	enterCooldown(t, ep, es, EventSenderResult{Unexpected: true, StatusCode: 401})
	require.Equal(t, 1, metrics.failedSendCount) // the window-opening delivery itself
	require.Equal(t, EventSendFailureMetadata{StatusCode: 401}, metrics.lastFailedSendMeta)

	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["in-window"]}`))
	require.True(t, ep.FlushBlocking(time.Second))
	ep.waitUntilInactive()

	// The in-window no-op is observably a failed delivery, exactly as if the payload had been sent
	// and refused: the failed-send metric fires, with DiscardedInCooldown distinguishing the
	// discard from a delivery that failed on the network.
	assert.Equal(t, 2, metrics.failedSendCount)
	assert.Equal(t, EventSendFailureMetadata{DiscardedInCooldown: true}, metrics.lastFailedSendMeta)
	assert.Equal(t, 0, metrics.lastPendingEvents) // the outbox was drained, not retained
}

func TestUnexpectedDiagnosticResultDoesNotAffectAnalyticsDelivery(t *testing.T) {
	// Diagnostic event delivery is not governed by the cooldown (EVTPROC does not cover
	// diagnostics): a diagnostic result never updates cooldown state, because the flush worker
	// does not publish results for diagnostic payloads.
	id := NewDiagnosticID("sdkkey")
	diagnosticsManager := NewDiagnosticsManager(id, testValue, testValue, time.Now(), nil)
	config, _ := configWithCooldownClock()
	config.DiagnosticsManager = diagnosticsManager

	es := newMockEventSender()
	es.setResult(EventSenderResult{Unexpected: true, StatusCode: 401}) // fails the startup diagnostic event
	config.EventSender = es
	ep := NewDefaultEventProcessor(config).(*defaultEventProcessor)
	defer ep.Close()

	es.awaitDiagnosticEvent(t)
	ep.waitUntilInactive()

	// Analytics delivery is unaffected by the failed diagnostic send.
	es.setResult(EventSenderResult{Success: true})
	message := json.RawMessage(`{"kind":"raw","arbitrary":["analytics"]}`)
	ep.RecordRawEvent(message)
	ep.Flush()
	assertEventsReceived(t, es, m.JSONEqual(message))
}

func TestShutdownDuringCooldownMakesNoDeliveryAttempt(t *testing.T) {
	config, _ := configWithCooldownClock()
	ep, es := createEventProcessorAndSender(config)

	enterCooldown(t, ep, es, EventSenderResult{Unexpected: true, StatusCode: 401})

	// The shutdown flush is in-window, so the buffered event is discarded without a network attempt.
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["at-shutdown"]}`))
	require.NoError(t, ep.Close())
	es.assertNoMoreEvents(t)
}

func TestProbeFailureOpensLongerWindow(t *testing.T) {
	config, clock := configWithCooldownClock()
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	enterCooldown(t, ep, es, EventSenderResult{Unexpected: true, StatusCode: 401})

	// The probe after the first window fails again; the second window is doubled, so a flush that
	// would be past a first window is still inside the second.
	clock.advance(cooldownInitialDelay + time.Second)
	enterCooldown(t, ep, es, EventSenderResult{Unexpected: true, StatusCode: 401})

	clock.advance(cooldownInitialDelay + time.Second) // inside the 10-minute second window
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["in-second-window"]}`))
	require.True(t, ep.FlushBlocking(time.Second))
	es.assertNoMoreEvents(t)

	// Past the second window, delivery resumes.
	clock.advance(cooldownInitialDelay + time.Second)
	postWindowMessage := json.RawMessage(`{"kind":"raw","arbitrary":["post-second-window"]}`)
	ep.RecordRawEvent(postWindowMessage)
	ep.Flush()
	assertEventsReceived(t, es, m.JSONEqual(postWindowMessage))
}

// newDispatcherForResultTests builds a bare dispatcher whose handleSenderResult can be driven
// directly and deterministically, without goroutines or a sender.
func newDispatcherForResultTests(clock *fakeCooldownClock) *eventDispatcher {
	config := basicConfigWithoutPrivateAttrs()
	config.Loggers = ldlog.NewDisabledLoggers()
	return &eventDispatcher{
		config:   config,
		cooldown: newDeliveryCooldownTracker(clock.nowFn),
		userKeys: newLruCache(100),
	}
}

func unexpectedResult() EventSenderResult {
	return EventSenderResult{Unexpected: true, StatusCode: 401}
}

func TestStaleInFlightSuccessesDoNotCountTowardReset(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	// Three deliveries were dispatched concurrently with the same token; the first fails and opens
	// a window, so the other two results are stale and must not advance the success streak.
	inFlight := ed.cooldown.beginDelivery()
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: inFlight})
	require.True(t, ed.cooldown.inCooldown())
	ed.handleSenderResult(senderResult{result: EventSenderResult{Success: true}, token: inFlight})
	ed.handleSenderResult(senderResult{result: EventSenderResult{Success: true}, token: inFlight})

	// The retry state did not reset: the probe's failure opens a doubled window.
	clock.advance(5*time.Minute + time.Second)
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: ed.cooldown.beginDelivery()})
	assert.Equal(t, 10*time.Minute, ed.cooldown.remaining())
}

func TestStaleInFlightFailureAfterWindowElapsesDoesNotStepLadder(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	inFlight := ed.cooldown.beginDelivery()
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: inFlight})
	require.True(t, ed.cooldown.inCooldown())

	// A result from a delivery that was in flight when the window opened lands after the window
	// has elapsed. It is stale, so it neither reopens nor doubles the window.
	clock.advance(5*time.Minute + time.Second)
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: inFlight})
	assert.False(t, ed.cooldown.inCooldown())

	// The actual probe failure steps the ladder exactly once.
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: ed.cooldown.beginDelivery()})
	assert.Equal(t, 10*time.Minute, ed.cooldown.remaining())
}

func TestStaleResultStillCalibratesServerTime(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	inFlight := ed.cooldown.beginDelivery()
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: inFlight})
	serverTime := ldtime.UnixMillisecondTime(999999)
	stale := EventSenderResult{Success: true, TimeFromServer: serverTime}
	ed.handleSenderResult(senderResult{result: stale, token: inFlight})
	assert.Equal(t, serverTime, ed.lastKnownPastTime)
}

func TestCooldownTrackerReportsResumeOnFirstPostWindowSuccess(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	assert.False(t, tracker.recordSuccess(tracker.beginDelivery())) // no window has ever opened

	_, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	assert.False(t, tracker.recordSuccess(tracker.beginDelivery())) // defensive: in-window success changes nothing
	assert.Equal(t, 0, tracker.consecutiveSuccesses)

	clock.advance(5*time.Minute + time.Second)
	assert.True(t, tracker.recordSuccess(tracker.beginDelivery()))  // the probe ends the window
	assert.False(t, tracker.recordSuccess(tracker.beginDelivery())) // only reported once
}

func TestInWindowDiscardOfSummaryCountersAndIndexRecovery(t *testing.T) {
	config, clock := configWithCooldownClock()
	metrics := &mockEventMetrics{}
	config.EventMetrics = metrics
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	enterCooldown(t, ep, es, unexpectedResult())
	baseline := metrics.failedSendCount

	// An evaluation recorded in-window produces an index event and a summary counter; the
	// in-window flush discards both (counted as index + summary = 2).
	flag := FlagEventProperties{Key: "flagkey", Version: 11}
	fe := defaultEventFactory.NewEvaluationData(flag, basicContext(), testEvalDetailWithoutReason,
		false, ldvalue.Null(), "", ldvalue.OptionalInt{}, false)
	ep.RecordEvaluation(fe)
	require.True(t, ep.FlushBlocking(time.Second))
	ep.waitUntilInactive()
	assert.Equal(t, baseline+2, metrics.failedSendCount)
	es.assertNoMoreEvents(t)

	// After the window, the same context evaluates again. The discarded index event's context was
	// forgotten along with it, so the delivered payload carries a fresh index event rather than
	// referencing a context the service never received.
	clock.advance(cooldownInitialDelay + time.Second)
	ep.RecordEvaluation(fe)
	ep.Flush()
	assertEventsReceived(t, es, anyIndexEvent(), anySummaryEvent())
	es.assertNoMoreEvents(t)
}

func TestBoundaryRoundSiblingsAreStaleAfterProbeSuccess(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	// Window opens; after it elapses, three deliveries are dispatched concurrently in the
	// boundary round, all sharing the same token.
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: ed.cooldown.beginDelivery()})
	clock.advance(5*time.Minute + time.Second)
	boundaryToken := ed.cooldown.beginDelivery()

	// The probe's success ends the round; the sibling success must not combine with it into a
	// reset, and the sibling failure must not open a window (the next round's failure does).
	ed.handleSenderResult(senderResult{result: EventSenderResult{Success: true}, token: boundaryToken})
	ed.handleSenderResult(senderResult{result: EventSenderResult{Success: true}, token: boundaryToken})
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: boundaryToken})
	require.False(t, ed.cooldown.inCooldown())

	// The retry state was not reset (the sibling failure also broke the streak), so the next
	// failed delivery opens a doubled window.
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: ed.cooldown.beginDelivery()})
	assert.Equal(t, 10*time.Minute, ed.cooldown.remaining())
}

func TestBoundaryRoundSiblingsAreStaleAfterRecoverableProbeFailure(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: ed.cooldown.beginDelivery()})
	clock.advance(5*time.Minute + time.Second)
	boundaryToken := ed.cooldown.beginDelivery()

	// The probe fails recoverably, which also ends the boundary round: the two sibling successes
	// are stale and must not combine into a reset. (A sibling unexpected failure in this round
	// would go stale with them, deferring its window to the next round's failure by at most one
	// cadence.)
	ed.handleSenderResult(senderResult{result: EventSenderResult{Success: false, StatusCode: 503}, token: boundaryToken})
	ed.handleSenderResult(senderResult{result: EventSenderResult{Success: true}, token: boundaryToken})
	ed.handleSenderResult(senderResult{result: EventSenderResult{Success: true}, token: boundaryToken})

	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: ed.cooldown.beginDelivery()})
	assert.Equal(t, 10*time.Minute, ed.cooldown.remaining())
}

func TestNoSpuriousResumeAfterRecoverableFailures(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	failPastWindow(t, tracker, clock)
	tracker.recordFailure(tracker.beginDelivery()) // the probe fails recoverably; the window is over

	// Much later, an ordinary success must not be reported as a cooldown resumption.
	clock.advance(1000 * time.Minute)
	assert.False(t, tracker.recordSuccess(tracker.beginDelivery()))
}

func TestProcessorTreats413ResultAsOrdinaryFailure(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	// A 413 result carries neither Unexpected nor MustShutDown; the processor must not open a
	// window for it.
	ed.handleSenderResult(senderResult{result: EventSenderResult{Success: false, StatusCode: 413}, token: ed.cooldown.beginDelivery()})
	assert.False(t, ed.cooldown.inCooldown())
}

func TestDiscardLogsWarnOncePerWindowThenDebug(t *testing.T) {
	config, clock := configWithCooldownClock()
	mockLog := ldlogtest.NewMockLog()
	mockLog.Loggers.SetMinLevel(ldlog.Debug)
	config.Loggers = mockLog.Loggers
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	countDiscardLogs := func(level ldlog.LogLevel) int {
		n := 0
		for _, line := range mockLog.GetOutput(level) {
			if strings.Contains(line, "Discarded") {
				n++
			}
		}
		return n
	}

	enterCooldown(t, ep, es, unexpectedResult())
	for i := 0; i < 3; i++ {
		ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["in-window"]}`))
		require.True(t, ep.FlushBlocking(time.Second))
	}
	assert.Equal(t, 1, countDiscardLogs(ldlog.Warn))
	assert.Equal(t, 2, countDiscardLogs(ldlog.Debug))

	// A second window gets its own single Warn, and the resumption is logged at Info.
	clock.advance(cooldownInitialDelay + time.Second)
	enterCooldown(t, ep, es, unexpectedResult())
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["in-window-2"]}`))
	require.True(t, ep.FlushBlocking(time.Second))
	assert.Equal(t, 2, countDiscardLogs(ldlog.Warn))

	clock.advance(2*cooldownInitialDelay + time.Second)
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["post-window"]}`))
	ep.Flush()
	es.awaitEvent(t)
	ep.waitUntilInactive()
	ep.waitUntilInactive()
	resumeLines := 0
	for _, line := range mockLog.GetOutput(ldlog.Info) {
		if strings.Contains(line, "resumed after cooldown") {
			resumeLines++
		}
	}
	assert.Equal(t, 1, resumeLines)
}

func TestOrdinaryFailureOutsideAWindowDoesNotStaleConcurrentResults(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	// During healthy operation (no window has ever opened), an ordinary failure must not close a
	// boundary round that does not exist: a concurrent unexpected failure in the same round must
	// still open a window.
	roundToken := ed.cooldown.beginDelivery()
	ed.handleSenderResult(senderResult{result: EventSenderResult{Success: false, StatusCode: 503}, token: roundToken})
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: roundToken})
	assert.True(t, ed.cooldown.inCooldown())
}

func TestIdentifyOnlyInWindowDiscardForgetsTheContext(t *testing.T) {
	config, clock := configWithCooldownClock()
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	enterCooldown(t, ep, es, unexpectedResult())

	// An identify event is a context introduction just as an index event is; when an in-window
	// discard destroys it, the dedup cache must forget the context so a later payload can
	// introduce it again.
	ep.RecordIdentifyEvent(defaultEventFactory.NewIdentifyEventData(basicContext(), ldvalue.OptionalInt{}))
	require.True(t, ep.FlushBlocking(time.Second))
	es.assertNoMoreEvents(t)

	clock.advance(cooldownInitialDelay + time.Second)
	flag := FlagEventProperties{Key: "flagkey", Version: 11}
	fe := defaultEventFactory.NewEvaluationData(flag, basicContext(), testEvalDetailWithoutReason,
		false, ldvalue.Null(), "", ldvalue.OptionalInt{}, false)
	ep.RecordEvaluation(fe)
	ep.Flush()
	assertEventsReceived(t, es, anyIndexEvent(), anySummaryEvent())
}

func TestWindowOpenForgetsEvenDeliveredIntroductions(t *testing.T) {
	config, clock := configWithCooldownClock()
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	// Introduce the context successfully before any window exists.
	flag := FlagEventProperties{Key: "flagkey", Version: 11}
	fe := defaultEventFactory.NewEvaluationData(flag, basicContext(), testEvalDetailWithoutReason,
		false, ldvalue.Null(), "", ldvalue.OptionalInt{}, false)
	ep.RecordEvaluation(fe)
	ep.Flush()
	assertEventsReceived(t, es, anyIndexEvent(), anySummaryEvent())

	// Opening a window forgets every context key, including ones whose index events were
	// delivered: the dispatcher cannot see which contexts the refused batch carried, and a
	// redundant index event after the window is harmless.
	enterCooldown(t, ep, es, unexpectedResult())
	ep.RecordEvaluation(fe)
	require.True(t, ep.FlushBlocking(time.Second))
	es.assertNoMoreEvents(t)

	clock.advance(cooldownInitialDelay + time.Second)
	ep.RecordEvaluation(fe)
	ep.Flush()
	assertEventsReceived(t, es, anyIndexEvent(), anySummaryEvent())
}

func TestRefusedWindowOpeningBatchForgetsItsContexts(t *testing.T) {
	config, clock := configWithCooldownClock()
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	// The window-opening batch itself introduces a context via an identify event, and is
	// refused: the full context never reached the service.
	es.setResult(unexpectedResult())
	ep.RecordIdentifyEvent(defaultEventFactory.NewIdentifyEventData(basicContext(), ldvalue.OptionalInt{}))
	ep.Flush()
	es.awaitEvent(t)
	ep.waitUntilInactive()
	ep.waitUntilInactive()
	es.setResult(EventSenderResult{Success: true})

	// With no in-window flush at all, the post-window payload must still introduce the context
	// afresh rather than referencing one the service never received.
	clock.advance(cooldownInitialDelay + time.Second)
	flag := FlagEventProperties{Key: "flagkey", Version: 11}
	fe := defaultEventFactory.NewEvaluationData(flag, basicContext(), testEvalDetailWithoutReason,
		false, ldvalue.Null(), "", ldvalue.OptionalInt{}, false)
	ep.RecordEvaluation(fe)
	ep.Flush()
	assertEventsReceived(t, es, anyIndexEvent(), anySummaryEvent())
	es.assertNoMoreEvents(t)
}

func TestInWindowFlushWithEmptyOutboxCompletesPromptlyWithoutDiscard(t *testing.T) {
	config, _ := configWithCooldownClock()
	mockLog := ldlogtest.NewMockLog()
	mockLog.Loggers.SetMinLevel(ldlog.Debug)
	config.Loggers = mockLog.Loggers
	metrics := &mockEventMetrics{}
	config.EventMetrics = metrics
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	enterCooldown(t, ep, es, unexpectedResult())
	failedAfterOpen := metrics.failedSendCount

	// Nothing is buffered, so there is no batch to dispose of: no failed-send metric and no
	// discard log. The flush must still complete immediately; a timeout of zero or less means
	// wait indefinitely, so this call returns only if the completion channel is closed promptly.
	returned := make(chan bool, 1)
	go func() { returned <- ep.FlushBlocking(0) }()
	select {
	case r := <-returned:
		assert.True(t, r)
	case <-time.After(time.Second):
		require.Fail(t, "FlushBlocking(0) did not complete for an empty in-window flush")
	}

	ep.waitUntilInactive()
	assert.Equal(t, failedAfterOpen, metrics.failedSendCount)
	for _, level := range []ldlog.LogLevel{ldlog.Warn, ldlog.Debug} {
		for _, line := range mockLog.GetOutput(level) {
			assert.NotContains(t, line, "Discarded")
		}
	}
}

func TestUnexpectedFailureResultDoesNotCalibrateServerTime(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	// The server time on an unexpected-failure result is not trusted for debug-event cutoff
	// calibration, even though the same field on a stale or recoverable result is.
	serverTime := ldtime.UnixMillisecondTime(777777)
	ed.handleSenderResult(senderResult{
		result: EventSenderResult{Unexpected: true, StatusCode: 401, TimeFromServer: serverTime},
		token:  ed.cooldown.beginDelivery(),
	})
	assert.Equal(t, ldtime.UnixMillisecondTime(0), ed.lastKnownPastTime)
}

func TestZeroTokenIsNeverCurrent(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	// An unstamped payload (the zero windowToken) must never pass for a live delivery, whether or
	// not a window has ever existed.
	assert.False(t, tracker.isCurrent(windowToken{}))
	_, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	clock.advance(cooldownInitialDelay + time.Second)
	assert.False(t, tracker.isCurrent(windowToken{}))

	before := tracker.consecutiveSuccesses
	assert.False(t, tracker.recordSuccess(windowToken{}))
	assert.Equal(t, before, tracker.consecutiveSuccesses)

	// Unstamped failures are not real delivery outcomes and must not break the streak either.
	require.True(t, tracker.recordSuccess(tracker.beginDelivery())) // the probe; streak is 1
	tracker.recordFailure(windowToken{})
	_, opened = tracker.recordUnexpectedFailure(windowToken{})
	require.False(t, opened)
	assert.Equal(t, 1, tracker.consecutiveSuccesses)
}

func TestStaleOrdinaryFailureCannotCancelTheNextWindow(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	// A delivery is in flight when a concurrent delivery's failure opens the window.
	inFlight := ed.cooldown.beginDelivery()
	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: inFlight})
	require.True(t, ed.cooldown.inCooldown())

	// The window elapses and a probe is dispatched; then the straggler's recoverable failure
	// lands. It is stale: were it allowed to end the already-elapsed window, it would close the
	// boundary round and stale the probe, so the probe's failure would open no window at all.
	clock.advance(cooldownInitialDelay + time.Second)
	probe := ed.cooldown.beginDelivery()
	ed.handleSenderResult(senderResult{
		result: EventSenderResult{Success: false, StatusCode: 503},
		token:  inFlight,
	})

	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: probe})
	assert.True(t, ed.cooldown.inCooldown())
	assert.Equal(t, 10*time.Minute, ed.cooldown.remaining())
}

func TestStaleFailureBetweenSuccessesPreventsLadderReset(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	// The probe and a sibling are dispatched together in the boundary round; the probe succeeds,
	// staling the sibling, and then the sibling's real failure lands.
	failPastWindow(t, tracker, clock) // one failed probe on the ladder
	boundaryToken := tracker.beginDelivery()
	require.True(t, tracker.recordSuccess(boundaryToken))
	tracker.recordFailure(boundaryToken)
	tracker.recordSuccess(tracker.beginDelivery())

	// Success, failure, success: the successes were not consecutive, so the ladder did not reset
	// and the next failed probe opens the doubled window.
	window, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	assert.Equal(t, 10*time.Minute, window)
}

func TestStaleUnexpectedFailureBetweenSuccessesPreventsLadderReset(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	failPastWindow(t, tracker, clock)
	boundaryToken := tracker.beginDelivery()
	require.True(t, tracker.recordSuccess(boundaryToken))

	// The stale sibling's unexpected failure breaks the streak but neither opens nor steps.
	window, opened := tracker.recordUnexpectedFailure(boundaryToken)
	require.False(t, opened)
	require.Equal(t, time.Duration(0), window)
	require.False(t, tracker.inCooldown())
	tracker.recordSuccess(tracker.beginDelivery())

	window, opened = tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	assert.Equal(t, 10*time.Minute, window)
}

func TestDefaultClockOpensWindow(t *testing.T) {
	// Production passes no clock; the default real-time clock path must work end to end.
	tracker := newDeliveryCooldownTracker(nil)
	window, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	assert.Equal(t, cooldownInitialDelay, window)
	assert.True(t, tracker.inCooldown())
	assert.Greater(t, tracker.remaining(), cooldownInitialDelay-time.Minute)
}

func TestWindowBoundaryInstantIsOutsideCooldown(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	window, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	clock.advance(window)
	assert.False(t, tracker.inCooldown())
	assert.Equal(t, time.Duration(0), tracker.remaining())
}

func TestResultClaimingBothSuccessAndUnexpectedFailureOpensWindow(t *testing.T) {
	// Only a custom EventSender can produce a result that claims success and an unexpected
	// failure at once; the failure takes precedence, so delivery health is never assumed from a
	// contradictory result.
	config, _ := configWithCooldownClock()
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	enterCooldown(t, ep, es, EventSenderResult{Success: true, MustShutDown: true, StatusCode: 401})

	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["in-window"]}`))
	require.True(t, ep.FlushBlocking(time.Second))
	es.assertNoMoreEvents(t)
}

func TestWindowShorterThanFlushIntervalResumesWithoutInWindowFlush(t *testing.T) {
	// With a flush interval longer than the window, no flush of any kind lands inside the window,
	// so nothing in the dispatcher may depend on an in-window tick: the window must be exactly
	// the ladder rung rather than stretched to the flush cadence, and the first post-window flush
	// must deliver.
	config, clock := configWithCooldownClock()
	require.Equal(t, time.Hour, config.FlushInterval)
	metrics := &mockEventMetrics{}
	config.EventMetrics = metrics
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	flag := FlagEventProperties{Key: "flagkey", Version: 11}
	fe := defaultEventFactory.NewEvaluationData(flag, basicContext(), testEvalDetailWithoutReason,
		false, ldvalue.Null(), "", ldvalue.OptionalInt{}, false)
	ep.RecordEvaluation(fe)
	ep.Flush()
	assertEventsReceived(t, es, anyIndexEvent(), anySummaryEvent())

	enterCooldown(t, ep, es, unexpectedResult())
	ep.waitUntilInactive()
	failedAfterOpen := metrics.failedSendCount

	clock.advance(cooldownInitialDelay - time.Second)
	assert.Equal(t, failedAfterOpen, metrics.failedSendCount) // nothing was discarded in-window

	// Just past the rung, the next flush delivers. The window-open wipe forgot the context, so
	// the payload introduces it again.
	clock.advance(2 * time.Second)
	ep.RecordEvaluation(fe)
	ep.Flush()
	assertEventsReceived(t, es, anyIndexEvent(), anySummaryEvent())
	es.assertNoMoreEvents(t)

	ep.waitUntilInactive()
	ep.waitUntilInactive()
	assert.Equal(t, failedAfterOpen, metrics.failedSendCount)
}

func TestWarnLevelDiscardReturnsAfterRecoverablyEndedWindow(t *testing.T) {
	config, clock := configWithCooldownClock()
	mockLog := ldlogtest.NewMockLog()
	mockLog.Loggers.SetMinLevel(ldlog.Debug)
	config.Loggers = mockLog.Loggers
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	countWarns := func() int {
		n := 0
		for _, line := range mockLog.GetOutput(ldlog.Warn) {
			if strings.Contains(line, "Discarded") {
				n++
			}
		}
		return n
	}

	enterCooldown(t, ep, es, unexpectedResult())
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["w1"]}`))
	require.True(t, ep.FlushBlocking(time.Second))
	require.Equal(t, 1, countWarns())

	// The window elapses and the probe fails recoverably, which ends the window without opening
	// a new one. The next window is a new incident and must get its own Warn-level discard.
	clock.advance(cooldownInitialDelay + time.Second)
	es.setResult(EventSenderResult{Success: false, StatusCode: 503})
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["probe"]}`))
	ep.Flush()
	es.awaitEvent(t)
	ep.waitUntilInactive()
	ep.waitUntilInactive()

	enterCooldown(t, ep, es, unexpectedResult())
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["w2"]}`))
	require.True(t, ep.FlushBlocking(time.Second))
	assert.Equal(t, 2, countWarns())
}

// The in-window discard reports a count computed from the outbox before any serialization, while
// a delivered-and-refused batch reports the count computed during serialization. For the same
// recorded inputs, the failed-send metric must report the same number either way.
func TestDiscardCountMatchesDeliveredBatchCount(t *testing.T) {
	flag := FlagEventProperties{Key: "flagkey", Version: 11}
	cases := []struct {
		name   string
		record func(ep *defaultEventProcessor)
	}{
		{"raw events only", func(ep *defaultEventProcessor) {
			ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["a"]}`))
			ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["b"]}`))
		}},
		{"evaluation with index and summary", func(ep *defaultEventProcessor) {
			ep.RecordEvaluation(defaultEventFactory.NewEvaluationData(flag, basicContext(),
				testEvalDetailWithoutReason, false, ldvalue.Null(), "", ldvalue.OptionalInt{}, false))
		}},
		{"identify and summary", func(ep *defaultEventProcessor) {
			ep.RecordIdentifyEvent(defaultEventFactory.NewIdentifyEventData(basicContext(), ldvalue.OptionalInt{}))
			ep.RecordEvaluation(defaultEventFactory.NewEvaluationData(flag, basicContext(),
				testEvalDetailWithoutReason, false, ldvalue.Null(), "", ldvalue.OptionalInt{}, false))
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The batch is delivered and refused by the service.
			deliveredConfig, _ := configWithCooldownClock()
			deliveredMetrics := &mockEventMetrics{}
			deliveredConfig.EventMetrics = deliveredMetrics
			deliveredEP, deliveredES := createEventProcessorAndSender(deliveredConfig)
			defer deliveredEP.Close()
			deliveredES.setResult(EventSenderResult{Success: false, StatusCode: 503})
			c.record(deliveredEP)
			require.True(t, deliveredEP.FlushBlocking(time.Second))
			deliveredEP.waitUntilInactive()
			deliveredCount := deliveredMetrics.failedSendCount
			require.Greater(t, deliveredCount, 0)

			// The same recorded inputs are discarded by an in-window flush.
			discardConfig, _ := configWithCooldownClock()
			discardMetrics := &mockEventMetrics{}
			discardConfig.EventMetrics = discardMetrics
			discardEP, discardES := createEventProcessorAndSender(discardConfig)
			defer discardEP.Close()
			enterCooldown(t, discardEP, discardES, unexpectedResult())
			base := discardMetrics.failedSendCount
			c.record(discardEP)
			require.True(t, discardEP.FlushBlocking(time.Second))
			discardEP.waitUntilInactive()

			assert.Equal(t, deliveredCount, discardMetrics.failedSendCount-base)
		})
	}
}
func TestFlushQueuedBehindWindowOpeningResultIsDiscarded(t *testing.T) {
	// A flush request that is already queued when a window-opening delivery result is pending
	// must be processed in-window and discarded: the dispatcher consumes pending delivery results
	// before any queued message, so cooldown state is current by the time the flush is handled.
	//
	// The gated sender holds the failing delivery in flight while a flush is queued behind a sync
	// message; the main loop blocks on the sync until the held delivery finishes, and the worker
	// publishes its result before it is counted done, so when the main loop moves on, the failure
	// result and the queued flush are both pending. An unprioritized select dispatches the queued
	// flush in a measurable fraction of iterations (empirically on the order of one in five), so
	// the iteration count makes a regression overwhelmingly likely to surface.
	for i := 0; i < 40; i++ {
		func() {
			config, _ := configWithCooldownClock()
			ep, es := createEventProcessorAndSender(config)
			defer ep.Close()

			gateCh := make(chan struct{})
			waitingCh := make(chan struct{}, 10) // roomy so no conceivable regression wedges the sender
			es.setGate(gateCh, waitingCh)
			es.setResult(unexpectedResult())

			ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["opener"]}`))
			ep.Flush()
			<-waitingCh      // the delivery is now held in flight inside the sender
			es.awaitEvent(t) // consume the opener so assertNoMoreEvents below is meaningful

			ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["queued"]}`))
			sync := syncEventsMessage{replyCh: make(chan struct{})}
			ep.inboxCh <- sync // queued directly so the flush is ordered strictly after it
			ep.Flush()
			close(gateCh) // release the held delivery; its failure opens the window
			<-sync.replyCh
			ep.waitUntilInactive() // the queued flush has now been processed

			assert.Equal(t, 1, es.getPayloadCount(),
				"the queued flush must have been discarded in-window, not dispatched")
			es.assertNoMoreEvents(t)
		}()
	}
}

func TestWarnLevelDiscardReturnsAfterSuccessfullyEndedWindow(t *testing.T) {
	config, clock := configWithCooldownClock()
	mockLog := ldlogtest.NewMockLog()
	mockLog.Loggers.SetMinLevel(ldlog.Debug)
	config.Loggers = mockLog.Loggers
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	countWarns := func() int {
		n := 0
		for _, line := range mockLog.GetOutput(ldlog.Warn) {
			if strings.Contains(line, "Discarded") {
				n++
			}
		}
		return n
	}

	enterCooldown(t, ep, es, unexpectedResult())
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["w1"]}`))
	require.True(t, ep.FlushBlocking(time.Second))
	require.Equal(t, 1, countWarns())

	// The window elapses and the probe succeeds, so delivery resumes.
	clock.advance(cooldownInitialDelay + time.Second)
	es.setResult(EventSenderResult{Success: true})
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["probe"]}`))
	ep.Flush()
	es.awaitEvent(t)
	ep.waitUntilInactive()
	ep.waitUntilInactive()

	// A later incident opens a second window; its first discard is a new incident's first notice
	// and must log at Warn, not Debug.
	enterCooldown(t, ep, es, unexpectedResult())
	ep.RecordRawEvent(json.RawMessage(`{"kind":"raw","arbitrary":["w2"]}`))
	require.True(t, ep.FlushBlocking(time.Second))
	assert.Equal(t, 2, countWarns())
}

func TestParallelSuccessesInOneNonBoundaryRoundCountIndividually(t *testing.T) {
	clock := newFakeCooldownClock()
	tracker := newTrackerForTest(clock)

	failPastWindow(t, tracker, clock)              // one failed probe on the ladder
	tracker.recordFailure(tracker.beginDelivery()) // a recoverable probe ends the window

	// Outside a window boundary, concurrently dispatched deliveries share a token and each counts
	// toward the streak: the reset threshold counts successful deliveries, not rounds.
	roundToken := tracker.beginDelivery()
	require.False(t, tracker.recordSuccess(roundToken))
	require.True(t, tracker.isCurrent(roundToken))
	tracker.recordSuccess(roundToken)

	window, opened := tracker.recordUnexpectedFailure(tracker.beginDelivery())
	require.True(t, opened)
	assert.Equal(t, cooldownInitialDelay, window, "two successes in one round reset the ladder")
}

func TestInWindowDiscardZeroesEventsInLastBatch(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := &eventDispatcher{
		config:       basicConfigWithoutPrivateAttrs(),
		outbox:       newEventsOutbox(100, ldlog.NewDisabledLoggers(), NoOpEventMetrics{}),
		userKeys:     newLruCache(100),
		cooldown:     newDeliveryCooldownTracker(clock.nowFn),
		flushCh:      make(chan *flushPayload, 1),
		workersGroup: &sync.WaitGroup{},
	}
	ed.config.EventMetrics = NoOpEventMetrics{}
	ed.config.Loggers = ldlog.NewDisabledLoggers()

	// A dispatched flush records the batch size.
	ed.outbox.addEvent(rawEvent{data: json.RawMessage(`{"kind":"custom","key":"a"}`)})
	ed.triggerFlush(nil)
	require.Equal(t, 1, ed.eventsInLastBatch)
	<-ed.flushCh
	ed.workersGroup.Done()

	// An in-window discard dispatches no batch, so the diagnostic batch-size stat must not
	// report one.
	_, opened := ed.cooldown.recordUnexpectedFailure(ed.cooldown.beginDelivery())
	require.True(t, opened)
	ed.outbox.addEvent(rawEvent{data: json.RawMessage(`{"kind":"custom","key":"b"}`)})
	ed.triggerFlush(nil)
	assert.Equal(t, 0, ed.eventsInLastBatch)
	assert.Empty(t, ed.outbox.events, "the outbox was drained by the discard")
}

func TestLadderPositionSurvivesRecoverablyEndedWindow(t *testing.T) {
	clock := newFakeCooldownClock()
	ed := newDispatcherForResultTests(clock)

	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: ed.cooldown.beginDelivery()})
	require.Equal(t, cooldownInitialDelay, ed.cooldown.remaining())

	// The probe fails recoverably: the window ends without opening a new one, but the ladder
	// position is preserved, so the next unexpected failure opens the next rung.
	clock.advance(cooldownInitialDelay + time.Second)
	ed.handleSenderResult(senderResult{
		result: EventSenderResult{Success: false, StatusCode: 503},
		token:  ed.cooldown.beginDelivery(),
	})
	require.False(t, ed.cooldown.inCooldown())

	ed.handleSenderResult(senderResult{result: unexpectedResult(), token: ed.cooldown.beginDelivery()})
	assert.Equal(t, 10*time.Minute, ed.cooldown.remaining())
}

func TestRecordingContinuesThroughoutWindow(t *testing.T) {
	config, clock := configWithCooldownClock()
	metrics := &mockEventMetrics{}
	config.EventMetrics = metrics
	ep, es := createEventProcessorAndSender(config)
	defer ep.Close()

	enterCooldown(t, ep, es, unexpectedResult())
	flag := FlagEventProperties{Key: "flagkey", Version: 11}
	fe := defaultEventFactory.NewEvaluationData(flag, basicContext(), testEvalDetailWithoutReason,
		false, ldvalue.Null(), "", ldvalue.OptionalInt{}, false)

	// Recording is never gated: every in-window interval accepts events, and each in-window flush
	// disposes of what accumulated without rejecting anything at the recording boundary.
	for i := 0; i < 3; i++ {
		ep.RecordEvaluation(fe)
		require.True(t, ep.FlushBlocking(time.Second))
	}
	metrics.lock.Lock()
	dropped := metrics.droppedCount
	metrics.lock.Unlock()
	assert.Equal(t, 0, dropped)

	clock.advance(cooldownInitialDelay + time.Second)
	ep.RecordEvaluation(fe)
	ep.Flush()
	assertEventsReceived(t, es, anyIndexEvent(), anySummaryEvent())
	es.assertNoMoreEvents(t)
}
