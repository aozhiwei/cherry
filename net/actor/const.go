package cherryActor

import (
	"time"

	cerror "github.com/cherry-game/cherry/error"
)

var (
	ErrForbiddenToCallSelf       = cerror.Errorf("SendActorID cannot be equal to TargetActorID")
	ErrForbiddenCreateChildActor = cerror.Errorf("Forbidden create child actor")
	ErrActorIDIsNil              = cerror.Error("actorID is nil.")
)

const (
	LocalName  = "local"
	RemoteName = "remote"
)

const (
	TickUnit     = time.Millisecond
	MaxSleepTick = 1024
)
