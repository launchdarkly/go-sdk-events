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
// windows can be crossed by advancing a fake clock. The flush interval only needs to be long
// enough in real time that the flush ticker never fires within a test.
func configWithCooldownClock() (EventsConfiguration, *fakeCooldownClock) {
	config := basicConfigWithoutPrivateAttrs()
	config.FlushInterval = time.Minute
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
	// and refused: the failed-send metric fires, with a zero status code since no response was
	// received.
	assert.Equal(t, 2, metrics.failedSendCount)
	assert.Equal(t, EventSendFailureMetadata{}, metrics.lastFailedSendMeta)
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

	// The retry state was not reset (one credited success, not two), so the next failed delivery
	// opens a doubled window.
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

func TestSummaryOnlyInWindowDiscardKeepsAlreadyIntroducedContexts(t *testing.T) {
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

	// A summary-only batch discarded in-window destroys no context introduction, so the dedup
	// cache must be left intact.
	enterCooldown(t, ep, es, unexpectedResult())
	ep.RecordEvaluation(fe)
	require.True(t, ep.FlushBlocking(time.Second))
	es.assertNoMoreEvents(t)

	// The post-window payload therefore carries no redundant index event.
	clock.advance(cooldownInitialDelay + time.Second)
	ep.RecordEvaluation(fe)
	ep.Flush()
	assertEventsReceived(t, es, anySummaryEvent())
}
