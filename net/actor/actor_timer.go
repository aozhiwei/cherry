package cherryActor

import (
	"math"
	"time"

	ctime "github.com/cherry-game/cherry/extend/time"
	"github.com/cherry-game/cherry/extend/xtimer"
)

type (
	actorTimer struct {
		start          time.Time
		xt             *xtimer.Timer
		nextWeakupTick int64
		idleTimer      *time.Timer
	}

	actorGroup struct {
		t *actorTimer
		g *xtimer.Group
	}

	actorCron struct {
		next *xtimer.Handle
	}

	actorHandle struct {
		*xtimer.Handle
	}
)

func newTimer() *actorTimer {
	idleTimer := time.NewTimer(time.Hour)
	idleTimer.Stop()
	at := &actorTimer{
		start:          time.Now(),
		idleTimer:      idleTimer,
		nextWeakupTick: math.MaxInt64,
	}
	at.resetIdle()
	return at
}

func (p *actorTimer) Update() {
	if p.xt != nil {
		p.xt.Update()
	}
	p.resetIdle()
}

func (p *actorTimer) SyncWeakup() {
	if p.xt == nil {
		return
	}
	idle := p.xt.GetMinExpires()
	tick := p.getTick()
	if tick+idle <= p.nextWeakupTick {
		p.idleTimer.Reset(time.Duration(idle) * time.Millisecond)
		p.nextWeakupTick = tick + idle
	}
}

func (p *actorTimer) ensureTimer() {
	if p.xt == nil {
		p.xt = &xtimer.Timer{}
		p.xt.Init(p.getTick)
	}
}

func (p *actorTimer) getTick() int64 {
	return time.Since(p.start).Milliseconds()
}

func (p *actorTimer) C() <-chan time.Time {
	return p.idleTimer.C
}

func (p *actorTimer) onStop() {
	if p.xt != nil {
		p.xt.Clear()
	}
	p.idleTimer.Stop()
}

func (p *actorTimer) toTicks(d time.Duration) int64 {
	return int64(d / time.Millisecond)
}

// ITimer

func (p *actorTimer) Add(delay time.Duration, fn func()) ITimerHandle {
	p.ensureTimer()
	ticks := p.toTicks(delay)
	if ticks < 1 || fn == nil {
		return nil
	}
	return &actorHandle{p.xt.Every(ticks, xtimer.TimerCb(fn))}
}

func (p *actorTimer) AddOnce(delay time.Duration, fn func()) ITimerHandle {
	p.ensureTimer()
	ticks := p.toTicks(delay)
	if ticks < 1 || fn == nil {
		return nil
	}
	return &actorHandle{p.xt.After(ticks, xtimer.TimerCb(fn))}
}

func (p *actorTimer) AddFixedHour(hour, minute, second int, fn func()) ICronHandle {
	p.ensureTimer()
	now := ctime.Now()
	target := time.Date(now.Year(), now.Time.Month(), now.Day(), hour, minute, second, 0, now.Location())
	return p.addCron(target, ctime.HoursPerDay*time.Hour, fn)
}

func (p *actorTimer) AddFixedMinute(minute, second int, fn func()) ICronHandle {
	p.ensureTimer()
	now := ctime.Now()
	target := time.Date(now.Year(), now.Time.Month(), now.Day(), now.Hour(), minute, second, 0, now.Location())
	return p.addCron(target, time.Hour, fn)
}

// addCron 添加一个wall-clock对齐的cron式定时器。
// 每次回调结束后重新计算与下一个目标时刻的距离，
// 确保长期运行不被fn()的执行耗时漂移。
func (p *actorTimer) addCron(target time.Time, interval time.Duration, fn func()) ICronHandle {
	if fn == nil {
		return nil
	}
	if !target.After(ctime.Now().Time) {
		target = target.Add(interval)
	}

	t := &actorCron{}

	var scheduleNext func()
	scheduleNext = func() {
		delay := max(p.toTicks(time.Until(target)), 1)
		t.next = p.xt.After(delay, func() {
			fn()
			if t.next.Valid() {
				target = target.Add(interval)
				scheduleNext()
			}
		})
	}
	scheduleNext()
	return t
}

func (p *actorTimer) NewGroup() ITimerGroup {
	return &actorGroup{t: p, g: xtimer.NewGroup()}
}

// actorGroup — thin adapter to match ITimerGroup return types

func (g *actorGroup) Add(delay time.Duration, fn func()) ITimerHandle {
	return g.t.Add(delay, xtimer.TimerCb(fn))
}

func (g *actorGroup) AddOnce(delay time.Duration, fn func()) ITimerHandle {
	return g.t.AddOnce(delay, xtimer.TimerCb(fn))
}

func (g *actorGroup) Clear() {
	g.g.Clear()
}

func (p *actorTimer) RemoveAll() {
	if p.xt != nil {
		p.xt.Clear()
		p.resetIdle()
	}
}

// actorCron

func (p *actorCron) Delete() {
	p.next.Delete()
}

func (p *actorCron) Remain() time.Duration {
	return time.Duration(p.next.Remain()) * time.Millisecond
}

func (p *actorCron) Valid() bool {
	return p.next.Valid()
}

// actorHandle — adapts xtimer.Handle (int64 ticks) to ITimerHandle (time.Duration)

func (p *actorHandle) Reschedule(delay time.Duration) {
	p.Handle.Reschedule(int64(delay / time.Millisecond))
}

func (p *actorHandle) Remain() time.Duration {
	return time.Duration(p.Handle.Remain()) * time.Millisecond
}

// internal

func (p *actorTimer) idleDuration() time.Duration {
	idle := p.xt.GetMinExpires()
	if idle <= 0 {
		return time.Millisecond
	}
	if idle > math.MaxInt64/int64(time.Millisecond) {
		return time.Hour
	}
	return time.Duration(idle) * time.Millisecond
}

func (p *actorTimer) resetIdle() {
	if !p.idleTimer.Stop() {
		select {
		case <-p.idleTimer.C:
		default:
		}
	}
	p.idleTimer.Reset(p.idleDuration())
}
