package concurrency

import (
	"time"
)

type BatchingChannel[T any] interface {
	In() chan<- T
	Out() <-chan []T
}

type batchingChannel[T any] struct {
	input        chan T
	output       chan []T
	buffer       []T
	maxBatchSize int
	maxWaitTime  time.Duration
}

func NewBatchingChannel[T any](capacity int, maxBatchSize int, maxWaitTime time.Duration) BatchingChannel[T] {
	ch := &batchingChannel[T]{
		input:        make(chan T, capacity),
		output:       make(chan []T, 2),
		maxBatchSize: maxBatchSize,
		maxWaitTime:  maxWaitTime,
	}
	go ch.batch()

	return ch
}

func (ch *batchingChannel[T]) In() chan<- T {
	return ch.input
}

func (ch *batchingChannel[T]) Out() <-chan []T {
	return ch.output
}

func (ch *batchingChannel[T]) batch() {
	defer close(ch.output)

	timer := time.NewTimer(ch.maxWaitTime)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	var timerCh <-chan time.Time

	for {
		select {
		case next, ok := <-ch.input:
			if !ok {
				if len(ch.buffer) > 0 {
					ch.output <- ch.buffer
					ch.buffer = nil
				}
				return
			}

			if len(ch.buffer) == 0 {
				timer.Reset(ch.maxWaitTime)
				timerCh = timer.C
			}

			ch.buffer = append(ch.buffer, next)
			if len(ch.buffer) >= ch.maxBatchSize {
				ch.flush(timer, &timerCh)
			}
		case <-timerCh:
			timerCh = nil
			if len(ch.buffer) > 0 {
				ch.flush(timer, &timerCh)
			}
		}
	}
}

func (ch *batchingChannel[T]) flush(timer *time.Timer, timerCh *<-chan time.Time) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	*timerCh = nil

	batch := ch.buffer
	ch.buffer = nil
	ch.output <- batch
}
