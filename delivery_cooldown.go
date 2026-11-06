package ldevents

import (
	"time"
)

// These constants bind the RETRY spec's extended regime to the event processor. A healthy
// operation is a successful (2xx) analytics payload delivery. The windows are not floored at the
// flush interval: unlike a polling data source, whose retry delay is its scheduler, the processor
// attempts delivery only on its unchanged flush schedule, so a window shorter than the flush
// interval cannot make it try faster while failing than while healthy.
const (
	cooldownInitialDelay   = 5 * time.Minute
	cooldownMaxDelay       = time.Hour
	cooldownResetThreshold = 2
)

// windowToken identifies the span between cooldown-window boundaries in which a delivery was
// dispatched; a token stops being current when a window opens or ends. The dispatcher obtains one
// from beginDelivery when it hands a payload off for delivery and presents it with the delivery's
// outcome; the tracker ignores outcomes whose token is no longer current. The zero value is never
// current, so an unstamped payload cannot pass for a live delivery.
type windowToken struct {
	generation int
}

// deliveryCooldownTracker decides when analytics event delivery is in a cooldown window after an
// unexpected failure, so that a condition such as an invalid SDK key backs delivery off instead
// of permanently disabling it.
//
// Every delivery outcome is reported together with the windowToken issued when that delivery
// was dispatched. A token stops being current when a window opens or ends, so an outcome from a
// delivery that was already in flight across that boundary updates nothing. This is what confines
// ladder movement and the success streak to actual probes even with concurrent deliveries
// straddling a window boundary.
//
// The tracker is pure state: it starts no timers and does no I/O or logging. It is not safe for
// concurrent use; the dispatcher accesses it only from its main goroutine.
type deliveryCooldownTracker struct {
	nowFn                func() time.Time
	attempts             int
	consecutiveSuccesses int
	windowEnd            time.Time
	epoch                int
}

func newDeliveryCooldownTracker(nowFn func() time.Time) *deliveryCooldownTracker {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &deliveryCooldownTracker{nowFn: nowFn}
}

// inCooldown reports whether a cooldown window is in effect.
func (c *deliveryCooldownTracker) inCooldown() bool {
	return !c.windowEnd.IsZero() && c.nowFn().Before(c.windowEnd)
}

// beginDelivery returns the token that a delivery dispatched now must present with its outcome.
func (c *deliveryCooldownTracker) beginDelivery() windowToken {
	return windowToken{generation: c.epoch + 1}
}

// isCurrent reports whether a delivery dispatched with this token is from the current era, that
// is, no window has opened or ended since the token was issued.
func (c *deliveryCooldownTracker) isCurrent(token windowToken) bool {
	return token.generation == c.epoch+1
}

// remaining reports how much of the current window is left, or zero outside a window.
func (c *deliveryCooldownTracker) remaining() time.Duration {
	if c.windowEnd.IsZero() {
		return 0
	}
	if r := c.windowEnd.Sub(c.nowFn()); r > 0 {
		return r
	}
	return 0
}

// recordSuccess records a successful delivery. The retry state resets only after
// cooldownResetThreshold consecutive successes. It returns true when this success is the probe
// that ended a window (the first success after the window elapsed), so the caller can log the
// resumption. Stale outcomes update nothing; the in-cooldown branch is defensive, since a
// current-era delivery cannot have been dispatched while a window was open.
func (c *deliveryCooldownTracker) recordSuccess(token windowToken) (resumed bool) {
	if !c.isCurrent(token) {
		return false
	}
	if c.inCooldown() { // COVERAGE: defensive; a current token cannot originate inside a window
		return false
	}
	if !c.windowEnd.IsZero() {
		c.windowEnd = time.Time{}
		resumed = true
		// The probe ends the window-boundary round: the tokens of its concurrently dispatched
		// siblings go stale, so "consecutive" successes are cadence-separated rounds, not parallel
		// deliveries of one round. (A sibling unexpected failure goes stale with them; the next
		// failed delivery of the new round opens the window instead, at most a cadence later.)
		c.epoch++
	}
	c.consecutiveSuccesses++
	if c.consecutiveSuccesses >= cooldownResetThreshold {
		c.attempts = 0
	}
	return resumed
}

// recordFailure records a delivery failure that was not unexpected. It breaks the
// consecutive-success streak but never opens or lengthens a window. Stale outcomes update
// nothing; the in-cooldown branch is defensive, since a current-era delivery cannot have been
// dispatched while a window was open.
func (c *deliveryCooldownTracker) recordFailure(token windowToken) {
	if !c.isCurrent(token) {
		return
	}
	if c.inCooldown() { // COVERAGE: defensive; a current token cannot originate inside a window
		return
	}
	if !c.windowEnd.IsZero() {
		// A probe that fails recoverably still ends the window: clear it so a much later success is
		// not misreported as the resumption, and close the boundary round (see recordSuccess) so the
		// probe's concurrently dispatched siblings cannot combine into a reset. The accepted cost is
		// that a sibling unexpected failure in the same round goes stale with them, deferring the
		// next window by at most one cadence: the next round's failure opens it, with the ladder
		// position preserved.
		c.windowEnd = time.Time{}
		c.epoch++
	}
	c.consecutiveSuccesses = 0
}

// recordUnexpectedFailure records a delivery failure classified as unexpected, opening a cooldown
// window. Stale outcomes update nothing: in particular, once one failure of a round has opened a
// window, its concurrently dispatched siblings cannot lengthen it or step the ladder again for
// the same incident. The window doubles from cooldownInitialDelay up to cooldownMaxDelay per
// failed probe. It returns the window length and true when a new window was opened.
func (c *deliveryCooldownTracker) recordUnexpectedFailure(token windowToken) (time.Duration, bool) {
	if !c.isCurrent(token) {
		return 0, false
	}
	if c.inCooldown() { // COVERAGE: defensive; a current token cannot originate inside a window
		return 0, false
	}
	c.consecutiveSuccesses = 0
	c.attempts++
	c.epoch++
	window := cooldownMaxDelay
	// Compare against the ceiling scaled down rather than the base scaled up, so the shift cannot
	// overflow. (A shift of 63 or more would always be clamped anyway.)
	exponent := c.attempts - 1
	if exponent >= 0 && exponent < 63 && cooldownInitialDelay <= cooldownMaxDelay>>exponent {
		window = cooldownInitialDelay << exponent
	}
	c.windowEnd = c.nowFn().Add(window)
	return window, true
}
