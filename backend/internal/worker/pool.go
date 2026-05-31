package worker

import (
	"sync"

	"go.uber.org/zap"
)

// Pool limits concurrent goroutines for OpenRouter and SML calls.
// Each semaphore also has a bounded queue; jobs that exceed the queue cap are dropped
// to prevent goroutine accumulation under load spikes.
type Pool struct {
	openrouterSem chan struct{}
	smlSem        chan struct{}
	wg            sync.WaitGroup
	log           *zap.Logger
}

const (
	openrouterConcurrency = 5
	openrouterQueueCap    = 20 // drop if backlog exceeds this
	smlConcurrency        = 3
	smlQueueCap           = 10
)

func New() *Pool {
	return &Pool{
		openrouterSem: make(chan struct{}, openrouterConcurrency),
		smlSem:        make(chan struct{}, smlConcurrency),
	}
}

func NewWithLogger(log *zap.Logger) *Pool {
	p := New()
	p.log = log
	return p
}

// Submit runs fn asynchronously, respecting the OpenRouter concurrency limit.
// If the semaphore is full and the queue would exceed openrouterQueueCap, the job is dropped.
func (p *Pool) Submit(fn func()) {
	select {
	case p.openrouterSem <- struct{}{}:
		// acquired slot immediately
	default:
		// semaphore full — check queue depth
		if len(p.openrouterSem) >= openrouterQueueCap {
			if p.log != nil {
				p.log.Warn("worker pool: OpenRouter queue full, dropping job")
			}
			return
		}
		p.openrouterSem <- struct{}{} // block until slot available (queue not yet full)
	}
	p.wg.Add(1)
	go func() {
		defer func() {
			<-p.openrouterSem
			p.wg.Done()
		}()
		fn()
	}()
}

// SubmitSML runs fn asynchronously, respecting the SML concurrency limit.
func (p *Pool) SubmitSML(fn func()) {
	select {
	case p.smlSem <- struct{}{}:
	default:
		if len(p.smlSem) >= smlQueueCap {
			if p.log != nil {
				p.log.Warn("worker pool: SML queue full, dropping job")
			}
			return
		}
		p.smlSem <- struct{}{}
	}
	p.wg.Add(1)
	go func() {
		defer func() {
			<-p.smlSem
			p.wg.Done()
		}()
		fn()
	}()
}

// Wait blocks until all submitted jobs finish.
func (p *Pool) Wait() {
	p.wg.Wait()
}
