package xtimer

import (
	"runtime"
	"testing"
	"time"
)

func newTestTimer() (*Timer, *int64) {
	var now int64
	t := &Timer{}
	t.Init(func() int64 { return now })
	return t, &now
}

// 测试一次性定时器：到期前不触发，到期后仅触发一次
func TestAfter(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	timer.After(10, func() { count++ })

	*now = 9
	timer.Update()
	if count != 0 {
		t.Fatal("should not fire at tick 9")
	}

	*now = 10
	timer.Update()
	if count != 1 {
		t.Fatalf("should fire once, got %d", count)
	}

	*now = 20
	timer.Update()
	if count != 1 {
		t.Fatal("After should only fire once")
	}
}

// 测试间隔定时器：每次到期后自动重置，可重复触发
func TestEvery(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	timer.Every(5, func() { count++ })

	*now = 5
	timer.Update()
	if count != 1 {
		t.Fatalf("first fire: got %d", count)
	}

	*now = 10
	timer.Update()
	if count != 2 {
		t.Fatalf("second fire: got %d", count)
	}

	*now = 15
	timer.Update()
	if count != 3 {
		t.Fatalf("third fire: got %d", count)
	}
}

// 测试句柄删除：删除后定时器不再触发，Valid返回false
func TestHandleDelete(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	h := timer.After(10, func() { count++ })

	*now = 5
	timer.Update()

	h.Delete()
	if h.Valid() {
		t.Fatal("handle should be expired after Delete")
	}

	*now = 10
	timer.Update()
	if count != 0 {
		t.Fatal("deleted timer should not fire")
	}
}

// 测试句柄重新安排：修改到期时间后按新时间触发
func TestHandleReschedule(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	h := timer.After(10, func() { count++ })

	h.Reschedule(20)

	*now = 10
	timer.Update()
	if count != 0 {
		t.Fatal("rescheduled timer should not fire at old time")
	}

	*now = 20
	timer.Update()
	if count != 1 {
		t.Fatalf("rescheduled timer should fire at new time, got %d", count)
	}
}

// 测试句柄剩余时间：返回距离到期的滴答数
func TestHandleRemain(t *testing.T) {
	timer, now := newTestTimer()

	h := timer.After(10, func() {})

	*now = 2
	timer.Update()

	r := h.Remain()
	if r != 8 {
		t.Fatalf("remain should be 8, got %d", r)
	}
}

// 测试定时器组清除：Clear后组内所有定时器不触发
func TestGroupClear(t *testing.T) {
	timer, now := newTestTimer()
	g := NewGroup()

	var count int
	timer.AfterGroup(5, func() { count++ }, g)
	timer.AfterGroup(10, func() { count++ }, g)

	g.Clear()

	*now = 10
	timer.Update()
	if count != 0 {
		t.Fatalf("cleared group timers should not fire, got %d", count)
	}
}

// 测试重入添加：回调中创建新定时器，新定时器正常触发
func TestReentrantAdd(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	timer.After(1, func() {
		count++
		timer.After(2, func() { count++ })
	})

	*now = 1
	timer.Update()
	if count != 1 {
		t.Fatalf("first timer should fire, count=%d", count)
	}

	*now = 2
	timer.Update()
	if count != 1 {
		t.Fatalf("nested timer at tick 1 should not fire yet, count=%d", count)
	}

	*now = 3
	timer.Update()
	if count != 2 {
		t.Fatalf("nested timer should fire at tick 3, count=%d", count)
	}
}

// 测试重入删除自身：回调中Handle.Delete删除当前定时器，不再重复触发
func TestReentrantDeleteSelf(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	var h *Handle
	h = timer.After(1, func() {
		count++
		h.Delete()
	})

	*now = 1
	timer.Update()
	if count != 1 {
		t.Fatalf("should fire, count=%d", count)
	}

	*now = 2
	timer.Update()
	if count != 1 {
		t.Fatal("deleted timer should not fire again")
	}
}

// 测试重入删除其他：回调中删除另一个定时器，被删除的不触发
func TestReentrantDeleteOther(t *testing.T) {
	timer, now := newTestTimer()

	var count1, count2 int
	var h2 *Handle

	timer.After(1, func() {
		count1++
		h2.Delete()
	})

	h2 = timer.After(2, func() { count2++ })

	*now = 2
	timer.Update()
	if count1 != 1 {
		t.Fatalf("timer1 should fire, count1=%d", count1)
	}
	if count2 != 0 {
		t.Fatal("deleted timer2 should not fire")
	}
}

