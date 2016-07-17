package promise

import (
	"fmt"
	"sync"
	"time"
)

// Promise is a disposable write-once latch, to act as a synchronization
// barrier to signal completion of some asynchronous operation
// (successful or otherwise).
//
// Functions that operate on this type (IsComplete, Complete,
// Await, AwaitUntil) are idempotent and thread-safe.
type Promise interface {
	ReadOnlyPromise

	// Unblock all goroutines awaiting promise completion.
	Complete(err error)
}

// ReadOnlyPromise is a view of Promise without the Complete method.
type ReadOnlyPromise interface {
	// Returns whether this promise is complete yet, without blocking.
	IsComplete() bool

	// Returns whether this promise completed with an error, without blocking.
	IsError() bool

	// Blocks the caller until the promise is marked complete. This function
	// is equivalent to invoking AwaitUntil() with a zero-length duration.
	// To avoid blocking the caller indefinitely, use AwaitUntil() with a
	// non-zero duration instead.
	Await() error

	// Blocks the caller until the promise is marked complete, or the supplied
	// duration has elapsed. If the promise has not been completed before the
	// await times out, this function returns with a non-nil error. If the
	// supplied duration has zero length, this await will never time out.
	AwaitUntil(timeout time.Duration) error

	// Invokes the supplied function after this promise completes. This function
	// is equivalent to invoking AndThenUntil() with a zero-length duration.
	// To avoid blocking a goroutine indefinitely, use AndThenUntil() with a
	// non-zero duration instead.
	AndThen(f func(error))

	// Invokes the supplied function after this promise completes or times out
	// after the supplied duration. If the supplied duration has zero length,
	// the deferred execution will never time out.
	AndThenUntil(timeout time.Duration, f func(error))
}

// NewPromise returns a new incomplete promise.
func NewPromise() Promise {
	return &promise{
		complete:     false,
		completeChan: make(chan struct{}),
	}
}

type promise struct {
	sync.Mutex

	complete     bool
	err          error
	completeChan chan struct{}
}

func (p *promise) IsComplete() bool {
	return p.complete
}

func (p *promise) IsError() bool {
	return p.IsComplete() && p.err != nil
}

func (p *promise) Complete(err error) {
	p.Lock()
	defer p.Unlock()

	if !p.complete {
		p.complete = true
		p.err = err
		close(p.completeChan)
	}
}

func (p *promise) Await() error {
	return p.AwaitUntil(0 * time.Second)
}

func (p *promise) AwaitUntil(duration time.Duration) error {
	var timeoutChan <-chan time.Time
	if duration.Nanoseconds() > 0 {
		timeoutChan = time.After(duration)
	}

	select {
	case <-p.completeChan:
		return p.err
	case <-timeoutChan:
		return fmt.Errorf("Await timed out for promise after [%s]", duration)
	}
}

func (p *promise) AndThen(f func(error)) {
	p.AndThenUntil(0*time.Nanosecond, f)
}

func (p *promise) AndThenUntil(d time.Duration, f func(error)) {
	go func() {
		f(p.AwaitUntil(d))
	}()
}

// RendezVous is a reciprocal promise that makes it easy for two coordinating
// routines A and B to wait on each other before proceeding.
type RendezVous interface {
	// Returns whether this rendez-vous is complete yet, without blocking.
	IsComplete() bool

	// Complete process A's half of the rendez-vous, and block until process
	// B has done the same.
	A()

	// Complete process B's half of the rendez-vous, and block until process
	// A has done the same.
	B()
}

// NewRendezVous returns a new incomplete rendezvous.
func NewRendezVous() RendezVous {
	return &rendezVous{
		a: NewPromise(),
		b: NewPromise(),
	}
}

type rendezVous struct {
	a Promise
	b Promise
}

func (r *rendezVous) IsComplete() bool {
	return r.a.IsComplete() && r.b.IsComplete()
}

func (r *rendezVous) A() {
	r.a.Complete(nil)
	r.b.Await()
}

func (r *rendezVous) B() {
	r.b.Complete(nil)
	r.a.Await()
}
