package gateway

import (
	"context"
	"errors"
	"sync"
)

// ErrForegroundPreempted is the cancellation cause of a background session
// interrupted by a foreground request. It does not attest GPU release.
var ErrForegroundPreempted = errors.New("FOREGROUND_PREEMPTED")

type evaluationSessionKey struct{}
type evaluationSession struct {
	gateway          *Gateway
	ctx              context.Context
	cancel           context.CancelCauseFunc
	slot             chan struct{}
	closing          bool
	preemptRequested bool
	inFlight         int
	initialKey       string
	ownedKey         string
}
type inferenceAdmission struct {
	mu                sync.Mutex
	changed           chan struct{}
	foreground        int
	interactiveActive int
	evaluationActive  int
	interactiveCalls  uint64
	evaluationCalls   uint64
	session           *evaluationSession
}
type admissionSnapshot struct {
	foreground, interactiveActive, evaluationActive int
	interactiveCalls, evaluationCalls               uint64
}

func (g *Gateway) admissionState() admissionSnapshot {
	a := &g.admission
	a.mu.Lock()
	defer a.mu.Unlock()
	return admissionSnapshot{a.foreground, a.interactiveActive, a.evaluationActive, a.interactiveCalls, a.evaluationCalls}
}
func (a *inferenceAdmission) signal() {
	if a.changed != nil {
		close(a.changed)
	}
	a.changed = make(chan struct{})
}
func (a *inferenceAdmission) waitChannel() <-chan struct{} {
	if a.changed == nil {
		a.changed = make(chan struct{})
	}
	return a.changed
}
func onceRelease(f func()) func() { var once sync.Once; return func() { once.Do(f) } }

// requestPreemptionLocked records foreground intent under admission.mu before
// cancellation is invoked outside the lock. Close uses this same ordering.
func (s *evaluationSession) requestPreemptionLocked() bool {
	if s.closing {
		return false
	}
	s.preemptRequested = true
	return true
}

// beginInteractive announces arrival before waiting for the legacy semaphore.
// The request remains visible throughout retries and queueing.
func (g *Gateway) beginInteractive(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a := &g.admission
	a.mu.Lock()
	a.foreground++
	a.signal()
	release := onceRelease(func() { a.mu.Lock(); a.foreground--; a.signal(); a.mu.Unlock() })
	for a.session != nil {
		s := a.session
		preempt := s.requestPreemptionLocked()
		ch := a.waitChannel()
		a.mu.Unlock()
		if preempt {
			s.cancel(ErrForegroundPreempted)
		}
		select {
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		case <-ch:
		}
		a.mu.Lock()
	}
	a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// BeginEvaluationSession reserves a single arm's lifecycle window. The caller
// must hold no persistence/service lock while waiting, and must release it.
func (g *Gateway) BeginEvaluationSession(ctx context.Context) (context.Context, func(), error) {
	if ctx.Value(evaluationSessionKey{}) != nil {
		return nil, nil, errors.New("nested evaluation session")
	}
	a := &g.admission
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		a.mu.Lock()
		if a.foreground == 0 && a.session == nil {
			sessionCtx, cancel := context.WithCancelCause(ctx)
			s := &evaluationSession{gateway: g, cancel: cancel, slot: make(chan struct{}, 1)}
			sessionCtx = context.WithValue(sessionCtx, evaluationSessionKey{}, s)
			s.ctx = sessionCtx
			a.session = s
			a.signal()
			a.mu.Unlock()
			closeSession := onceRelease(func() {
				a.mu.Lock()
				cause := context.Canceled
				if s.preemptRequested {
					cause = ErrForegroundPreempted
				}
				s.closing = true
				if s.inFlight == 0 && a.session == s {
					a.session = nil
				}
				a.signal()
				a.mu.Unlock()
				// Cancellation stays outside admission.mu. A foreground intent
				// recorded first still wins if its own cancel has not run yet.
				s.cancel(cause)
			})
			return sessionCtx, closeSession, nil
		}
		ch := a.waitChannel()
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-ch:
		}
	}
}
func (g *Gateway) sessionFrom(ctx context.Context) *evaluationSession {
	s, _ := ctx.Value(evaluationSessionKey{}).(*evaluationSession)
	if s == nil || s.gateway != g {
		return nil
	}
	return s
}

// acquireEvaluationCall protects lifecycle operations as well as inference.
// cleanup permits a detached context only for read/release, never inference.
func (g *Gateway) acquireEvaluationCall(ctx context.Context, cleanup bool) (func(), error) {
	s := g.sessionFrom(ctx)
	if s == nil {
		return nil, errors.New("valid evaluation session required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !cleanup && s.ctx.Err() != nil {
		return nil, context.Cause(s.ctx)
	}
	select {
	case s.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	a := &g.admission
	a.mu.Lock()
	if s.closing || a.session != s || (!cleanup && s.ctx.Err() != nil) {
		a.mu.Unlock()
		<-s.slot
		return nil, errors.New("evaluation session closed or cancelled")
	}
	s.inFlight++
	a.mu.Unlock()
	return onceRelease(func() {
		a.mu.Lock()
		s.inFlight--
		if s.closing && s.inFlight == 0 && a.session == s {
			a.session = nil
		}
		a.signal()
		a.mu.Unlock()
		<-s.slot
	}), nil
}

// countInference brackets the provider invocation, not admission or metadata.
func (g *Gateway) countInference(evaluation bool) func() {
	a := &g.admission
	a.mu.Lock()
	if evaluation {
		a.evaluationActive++
		a.evaluationCalls++
	} else {
		a.interactiveActive++
		a.interactiveCalls++
	}
	a.mu.Unlock()
	return onceRelease(func() {
		a.mu.Lock()
		if evaluation {
			a.evaluationActive--
		} else {
			a.interactiveActive--
		}
		a.signal()
		a.mu.Unlock()
	})
}
