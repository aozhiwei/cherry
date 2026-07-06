package xtimer

import (
	"fmt"
	"testing"
	"time"
)

type testWall struct {
	wt  *wallTimer
	now *int64
}

func newTestWall() *testWall {
	var now int64
	wt := innerNewWallTimer(NewCascadeWheel(), time.Millisecond, func() int64 { return now }, func() {}, func() {})
	return &testWall{wt: wt.(*wallTimer), now: &now}
}

func (p *testWall) tick(t int64) {
	*p.now = t
	p.wt.xt.Update()
}

// --- AfterGroup ---

func TestWallAfter(t *testing.T) {
	tw := newTestWall()
	var count int
	tw.wt.AfterGroup(100*time.Millisecond, func() { count++ }, nil, nil, nil)

	tw.tick(50)
	if count != 0 {
		t.Fatal("should not fire at 50ms")
	}

	tw.tick(100)
	if count != 1 {
		t.Fatalf("should fire at 100ms, got %d", count)
	}

	tw.tick(200)
	if count != 1 {
		t.Fatal("After should only fire once")
	}
}

// --- EveryGroup ---

func TestWallEvery(t *testing.T) {
	tw := newTestWall()
	var count int
	tw.wt.EveryGroup(50*time.Millisecond, func() { count++ }, nil, nil, nil)

	tw.tick(50)
	if count != 1 {
		t.Fatalf("first fire: got %d", count)
	}

	tw.tick(100)
	if count != 2 {
		t.Fatalf("second fire: got %d", count)
	}

	tw.tick(150)
	if count != 3 {
		t.Fatalf("third fire: got %d", count)
	}
}

// --- Delete ---

func TestWallDelete(t *testing.T) {
	tw := newTestWall()
	var count int
	h := tw.wt.AfterGroup(100*time.Millisecond, func() { count++ }, nil, nil, nil)

	h.Delete()
	if h.Valid() {
		t.Fatal("handle should be invalid after Delete")
	}

	tw.tick(100)
	if count != 0 {
		t.Fatal("deleted timer should not fire")
	}
}

// --- Reschedule ---

func TestWallReschedule(t *testing.T) {
	tw := newTestWall()
	var count int
	h := tw.wt.AfterGroup(100*time.Millisecond, func() { count++ }, nil, nil, nil)

	h.Reschedule(200 * time.Millisecond)

	tw.tick(100)
	if count != 0 {
		t.Fatal("should not fire at old time")
	}

	tw.tick(200)
	if count != 1 {
		t.Fatalf("should fire at new time, got %d", count)
	}
}

// --- CronGroup ---

func TestWallCron(t *testing.T) {
	tw := newTestWall()
	var count int
	c := tw.wt.CronGroup(100*time.Millisecond, 50*time.Millisecond, func() { count++ }, nil, nil, nil)

	tw.tick(50)
	if count != 0 {
		t.Fatal("should not fire before delay")
	}

	tw.tick(100)
	if count != 1 {
		t.Fatalf("first fire: got %d", count)
	}

	tw.tick(150)
	if count != 2 {
		t.Fatalf("second fire: got %d", count)
	}

	tw.tick(200)
	if count != 3 {
		t.Fatalf("third fire: got %d", count)
	}

	c.Delete()
	if c.Valid() {
		t.Fatal("should be invalid after Delete")
	}

	tw.tick(250)
	if count != 3 {
		t.Fatal("should not fire after Delete")
	}
}

// --- CronGroup drift correction ---

func TestWallCronDrift(t *testing.T) {
	tw := newTestWall()
	var count int
	_ = tw.wt.CronGroup(100*time.Millisecond, 50*time.Millisecond, func() { count++ }, nil, nil, nil)

	tw.tick(100)
	if count != 1 {
		t.Fatalf("first fire: got %d", count)
	}

	// 逐步追赶，模拟真实时间推进
	for t := int64(150); t <= 500; t += 50 {
		tw.tick(t)
	}
	// 应触发8次：100,150,200,250,300,350,400,450
	if count < 8 {
		t.Fatalf("should fire 8 times, got %d", count)
	}
}

// --- NewGroup ---

func TestWallGroup(t *testing.T) {
	tw := newTestWall()
	g := tw.wt.NewGroup()
	var count int
	tw.wt.AfterGroup(100*time.Millisecond, func() { count++ }, g, nil, nil)

	g.Clear()

	tw.tick(100)
	if count != 0 {
		t.Fatal("cleared group timer should not fire")
	}
}

// --- Remain ---

func TestWallRemain(t *testing.T) {
	tw := newTestWall()
	h := tw.wt.AfterGroup(100*time.Millisecond, func() {}, nil, nil, nil)

	tw.tick(30)
	r := h.Remain()
	if r < 60*time.Millisecond || r > 80*time.Millisecond {
		t.Fatalf("remain should be ~70ms, got %v", r)
	}
}

func TestAfter1(t *testing.T) {
	var globalStartTime = time.Now()
	getGlobalTick := func() int64 {
		return time.Since(globalStartTime).Milliseconds()
	}
	wt := NewWallTimer(NewCascadeWheel(), time.Millisecond, getGlobalTick)
	wt.EveryGroup(time.Second*3, func() {
		fmt.Println(time.Now().Format("2006-01-02 15:04:05.000"), 2222)
	}, nil, nil, nil)
	count := 1
	go func() {
		for {
			<-wt.Schedule().C()
			wt.Schedule().Update()
			time.Sleep(time.Millisecond)
			count++
		}
	}()
	for {
		time.Sleep(time.Millisecond)
	}
}
