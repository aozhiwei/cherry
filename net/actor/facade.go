package cherryActor

import (
	"time"

	creflect "github.com/cherry-game/cherry/extend/reflect"
	cfacade "github.com/cherry-game/cherry/facade"
)

type (
	IActorLoader interface {
		load(actor *Actor)
	}
)

type (
	IEvent interface {
		Register(name string, fn IEventFunc, uniqueID ...int64)     // 注册事件
		Registers(names []string, fn IEventFunc, uniqueID ...int64) // 注册多个事件
		Unregister(name string)                                     // 注销事件
	}

	IEventFunc func(cfacade.IEventData) // 接收事件数据时的处理函数
)

type (
	IMailBox interface {
		Register(funcName string, fn interface{}) // 注册执行函数
		GetFuncInfo(funcName string) (*creflect.FuncInfo, bool)
	}
)

type (
	ICornHandle interface {
		Delete()
		Remain() time.Duration
		Valid() bool
	}

	ITimerHandle interface {
		ICornHandle
		Reschedule(d time.Duration)
	}

	ITimerGroup interface {
		After(d time.Duration, fn func()) ITimerHandle
		Every(d time.Duration, fn func()) ITimerHandle
		Hourly(minute, second int, fn func()) ICornHandle
		Daily(hour, minute, second int, fn func()) ICornHandle
		Clear()
	}

	ITimer interface {
		ITimerGroup
		Add(d time.Duration, fn func(), async ...bool) uint64                   // Deprecated: use After/Every instead.
		AddOnce(d time.Duration, fn func(), async ...bool) uint64               // Deprecated: use After instead.
		AddFixedHour(hour, minute, second int, fn func(), async ...bool) uint64 // Deprecated: use Daily instead.
		AddFixedMinute(minute, second int, fn func(), async ...bool) uint64     // Deprecated: use Hourly instead.
		AddSchedule(s ITimerSchedule, fn func(), async ...bool) uint64          // Deprecated.
		Remove(id uint64)                                                       // Deprecated: use Handle.Delete() instead.
		RemoveAll()                                                             // Deprecated: use Clear() instead.
		NewGroup() ITimerGroup
		WithOwnTimer() bool
	}

	// Deprecated
	ITimerSchedule interface {
		Next(time.Time) time.Time
	}
)
