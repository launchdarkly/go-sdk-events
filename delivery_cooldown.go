package ldevents

import (
	"time"
)

// The extended-regime ladder. Windows are deliberately not floored at the flush interval.
const (
	cooldownInitialDelay   = 5 * time.Minute
	cooldownMaxDelay       = time.Hour
	cooldownResetThreshold = 2
)

// windowToken identifies the span between window boundaries in which a delivery was dispatched.
// The zero value is never current, so an unstamped payload cannot pass for a live delivery.
type windowToken struct {
	generation int
}

// deliveryCooldownTracker decides when analytics event delivery is in a cooldown window after an
// unexpected failure, so that a condition such as an invalid SDK key backs delivery off instead
// of permanently disabling it. Outcomes with a stale windowToken update nothing, which confines
// ladder movement and the success streak to actual probes under concurrent deliveries.
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
// cooldownResetThreshold consecutive successes. It returns true when this success is the first probe
// after a window ended.
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
		// The probe ends the window: the tokens previously given out go stale
		c.epoch++
	}
	c.consecutiveSuccesses++
	if c.consecutiveSuccesses >= cooldownResetThreshold {
		c.attempts = 0
	}
	return resumed
}

// recordFailure records a delivery failure that was not unexpected. It breaks the
// consecutive-success streak but never opens or lengthens a window.
func (c *deliveryCooldownTracker) recordFailure(token windowToken) {
	if !c.isCurrent(token) {
		return
	}
	if c.inCooldown() { // COVERAGE: defensive; a current token cannot originate inside a window
		return
	}
	if !c.windowEnd.IsZero() {
		// A probe that fails recoverably still ends the window: clear it so a later success is
		// not misreported as the resumption
		c.windowEnd = time.Time{}
		c.epoch++
	}
	c.consecutiveSuccesses = 0
}

// recordUnexpectedFailure records a delivery failure classified as unexpected, opening a cooldown
// window.
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