// 测试重入调整自身：回调中Reschedule自身，按新时间触发
func TestReentrantRescheduleSelf(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	var h *Handle
	h = timer.Every(10, func() {
		count++
		if count == 1 {
			h.Reschedule(5)
		}
	})

	*now = 10
	timer.Update()
	if count != 1 {
		t.Fatalf("should fire at tick 10, count=%d", count)
	}

	*now = 15
	timer.Update()
	if count != 2 {
		t.Fatalf("rescheduled to 5, should fire at 15, count=%d", count)
	}
}

// 测试重入调整其他：回调中Reschedule另一个定时器，被调整的按新时间触发
func TestReentrantRescheduleOther(t *testing.T) {
	timer, now := newTestTimer()

	var count1, count2 int
	var h2 *Handle

	timer.After(1, func() {
		count1++
		h2.Reschedule(5)
	})

	h2 = timer.After(10, func() { count2++ })

	*now = 1
	timer.Update()

	*now = 6
	timer.Update()
	if count2 != 1 {
		t.Fatalf("rescheduled timer should fire at tick 6, count2=%d", count2)
	}
}

// 测试句柄有效性：正常/删除后/nil三种状态
func TestHandleValid(t *testing.T) {
	timer, _ := newTestTimer()

	h := timer.After(10, func() {})
	if !h.Valid() {
		t.Fatal("valid handle should return true for Valid")
	}

	h.Delete()
	if h.Valid() {
		t.Fatal("deleted handle should return false for Valid")
	}

	var nilH *Handle
	if nilH.Valid() {
		t.Fatal("nil handle should return false for Valid")
	}
}

// 测试跨轮触发：滴答跨越时间轮一层容量后正常触发
func TestTickOverflow(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	timer.Every(300, func() { count++ })

	*now = 300
	timer.Update()
	if count != 1 {
		t.Fatalf("should fire once at 300, count=%d", count)
	}

	*now = 600
	timer.Update()
	if count != 2 {
		t.Fatalf("should fire twice at 600, count=%d", count)
	}
}

// 测试追赶模式：逐滴答推进确保触发次数正确
func TestTimerTickCatchUp(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	timer.Every(1, func() { count++ })

	for i := int64(1); i <= 100; i++ {
		*now = i
		timer.Update()
	}
	if count != 100 {
		t.Fatalf("should fire 100 times, count=%d", count)
	}
}

// 压力测试：10000个After定时器全部正常触发
func TestStressManyTimers(t *testing.T) {
	timer, now := newTestTimer()

	const N = 10000
	handles := make([]*Handle, N)
	var count int64

	for i := 0; i < N; i++ {
		d := int64((i % 100) + 1)
		h := timer.After(d, func() { count++ })
		handles[i] = h
	}

	for tick := int64(1); tick <= 200; tick++ {
		*now = tick
		timer.Update()
	}

	if count != int64(N) {
		t.Fatalf("all %d timers should fire, got %d", N, count)
	}
}

// 压力测试：Every定时器逐滴答推进触发20次
func TestStressEveryTimer(t *testing.T) {
	timer, now := newTestTimer()

	var count int64
	timer.Every(10, func() { count++ })

	for i := int64(1); i <= 200; i++ {
		*now = i
		timer.Update()
	}
	if count != 20 {
		t.Fatalf("every 10 over 200 ticks = 20 fires, got %d", count)
	}
}

// 压力测试：重入链式添加定时器，累计触发100次后停止
func TestStressReentrantAddDelete(t *testing.T) {
	timer, now := newTestTimer()

	var count int32

	var addTimer func(d int64)
	addTimer = func(d int64) {
		timer.After(d, func() {
			count++
			if count < 100 {
				addTimer(1)
			}
		})
	}

	addTimer(1)

	for tick := int64(1); tick <= 500; tick++ {
		*now = tick
		timer.Update()
		if count >= 100 {
			break
		}
	}

	if count < 100 {
		t.Fatalf("reentrant chain should reach 100, got %d", count)
	}
}

// 测试获取最近到期滴答数
func TestGetMinExpires(t *testing.T) {
	timer, now := newTestTimer()

	timer.After(50, func() {})
	*now = 10
	timer.Update()

	minExpires := timer.GetMinExpires()
	if minExpires != 50 {
		t.Fatalf("minExpires should be 50, got %d", minExpires)
	}
}

