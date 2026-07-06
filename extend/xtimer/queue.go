package xtimer

import (
	"sync/atomic"
)

type Transmitter[T any] interface {
	Push(*ListHead[T])
}

type Receiver[T any, U any] interface {
	C() chan U
	Pop() (T, bool)
	flush()
}

type spscQueue[T any, U any] struct {
	c           chan U
	pendingList ListHead[T]
	workList    ListHead[T]
	flag        int32
	more        int32
}

func (p *spscQueue[T, U]) lock() {
	for !atomic.CompareAndSwapInt32(&p.flag, 0, 1) {
	}
}

func (p *spscQueue[T, U]) unlock() {
	atomic.StoreInt32(&p.flag, 0)
}

func (p *spscQueue[T, U]) Push(entry *ListHead[T]) {
	p.lock()
	p.pendingList.AddTail(entry)
	p.unlock()

	if atomic.CompareAndSwapInt32(&p.more, 0, 1) {
		var zero U
		select {
		case p.c <- zero:
		default:
		}
	}
}

func (p *spscQueue[T, U]) C() chan U {
	return p.c
}

func (p *spscQueue[T, U]) Pop() (T, bool) {
	if p.workList.Empty() {
		p.lock()
		p.pendingList.ReplaceInit(&p.workList)
		p.unlock()
	}
	if !p.workList.Empty() {
		first := p.workList.First()
		first.DelInit()
		return first.data, true
	}
	atomic.StoreInt32(&p.more, 0)

	var zero T
	return zero, false
}

func (p *spscQueue[T, U]) flush() {
	if p.workList.Empty() {
		p.lock()
		p.pendingList.ReplaceInit(&p.workList)
		p.unlock()
	}
	if !p.workList.Empty() {
		var zero U
		select {
		case p.c <- zero:
		default:
		}
	}
}

// NewSpScQueue 创建SPSC队列
func NewSpScQueue[T any, U any]() (Transmitter[T], Receiver[T, U]) {
	var zero T
	q := &spscQueue[T, U]{
		c: make(chan U, 1),
	}
	q.pendingList.Init(zero)
	q.workList.Init(zero)
	return q, q
}
