package xtimer

import "time"

type (
	Schedule interface {
		C() <-chan time.Time
		Update()
	}

	Channel interface {
		Schedule
		yield(*Node)
	}

	schedule struct {
		idleTimer *time.Timer
		updateFn  func()
	}

	channel struct {
		tx       Transmitter[*Node]
		rx       Receiver[*Node, time.Time]
		finishFn func(*Node)
		batch    int
	}
)

// C 返回idleTimer的channel
func (p *schedule) C() <-chan time.Time {
	return p.idleTimer.C
}

// Update 调用绑定的updateFn
func (p *schedule) Update() {
	p.updateFn()
}

// C 返回接收端channel
func (p *channel) C() <-chan time.Time {
	return p.rx.C()
}

// Update 批量处理yield节点
func (p *channel) Update() {
	for i := 0; i < p.batch; i++ {
		n, ok := p.rx.Pop()
		if !ok {
			return
		}
		p.invoke(n)
	}
	p.rx.flush()
}

// invoke 执行回调并调用finishFn
func (p *channel) invoke(n *Node) {
	if n.isDeleted() {
		return
	}
	n.timer.safeCall(n)
	if !n.isDeleted() {
		p.finishFn(n)
	}
}

// yield 将到期节点推入发送端
func (p *channel) yield(n *Node) {
	p.tx.Push(&n.Entry)
}