// 测试PanicHandler：回调panic被吞掉，定时器正常删除
func TestPanicHandlerSwallow(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	timer.SetOnPanic(func(cb *Handle, recovered any) bool {
		return true
	})

	timer.After(10, func() {
		count++
		panic("oops")
	})

	*now = 10
	timer.Update()
	if count != 1 {
		t.Fatalf("should fire despite panic, count=%d", count)
	}
}

// 测试PanicHandler：回调panic被重新抛出
func TestPanicHandlerRethrow(t *testing.T) {
	timer, now := newTestTimer()

	timer.SetOnPanic(func(cb *Handle, recovered any) bool {
		return false
	})

	timer.After(10, func() { panic("fatal") })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("should re-panic")
		}
	}()

	*now = 10
	timer.Update()
}


// 基准测试：Every(1)定时器驱动性能
func BenchmarkStressTimer(b *testing.B) {
	var now int64
	timer := &Timer{}
	timer.Init(func() int64 { return now })

	var count int64
	timer.Every(1, func() { count++ })

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now = int64(i + 1)
		timer.Update()
	}
}

// 百万定时器同一时刻全流程耗时测试

// 基准测试：After定时器创建和驱动
func BenchmarkAfterAdd(b *testing.B) {
	var now int64
	timer := &Timer{}
	timer.Init(func() int64 { return now })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		timer.After(10, func() {})
		now++
		timer.Update()
	}
}

// 十万定时器全流程耗时测试（1000次平均，含PreAlloc对比）
func TestHundredThousandTimersCascade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	const N = 100_000
	const runs = 1000
	var totalCreate, totalCascade, totalFire, totalCascadeFire int64

	// Phase 1: without GC
	for r := 0; r < runs; r++ {
		var now int64
		timer := &Timer{}
		timer.Init(func() int64 { return now })
		start := time.Now()
		for i := 0; i < N; i++ {
			timer.After(257, func() {})
		}
		createTime := time.Since(start)
		now = 255
		timer.Update()
		now = 256
		start = time.Now()
		timer.Update()
		cascadeTime := time.Since(start)
		var fired int64
		var now2 int64
		timer2 := &Timer{}
		timer2.Init(func() int64 { return now2 })
		for i := 0; i < N; i++ {
			timer2.After(256, func() { fired++ })
		}
		now2 = 255
		timer2.Update()
		now2 = 256
		start = time.Now()
		timer2.Update()
		cascadeFireTime := time.Since(start)
		fireTime := cascadeFireTime - cascadeTime
		totalCreate += createTime.Nanoseconds()
		totalCascade += cascadeTime.Nanoseconds()
		totalFire += fireTime.Nanoseconds()
		totalCascadeFire += cascadeFireTime.Nanoseconds()
		if fired != N {
			t.Fatalf("expected %d fired, got %d", N, fired)
		}
	}
	avgCreate := time.Duration(totalCreate / runs)
	avgCascade := time.Duration(totalCascade / runs)
	avgFire := time.Duration(totalFire / runs)
	avgCascadeFire := time.Duration(totalCascadeFire / runs)
	t.Logf("--- without GC (%d-run avg) ---", runs)
	t.Logf("create: %.3fms  cascade: %.3fms  cas+fire: %.3fms  fire: %.3fms",
		float64(avgCreate.Microseconds())/1000, float64(avgCascade.Microseconds())/1000,
		float64(avgCascadeFire.Microseconds())/1000, float64(avgFire.Microseconds())/1000)

	// Phase 2: with GC
	totalCreate, totalCascade, totalFire, totalCascadeFire = 0, 0, 0, 0
	for r := 0; r < runs; r++ {
		runtime.GC()
		var now int64
		timer := &Timer{}
		timer.Init(func() int64 { return now })
		start := time.Now()
		for i := 0; i < N; i++ {
			timer.After(257, func() {})
		}
		createTime := time.Since(start)
		now = 255
		timer.Update()
		now = 256
		start = time.Now()
		timer.Update()
		cascadeTime := time.Since(start)
		var fired int64
		var now2 int64
		timer2 := &Timer{}
		timer2.Init(func() int64 { return now2 })
		for i := 0; i < N; i++ {
			timer2.After(256, func() { fired++ })
		}
		now2 = 255
		timer2.Update()
		now2 = 256
		start = time.Now()
		timer2.Update()
		cascadeFireTime := time.Since(start)
		fireTime := cascadeFireTime - cascadeTime
		totalCreate += createTime.Nanoseconds()
		totalCascade += cascadeTime.Nanoseconds()
		totalFire += fireTime.Nanoseconds()
		totalCascadeFire += cascadeFireTime.Nanoseconds()
		if fired != N {
			t.Fatalf("expected %d fired, got %d", N, fired)
		}
	}
	avgCreate = time.Duration(totalCreate / runs)
	avgCascade = time.Duration(totalCascade / runs)
	avgFire = time.Duration(totalFire / runs)
	avgCascadeFire = time.Duration(totalCascadeFire / runs)
	t.Logf("--- with GC (%d-run avg) ---", runs)
	t.Logf("create: %.3fms  cascade: %.3fms  cas+fire: %.3fms  fire: %.3fms",
		float64(avgCreate.Microseconds())/1000, float64(avgCascade.Microseconds())/1000,
		float64(avgCascadeFire.Microseconds())/1000, float64(avgFire.Microseconds())/1000)

	// Phase 3: with GC + PreAlloc
	totalCreate, totalCascade, totalFire, totalCascadeFire = 0, 0, 0, 0
	for r := 0; r < runs; r++ {
		runtime.GC()
		var now int64
		timer := &Timer{}
		timer.Init(func() int64 { return now })
		timer.PreAlloc(N)
		start := time.Now()
		for i := 0; i < N; i++ {
			timer.After(257, func() {})
		}
		createTime := time.Since(start)
		now = 255
		timer.Update()
		now = 256
		start = time.Now()
		timer.Update()
		cascadeTime := time.Since(start)
		var fired int64
		var now2 int64
		timer2 := &Timer{}
		timer2.Init(func() int64 { return now2 })
		timer2.PreAlloc(N)
		for i := 0; i < N; i++ {
			timer2.After(256, func() { fired++ })
		}
		now2 = 255
		timer2.Update()
		now2 = 256
		start = time.Now()
		timer2.Update()
		cascadeFireTime := time.Since(start)
		fireTime := cascadeFireTime - cascadeTime
		totalCreate += createTime.Nanoseconds()
		totalCascade += cascadeTime.Nanoseconds()
		totalFire += fireTime.Nanoseconds()
		totalCascadeFire += cascadeFireTime.Nanoseconds()
		if fired != N {
			t.Fatalf("expected %d fired, got %d", N, fired)
		}
	}
	avgCreate = time.Duration(totalCreate / runs)
	avgCascade = time.Duration(totalCascade / runs)
	avgFire = time.Duration(totalFire / runs)
	avgCascadeFire = time.Duration(totalCascadeFire / runs)
	t.Logf("--- with GC+PreAlloc (%d-run avg) ---", runs)
	t.Logf("create: %.3fms  cascade: %.3fms  cas+fire: %.3fms  fire: %.3fms",
		float64(avgCreate.Microseconds())/1000, float64(avgCascade.Microseconds())/1000,
		float64(avgCascadeFire.Microseconds())/1000, float64(avgFire.Microseconds())/1000)
}

