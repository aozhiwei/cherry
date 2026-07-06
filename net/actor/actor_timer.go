package cherryActor

import (
	"time"

	ctime "github.com/cherry-game/cherry/extend/time"
	"github.com/cherry-game/cherry/extend/xtimer"
)

type (
	legacyTimerHandle interface {
		Delete()
	}

	actorTimer struct {
		*timerGroup
		thisActor     *Actor // this actor
		isGlobalTimer bool
		t             xtimer.WallTimer
		ownSched      xtimer.Schedule
		globalSched   xtimer.Channel

		curID     uint64
		timersMap map[uint64]legacyTimerHandle
	}

	timerGroup struct {
		owner *actorTimer
		g     *xtimer.WallGroup
	}
)

func newActorTimer(thisActor *Actor) *actorTimer {
	at := &actorTimer{
		thisActor:     thisActor,
		isGlobalTimer: true,
		timersMap:     make(map[uint64]legacyTimerHandle),
	}
	at.t = allocGlobalTimer()
	at.globalSched = at.t.NewChannel(64)
	at.timerGroup = &timerGroup{
		owner: at,
		g:     at.t.NewGroup(),
	}
	return at
}

func (p *actorTimer) WithOwnTimer() bool {
	if !p.isGlobalTimer {
		return true
	}
	if !p.timerGroup.g.Empty() {
		return false
	}
	p.isGlobalTimer = false
	p.globalSched = nil
	p.t = createOwnTimer()
	p.ownSched = p.t.Schedule()
	return true
}

func (p *actorTimer) onStop() {
	p.RemoveAll()
	p.thisActor = nil
	p.t = nil
	p.globalSched = nil
	p.ownSched = nil
}

func (p *actorTimer) C() <-chan time.Time {
	if p.isGlobalTimer {
		return p.globalSched.C()
	} else {
		return p.ownSched.C()
	}
}

func (p *actorTimer) wrapperAsyncFn(fn func(), async ...bool) func() {
	if len(async) > 0 {
		return func() {
			go fn()
		}
	} else {
		return fn
	}
}

func (p *actorTimer) Add(d time.Duration, fn func(), async ...bool) uint64 {
	return p.addOldHandle(p.Every(d, p.wrapperAsyncFn(fn, async...)))
}

func (p *actorTimer) AddOnce(d time.Duration, fn func(), async ...bool) uint64 {
	var id uint64
	id = p.addOldHandle(p.After(d, func() {
		p.wrapperAsyncFn(fn, async...)()
		delete(p.timersMap, id)
	}))
	return id
}

func (p *actorTimer) AddFixedHour(hour, minute, second int, fn func(), async ...bool) uint64 {
	return p.addOldHandle(p.Daily(hour, minute, second, p.wrapperAsyncFn(fn, async...)))
}

func (p *actorTimer) AddFixedMinute(minute, second int, fn func(), async ...bool) uint64 {
	return p.addOldHandle(p.Hourly(minute, second, fn))
}

func (p *actorTimer) AddSchedule(s ITimerSchedule, fn func(), async ...bool) uint64 {
	return 0
}

func (p *actorTimer) NewGroup() ITimerGroup {
	return &timerGroup{
		owner: p,
		g:     p.t.NewGroup(),
	}
}

func (p *actorTimer) Remove(id uint64) {
	if funcItem, found := p.timersMap[id]; found {
		funcItem.Delete()
		delete(p.timersMap, id)
	}
}

func (p *actorTimer) RemoveAll() {
	p.timersMap = make(map[uint64]legacyTimerHandle)
	p.timerGroup.Clear()
}

func (p *actorTimer) addOldHandle(h legacyTimerHandle) uint64 {
	if h == nil {
		return 0
	}
	id := p.nextID()
	p.timersMap[id] = h
	return id
}

func (p *actorTimer) nextID() uint64 {
	p.curID++
	return p.curID
}

func (p *actorTimer) processTimer() {
	if p.isGlobalTimer {
		p.globalSched.Update()
	} else {
		p.ownSched.Update()
	}
}

func (p *timerGroup) After(d time.Duration, fn func()) ITimerHandle {
	return p.owner.t.AfterGroup(d, fn, p.g, p.root(), p.owner.globalSched)
}

func (p *timerGroup) Every(d time.Duration, fn func()) ITimerHandle {
	return p.owner.t.EveryGroup(d, fn, p.g, p.root(), p.owner.globalSched)
}

func (p *timerGroup) Hourly(minute, second int, fn func()) ICornHandle {
	interval := time.Hour
	now := ctime.Now()
	target := time.Date(now.Year(), now.Time.Month(), now.Day(), now.Hour(), minute, second, 0, now.Location())
	if !target.After(now.Time) {
		target = target.Add(interval)
	}

	delay := max(time.Until(target), 1)
	return p.owner.t.CronGroup(delay, interval, fn, p.g, p.root(), p.owner.globalSched)
}

func (p *timerGroup) Daily(hour, minute, second int, fn func()) ICornHandle {
	return nil
}

func (p *timerGroup) Clear() {
	p.g.Clear()
}

func (p *timerGroup) root() *xtimer.WallGroup {
	if p == p.owner.timerGroup {
		return nil
	}
	return p.g
}
