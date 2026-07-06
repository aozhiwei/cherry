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
	ITimerHandle interface {
		Delete()
		Reschedule(delay time.Duration)
		Remain() time.Duration
		Valid() bool
	}

	ICronHandle interface {
		Delete()
		Remain() time.Duration
		Valid() bool
	}

	ITimerGroup interface {
		Add(delay time.Duration, fn func()) ITimerHandle
		AddOnce(delay time.Duration, fn func()) ITimerHandle
		Clear()
	}

	ITimer interface {
		Add(delay time.Duration, fn func()) ITimerHandle
		AddOnce(delay time.Duration, fn func()) ITimerHandle
		AddFixedHour(hour, minute, second int, fn func()) ICronHandle
		AddFixedMinute(minute, second int, fn func()) ICronHandle
		NewGroup() ITimerGroup
		RemoveAll()
	}
)