// 百万定时器全流程耗时测试（1000次平均，含PreAlloc对比）
func TestMillionTimersCascade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping million-timer test in short mode")
	}
	const N = 1_000_000
	const runs = 10
	var totalCreate, totalCascade, totalFire, totalCascadeFire int64

	// Phase 1: without GC
	for r := 0; r < runs; r++ {
		var now int64
		timer := &Timer{}
		timer.Init(func() int64 { return now })
		start := time.Now()
		for i := 0; i < N; i++ {
			timer.After(257, func() {})
		}
		createTime := time.Since(start)
		now = 255
		timer.Update()
		now = 256
		start = time.Now()
		timer.Update()
		cascadeTime := time.Since(start)
		var fired int64
		var now2 int64
		timer2 := &Timer{}
		timer2.Init(func() int64 { return now2 })
		for i := 0; i < N; i++ {
			timer2.After(256, func() { fired++ })
		}
		now2 = 255
		timer2.Update()
		now2 = 256
		start = time.Now()
		timer2.Update()
		cascadeFireTime := time.Since(start)
		fireTime := cascadeFireTime - cascadeTime
		totalCreate += createTime.Nanoseconds()
		totalCascade += cascadeTime.Nanoseconds()
		totalFire += fireTime.Nanoseconds()
		totalCascadeFire += cascadeFireTime.Nanoseconds()
		if fired != N {
			t.Fatalf("expected %d fired, got %d", N, fired)
		}
	}
	avgCreate := time.Duration(totalCreate / runs)
	avgCascade := time.Duration(totalCascade / runs)
	avgFire := time.Duration(totalFire / runs)
	avgCascadeFire := time.Duration(totalCascadeFire / runs)
	t.Logf("--- without GC (%d-run avg) ---", runs)
	t.Logf("create: %.3fms  cascade: %.3fms  cas+fire: %.3fms  fire: %.3fms",
		float64(avgCreate.Microseconds())/1000, float64(avgCascade.Microseconds())/1000,
		float64(avgCascadeFire.Microseconds())/1000, float64(avgFire.Microseconds())/1000)

	// Phase 2: with GC
	totalCreate, totalCascade, totalFire, totalCascadeFire = 0, 0, 0, 0
	for r := 0; r < runs; r++ {
		runtime.GC()
		var now int64
		timer := &Timer{}
		timer.Init(func() int64 { return now })
		start := time.Now()
		for i := 0; i < N; i++ {
			timer.After(257, func() {})
		}
		createTime := time.Since(start)
		now = 255
		timer.Update()
		now = 256
		start = time.Now()
		timer.Update()
		cascadeTime := time.Since(start)
		var fired int64
		var now2 int64
		timer2 := &Timer{}
		timer2.Init(func() int64 { return now2 })
		for i := 0; i < N; i++ {
			timer2.After(256, func() { fired++ })
		}
		now2 = 255
		timer2.Update()
		now2 = 256
		start = time.Now()
		timer2.Update()
		cascadeFireTime := time.Since(start)
		fireTime := cascadeFireTime - cascadeTime
		totalCreate += createTime.Nanoseconds()
		totalCascade += cascadeTime.Nanoseconds()
		totalFire += fireTime.Nanoseconds()
		totalCascadeFire += cascadeFireTime.Nanoseconds()
		if fired != N {
			t.Fatalf("expected %d fired, got %d", N, fired)
		}
	}
	avgCreate = time.Duration(totalCreate / runs)
	avgCascade = time.Duration(totalCascade / runs)
	avgFire = time.Duration(totalFire / runs)
	avgCascadeFire = time.Duration(totalCascadeFire / runs)
	t.Logf("--- with GC (%d-run avg) ---", runs)
	t.Logf("create: %.3fms  cascade: %.3fms  cas+fire: %.3fms  fire: %.3fms",
		float64(avgCreate.Microseconds())/1000, float64(avgCascade.Microseconds())/1000,
		float64(avgCascadeFire.Microseconds())/1000, float64(avgFire.Microseconds())/1000)

	// Phase 3: with GC + PreAlloc
	totalCreate, totalCascade, totalFire, totalCascadeFire = 0, 0, 0, 0
	for r := 0; r < runs; r++ {
		runtime.GC()
		var now int64
		timer := &Timer{}
		timer.Init(func() int64 { return now })
		timer.PreAlloc(N)
		start := time.Now()
		for i := 0; i < N; i++ {
			timer.After(257, func() {})
		}
		createTime := time.Since(start)
		now = 255
		timer.Update()
		now = 256
		start = time.Now()
		timer.Update()
		cascadeTime := time.Since(start)
		var fired int64
		var now2 int64
		timer2 := &Timer{}
		timer2.Init(func() int64 { return now2 })
		timer2.PreAlloc(N)
		for i := 0; i < N; i++ {
			timer2.After(256, func() { fired++ })
		}
		now2 = 255
		timer2.Update()
		now2 = 256
		start = time.Now()
		timer2.Update()
		cascadeFireTime := time.Since(start)
		fireTime := cascadeFireTime - cascadeTime
		totalCreate += createTime.Nanoseconds()
		totalCascade += cascadeTime.Nanoseconds()
		totalFire += fireTime.Nanoseconds()
		totalCascadeFire += cascadeFireTime.Nanoseconds()
		if fired != N {
			t.Fatalf("expected %d fired, got %d", N, fired)
		}
	}
	avgCreate = time.Duration(totalCreate / runs)
	avgCascade = time.Duration(totalCascade / runs)
	avgFire = time.Duration(totalFire / runs)
	avgCascadeFire = time.Duration(totalCascadeFire / runs)
	t.Logf("--- with GC+PreAlloc (%d-run avg) ---", runs)
	t.Logf("create: %.3fms  cascade: %.3fms  cas+fire: %.3fms  fire: %.3fms",
		float64(avgCreate.Microseconds())/1000, float64(avgCascade.Microseconds())/1000,
		float64(avgCascadeFire.Microseconds())/1000, float64(avgFire.Microseconds())/1000)
}
