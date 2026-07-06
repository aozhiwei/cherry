package xtimer

import (
	"testing"
	"time"
)

func newTestTimer() (*Timer, *int64) {
	var now int64
	t := &Timer{}
	t.Init(func() int64 { return now }, 256)
	return t, &now
}

// TestAfter 一次性定时器：到期前不触发，到期后仅触发一次
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

// TestEvery 间隔定时器：每次到期后自动重置，可重复触发
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

// TestHandleDelete 句柄删除：删除后定时器不再触发，Valid返回false
func TestHandleDelete(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	h := timer.After(10, func() { count++ })

	h.Delete()
	if h.Valid() {
		t.Fatal("handle should be invalid after Delete")
	}

	*now = 10
	timer.Update()
	if count != 0 {
		t.Fatal("deleted timer should not fire")
	}
}

// TestHandleReschedule 句柄重新安排：修改到期时间后按新时间触发
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

// TestHandleRemain 句柄剩余时间：返回距离到期的滴答数
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

// TestGroupClear 定时器组清除：Clear后组内所有定时器不触发
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

// TestReentrantAdd 重入添加：回调中创建新定时器，新定时器正常触发
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

// TestHandleValid 句柄有效性：正常/删除后/nil三种状态
func TestHandleValid(t *testing.T) {
	timer, _ := newTestTimer()

	h := timer.After(10, func() {})
	if !h.Valid() {
		t.Fatal("valid handle should return true")
	}

	h.Delete()
	if h.Valid() {
		t.Fatal("deleted handle should return false")
	}

	var nilH *Handle
	if nilH.Valid() {
		t.Fatal("nil handle should return false")
	}
}

// TestTickOverflow 跨轮触发：滴答跨越轮容量后正常触发
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

