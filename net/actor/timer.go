package cherryActor

import (
	"time"

	"github.com/cherry-game/cherry/extend/xtimer"
)

var globalStartTime = time.Now()
var globalTimer xtimer.WallTimer

func getGlobalTick() int64 {
	return time.Since(globalStartTime).Milliseconds()
}

func allocGlobalTimer() xtimer.WallTimer {
	return globalTimer
}

func createOwnTimer() xtimer.WallTimer {
	return xtimer.NewWallTimer(xtimer.NewCascadeWheel(), TickUnit, getGlobalTick)
}

func init() {
	globalTimer = xtimer.NewSyncWallTimer(xtimer.NewCascadeWheel(), TickUnit, getGlobalTick)
	sched := globalTimer.Schedule()
	go func() {
		for {
			<-sched.C()
			sched.Update()
		}
	}()
}
