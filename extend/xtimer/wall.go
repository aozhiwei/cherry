package xtimer

import (
	"sync"
	"time"
)

type (
	WallHandle interface {
		Delete()
		Reschedule(duration time.Duration)
		Remain() time.Duration
		Valid() bool
	}

	CornHandle interface {
		Delete()
		Remain() time.Duration
		Valid() bool
	}

	WallTimer interface {
		Clear()
		NewGroup() *WallGroup
		NewChannel(batch int) Channel
		Schedule() Schedule
		AfterGroup(duration time.Duration, cb TimerCb, group *WallGroup, root *WallGroup, sched Channel) WallHandle
		EveryGroup(duration time.Duration, cb TimerCb, group *WallGroup, root *WallGroup, sched Channel) WallHandle
		CronGroup(delay time.Duration, interval time.Duration, cb TimerCb, group *WallGroup, root *WallGroup, sched Channel) CornHandle
	}

	wallTimer struct {
		xt        *timer
		schedule  Schedule
		idleTimer *time.Timer
		timeUnit  time.Duration
		lockFn    func()
		unlockFn  func()
	}

	wallHandle struct {
		h *Handle
		t *wallTimer
	}

	cronHandle struct {
		t        *wallTimer
		next     WallHandle
		nextFire int64
		interval int64
		started  bool
	}

	WallGroup struct {
		g *Group
		t *wallTimer
	}
)

const MaxSleepTick = 1024

// innerNewWallTimer 创建wallTimer内部实现
func innerNewWallTimer(be Backend, timeUnit time.Duration, tickFn func() int64, lockFn func(), unlockFn func()) WallTimer {
	xt := &timer{}
	xt.Init(tickFn, be)
	xt.SetCacheNum(1024)
	idleTimer := time.NewTimer(time.Hour)
	wt := &wallTimer{
		xt:        xt,
		idleTimer: idleTimer,
		timeUnit:  timeUnit,
		lockFn:    lockFn,
		unlockFn:  unlockFn,
	}
	wt.schedule = &schedule{
		idleTimer: idleTimer,
		updateFn:  wt.update,
	}
	if !wt.idleTimer.Stop() {
		select {
		case <-wt.idleTimer.C:
		default:
		}
	}
	return wt
}

func NewWallTimer(be Backend, timeUnit time.Duration, tickFn func() int64) WallTimer {
	return innerNewWallTimer(be, timeUnit, tickFn, func() {}, func() {})
}

func NewSyncWallTimer(be Backend, timeUnit time.Duration, tickFn func() int64) WallTimer {
	mu := sync.Mutex{}
	return innerNewWallTimer(be, timeUnit, tickFn,
		func() { mu.Lock() },
		func() { mu.Unlock() })
}

// NewGroup 创建定时器组
func (p *wallTimer) NewGroup() *WallGroup {
	return &WallGroup{
		g: NewGroup(),
		t: p,
	}
}

// NewChannel 创建带batch的异步调度通道
func (p *wallTimer) NewChannel(batch int) Channel {
	tx, rx := NewSpScQueue[*Node, time.Time]()
	return &channel{
		tx:       tx,
		rx:       rx,
		finishFn: func(n *Node) { p.finish(n) },
		batch:    max(batch, 1),
	}
}

// Schedule 返回根调度器
func (p *wallTimer) Schedule() Schedule {
	return p.schedule
}

func (p *wallTimer) AfterGroup(duration time.Duration, cb TimerCb, group *WallGroup, root *WallGroup, sched Channel) WallHandle {
	defer p.unlockFn()
	p.lockFn()
	h := p.xt.AfterGroup(int64(duration/p.timeUnit), cb, group.raw())
	p.setupHandle(h, root.raw(), sched)
	return &wallHandle{h: h, t: p}
}

func (p *wallTimer) EveryGroup(duration time.Duration, cb TimerCb, group *WallGroup, root *WallGroup, sched Channel) WallHandle {
	defer p.unlockFn()
	p.lockFn()
	h := p.xt.EveryGroup(int64(duration/p.timeUnit), cb, group.raw())
	p.setupHandle(h, root.raw(), sched)
	return &wallHandle{h: h, t: p}
}