// TestTickCatchUp 追赶模式：逐滴答推进确保触发次数正确
func TestTickCatchUp(t *testing.T) {
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

// TestStressManyTimers 压力测试：10000个After定时器全部正常触发
func TestStressManyTimers(t *testing.T) {
	timer, now := newTestTimer()

	const N = 10000
	var count int

	for i := 0; i < N; i++ {
		d := int64((i % 100) + 1)
		timer.After(d, func() { count++ })
	}

	for tick := int64(1); tick <= 200; tick++ {
		*now = tick
		timer.Update()
	}

	if count != N {
		t.Fatalf("all %d timers should fire, got %d", N, count)
	}
}

// TestRemoteTimer 远程定时器：超出轮范围的定时器通过红黑树管理
func TestRemoteTimer(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	timer.After(1000, func() { count++ })

	*now = 999
	timer.Update()
	if count != 0 {
		t.Fatal("remote timer should not fire before expire")
	}

	*now = 1000
	timer.Update()
	if count != 1 {
		t.Fatalf("remote timer should fire at 1000, got %d", count)
	}
}

// TestFlushTree 树搬入轮：轮槽归零时远程定时器正确搬入
func TestFlushTree(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	// expires at 300, base starts at 0, 300-0=300 >= 256 → goes to tree
	timer.After(300, func() { count++ })

	// advance to 44, base=44. 300-44=256 still >= 256 → still in tree
	// then advance to 256+44=300
	*now = 300
	timer.Update()
	if count != 1 {
		t.Fatalf("should fire at 300, got %d", count)
	}
}

// TestGetMinExpires 最小到期时间：返回最近定时器的滴答数
func TestGetMinExpires(t *testing.T) {
	timer, now := newTestTimer()

	// no timers → MaxInt64
	if timer.GetMinExpires() == 0 {
		t.Fatal("empty timer should return non-zero")
	}

	timer.After(10, func() {})
	*now = 2
	timer.Update()

	e := timer.GetMinExpires()
	if e != 10 {
		t.Fatalf("min expires should be 10, got %d", e)
	}
}

// TestActiveNum 活跃计数：添加和删除后计数正确
func TestActiveNum(t *testing.T) {
	timer, _ := newTestTimer()
	timer.SetCacheNum(10000) // 抑制gc创建新定时器

	if timer.ActiveNum() != 0 {
		t.Fatal("initial count should be 0")
	}

	h := timer.After(10, func() {})
	if timer.ActiveNum() != 1 {
		t.Fatal("count should be 1 after add")
	}

	h.Delete()
	if timer.ActiveNum() != 0 {
		t.Fatalf("count should be 0 after delete, got %d", timer.ActiveNum())
	}
}

// TestMultipleSameExpires 同expires多个定时器：树中同一key分组管理
func TestMultipleSameExpires(t *testing.T) {
	timer, now := newTestTimer()
	timer.SetCacheNum(10000) // 抑制gc创建新定时器

	var count int
	timer.After(500, func() { count++ })
	timer.After(500, func() { count++ })
	timer.After(500, func() { count++ })

	if timer.ActiveNum() != 3 {
		t.Fatalf("should have 3 timers, got %d", timer.ActiveNum())
	}

	*now = 500
	timer.Update()
	if count != 3 {
		t.Fatalf("all 3 should fire at 500, got %d", count)
	}
	if timer.ActiveNum() != 0 {
		t.Fatalf("no timers should remain, got %d", timer.ActiveNum())
	}
}

// TestClear 清除所有定时器
func TestClear(t *testing.T) {
	timer, _ := newTestTimer()
	timer.SetCacheNum(10000) // 抑制gc创建新定时器

	timer.After(10, func() {})
	timer.After(500, func() {})
	timer.Every(100, func() {})

	if timer.ActiveNum() != 3 {
		t.Fatal("should have 3 timers")
	}

	timer.Clear()
	if timer.ActiveNum() != 0 {
		t.Fatal("should have 0 timers after clear")
	}
}

// TestEveryStress 间隔定时器压力：2000滴答间每10滴答触发
func TestEveryStress(t *testing.T) {
	timer, now := newTestTimer()

	var count int
	timer.Every(10, func() { count++ })

	for i := int64(1); i <= 2000; i++ {
		*now = i
		timer.Update()
	}
	if count != 200 {
		t.Fatalf("every 10 over 2000 ticks = 200 fires, got %d", count)
	}
}

// 十万定时器全流程耗时测试（1000次平均，含PreAlloc对比）
func TestHundredThousandTimers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	const N = 100_000
	const runs = 1000
	var totalCreate, totalCascade, totalFire, totalCascadeFire int64

	// Phase 1: without PreAlloc
	for r := 0; r < runs; r++ {
		var now int64
		timer := &Timer{}
		timer.Init(func() int64 { return now }, 256)
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
		timer2.Init(func() int64 { return now2 }, 256)
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
	t.Logf("--- without PreAlloc (%d-run avg) ---", runs)
	t.Logf("create: %.3fms  cascade: %.3fms  cas+fire: %.3fms  fire: %.3fms",
		float64(avgCreate.Microseconds())/1000, float64(avgCascade.Microseconds())/1000,
		float64(avgCascadeFire.Microseconds())/1000, float64(avgFire.Microseconds())/1000)

	// Phase 2: with PreAlloc
	totalCreate, totalCascade, totalFire, totalCascadeFire = 0, 0, 0, 0
	for r := 0; r < runs; r++ {
		var now int64
		timer := &Timer{}
		timer.Init(func() int64 { return now }, 256)
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
		timer2.Init(func() int64 { return now2 }, 256)
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
	t.Logf("--- with PreAlloc (%d-run avg) ---", runs)
	t.Logf("create: %.3fms  cascade: %.3fms  cas+fire: %.3fms  fire: %.3fms",
		float64(avgCreate.Microseconds())/1000, float64(avgCascade.Microseconds())/1000,
		float64(avgCascadeFire.Microseconds())/1000, float64(avgFire.Microseconds())/1000)
}

// 百万定时器全流程耗时测试（10次平均）
func TestMillionTimers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping million-timer test in short mode")
	}
	const N = 1_000_000
	const runs = 10
	var totalCreate, totalCascade, totalFire, totalCascadeFire int64

	for r := 0; r < runs; r++ {
		var now int64
		timer := &Timer{}
		timer.Init(func() int64 { return now }, 256)
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
		timer2.Init(func() int64 { return now2 }, 256)
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
	avgCreate := time.Duration(totalCreate / runs)
	avgCascade := time.Duration(totalCascade / runs)
	avgFire := time.Duration(totalFire / runs)
	avgCascadeFire := time.Duration(totalCascadeFire / runs)
	t.Logf("--- PreAlloc (%d-run avg) ---", runs)
	t.Logf("create: %.3fms  cascade: %.3fms  cas+fire: %.3fms  fire: %.3fms",
		float64(avgCreate.Microseconds())/1000, float64(avgCascade.Microseconds())/1000,
		float64(avgCascadeFire.Microseconds())/1000, float64(avgFire.Microseconds())/1000)
}