func (p *wallTimer) CronGroup(delay time.Duration, interval time.Duration, cb TimerCb, group *WallGroup, root *WallGroup, sched Channel) CornHandle {
	defer p.unlockFn()
	p.lockFn()
	nowTick := p.xt.Tick()
	intervalTick := int64(interval / p.timeUnit)
	ch := &cronHandle{
		t:        p,
		nextFire: nowTick + int64(delay/p.timeUnit),
		interval: intervalTick,
	}
	firstDelay := max(1, ch.nextFire-nowTick)
	ch.next = p.AfterGroup(time.Duration(firstDelay)*p.timeUnit, ch.fire(cb, group, root, sched), group, root, sched)
	return ch
}

// fire 触发cron回调并调度下一次
func (p *cronHandle) fire(cb TimerCb, group *WallGroup, root *WallGroup, sched Channel) func() {
	return func() {
		cb()
		p.nextFire += p.interval
		nowTick := p.t.xt.Tick()
		corrected := max(1, p.nextFire-nowTick)
		if !p.started {
			p.next = p.t.EveryGroup(time.Duration(p.interval)*p.t.timeUnit, p.fire(cb, group, root, sched), group, root, sched)
			p.started = true
		}
		p.next.Reschedule(time.Duration(corrected) * p.t.timeUnit)
	}
}

// Delete 删除cron定时器
func (p *cronHandle) Delete() { p.next.Delete() }

// Remain 返回cron剩余时间
func (p *cronHandle) Remain() time.Duration {
	return time.Duration(p.nextFire-p.t.xt.Tick()) * p.t.timeUnit
}

// Valid 判断cron句柄有效性
func (p *cronHandle) Valid() bool { return p.next != nil && p.next.Valid() }

// update 驱动底层定时器并重置idleTimer
func (p *wallTimer) update() {
	p.xt.Update()
	idleTick := p.xt.NextExpire() - p.xt.Tick()
	idleTick = max(1, min(MaxSleepTick, idleTick))
	if !p.idleTimer.Stop() {
		select {
		case <-p.idleTimer.C:
		default:
		}
	}
	p.idleTimer.Reset(time.Duration(idleTick) * p.timeUnit)
}

// wakeIfCloser 新定时器更近时立即唤醒
func (p *wallTimer) wakeIfCloser(e int64) {
	if p.xt.activeNum <= 1 || e < p.xt.NextExpire() {
		select {
		case <-p.idleTimer.C:
		default:
		}
		p.idleTimer.Reset(p.timeUnit)
	}
}

// setupHandle 配置句柄rootGroup和sched
func (p *wallTimer) setupHandle(h *Handle, root *Group, sched Channel) {
	if root != nil {
		h.setRootGroup(root)
	}
	if sched != nil {
		h.setSched(sched)
	}
	p.wakeIfCloser(h.node.GetExpires())
}

// finish 加锁调用底层timer.finish
func (p *wallTimer) finish(n *Node) {
	defer p.unlockFn()
	p.lockFn()
	p.xt.finish(n.handle)
}

// Clear 清空所有定时器
func (p *wallTimer) Clear() {
	defer p.unlockFn()
	p.lockFn()
	p.xt.Clear()
}

func (p *wallHandle) Delete() {
	defer p.t.unlockFn()
	p.t.lockFn()
	p.h.Delete()
}

func (p *wallHandle) Reschedule(d time.Duration) {
	defer p.t.unlockFn()
	p.t.lockFn()
	p.h.Reschedule(int64(d / p.t.timeUnit))
	p.t.wakeIfCloser(p.h.node.GetExpires())
}

func (p *wallHandle) Remain() time.Duration {
	return time.Duration(p.h.Remain()) * p.t.timeUnit
}

func (p *wallHandle) Valid() bool {
	return p.h.Valid()
}

func (p *WallGroup) Clear() {
	defer p.t.unlockFn()
	p.t.lockFn()
	p.g.Clear()
}

func (p *WallGroup) Empty() bool {
	defer p.t.unlockFn()
	p.t.lockFn()
	return p.g.Empty()
}

func (p *WallGroup) raw() *Group {
	if p == nil {
		return nil
	}
	return p.g
}
