package xtimer

import (
	"math"

	rbt "github.com/emirpasic/gods/trees/redblacktree"
)

// 定时器运行参数
const (
	GcReschedMin = 4000 // gc重新调度最小间隔
	GcBatch      = 1000 // gc单次回收上限
)

// 定时器类型
const (
	TIMER_TYPE_AFTER = 0    // 一次性定时器，到期后自动删除
	TIMER_TYPE_EVERY = iota // 间隔定时器，到期后自动重置
)

// TimerCb 定时器回调函数类型
type TimerCb func()

// PanicHandler 定时器回调发生panic时调用，返回true吞掉panic继续运行
type PanicHandler func(h *Handle, recovered any) bool

// Timer 单轮+红黑树定时器系统。
// 近程定时器（expires-base < wheelSize）存入时间轮槽位，O(1)触发；
// 远程定时器按expires分组存入红黑树，轮槽归零时批量搬入。
type Timer struct {
	base       int64                  // 时间轮已追到的基准滴答
	tick       func() int64           // 获取当前滴答数
	wheelSize  int64                  // 轮容量（须为2的幂）
	wheelMask  int64                  // wheelSize - 1，位运算用
	maxScan    int64                  // 扫描最大前瞻跨度
	cacheNum   int32                  // 空闲定时器缓存数量上限，超出部分由GC回收
	gcHandle   *Handle                // gc定时器句柄
	onPanic    PanicHandler           // panic处理回调
	freeNum    int32                  // 空闲定时器链表中可复用的节点数量
	freeList   listHead[*timerList]   // 空闲定时器链表，用于复用timerList节点
	activeNum  int32                  // 当前活跃定时器数量
	wheelNum   int32                  // 落在轮内的定时器数量
	minExpires int64                  // 最近到期滴答数缓存
	wheel      []listHead[*timerList] // 时间轮槽位切片
	tree       *rbt.Tree              // 红黑树，key=expires, value=同expires的定时器链表头
}

// timerComparator 红黑树比较函数，按int64(expires)升序排列
func timerComparator(a, b interface{}) int {
	ea := a.(int64)
	eb := b.(int64)
	switch {
	case ea < eb:
		return -1
	case ea > eb:
		return 1
	default:
		return 0
	}
}

// Init 初始化时间轮定时器系统，传入获取当前滴答的函数和轮容量（须为2的幂）
func (p *Timer) Init(tick func() int64, wheelSize int64) {
	p.wheelSize = wheelSize
	p.wheelMask = wheelSize - 1
	p.maxScan = wheelSize * 2
	p.wheel = make([]listHead[*timerList], wheelSize)
	p.freeList.init(nil)
	for i := range p.wheel {
		p.wheel[i].init(nil)
	}
	p.tree = rbt.NewWith(timerComparator)
	p.base = tick()
	p.tick = tick
	p.minExpires = p.base
}

// UnInit 反初始化定时器系统，清除所有定时器
func (p *Timer) UnInit() {
	p.Clear()
	p.activeNum = 0
}

// drain 清除链表中的所有定时器节点（不回收，仅解除链表关系）
func (p *Timer) drain(head *listHead[*timerList]) {
	for !head.empty() {
		t := head.firstEntry()
		t.entry.delInit()
		t.groupEntry.delInit()
	}
}

// Clear 清除时间轮和红黑树中所有定时器
func (p *Timer) Clear() {
	p.drain(&p.freeList)
	for i := range p.wheel {
		p.drain(&p.wheel[i])
	}
	iter := p.tree.Iterator()
	for iter.Next() {
		p.drain(iter.Value().(*listHead[*timerList]))
	}
	p.tree.Clear()
	p.wheelNum = 0
	p.activeNum = 0
	p.minExpires = p.base
}

// Update 驱动时间轮，执行到期定时器的回调。
// 单次调用可能推进多个滴答，每滴答处理对应槽位的所有到期定时器。
func (p *Timer) Update() {
	tick := p.tick()
	if p.wheelNum <= 0 {
		p.base = tick
	}
	for tick >= p.base {
		index := p.base & p.wheelMask
		// 轮槽归零时，从树中搬入落入下一轮范围的定时器
		if index == 0 {
			p.flushTree()
		}
		p.processTimerList(&p.wheel[index])
		p.base++
	}
}

// flushTree 将红黑树中落入轮范围（expires < base+wheelSize）的定时器搬入对应槽位。
// 利用红黑树中序遍历有序性，遇首个不满足条件的key即停止。
// 先收集keys再删除，避免迭代中删key破坏迭代器。
func (p *Timer) flushTree() {
	bound := p.base + p.wheelSize
	var keys []int64
	iter := p.tree.Iterator()
	for iter.Next() {
		if iter.Key().(int64) >= bound {
			break
		}
		head := iter.Value().(*listHead[*timerList])
		for !head.empty() {
			t := head.firstEntry()
			t.entry.delInit()
			t.rbNodeHead = nil
			p.wheel[t.expires&p.wheelMask].addTail(&t.entry)
			p.wheelNum++
		}
		keys = append(keys, iter.Key().(int64))
	}
	for _, k := range keys {
		p.tree.Remove(k)
	}
}

// processTimerList 处理槽位链表中所有到期定时器的回调
func (p *Timer) processTimerList(head *listHead[*timerList]) {
	for !head.empty() {
		t := head.firstEntry()
		p.safeCall(t)
		if !t.isDeleted() {
			switch t.timerType {
			case TIMER_TYPE_AFTER:
				p.remove(t)
			case TIMER_TYPE_EVERY:
				p.reschedule(t, t.duration)
			}
		}
	}
}

// safeCall 安全调用定时器回调，已注册PanicHandler时捕获panic并按返回值决定吞掉或重抛
func (p *Timer) safeCall(t *timerList) {
	if p.onPanic != nil {
		h := t.handle
		defer func() {
			if r := recover(); r != nil {
				if !p.onPanic(h, r) {
					panic(r)
				}
			}
		}()
	}
	t.cb()
}

// SetOnPanic 设置panic处理回调，返回true吞掉panic继续运行，返回false则重新panic
func (p *Timer) SetOnPanic(handler PanicHandler) {
	p.onPanic = handler
}

// After 设置一次性定时器，到期后自动删除
func (p *Timer) After(duration int64, cb TimerCb) *Handle {
	return p.add(TIMER_TYPE_AFTER, duration, cb, nil)
}

// AfterGroup 设置一次性定时器并关联定时器组
func (p *Timer) AfterGroup(duration int64, cb TimerCb, g *Group) *Handle {
	return p.add(TIMER_TYPE_AFTER, duration, cb, g)
}

// Every 设置间隔定时器，每隔duration滴答执行一次回调
func (p *Timer) Every(duration int64, cb TimerCb) *Handle {
	return p.add(TIMER_TYPE_EVERY, duration, cb, nil)
}

// EveryGroup 设置间隔定时器并关联定时器组
func (p *Timer) EveryGroup(duration int64, cb TimerCb, g *Group) *Handle {
	return p.add(TIMER_TYPE_EVERY, duration, cb, g)
}

// add 添加定时器到时间轮系统，返回句柄供外部管理
func (p *Timer) add(typ int8, duration int64, cb TimerCb, g *Group) *Handle {
	if p.activeNum < 1 {
		p.base = p.tick()
	}
	t := p.alloc()
	t.set(typ, duration, cb)
	if g != nil {
		g.entries.addTail(&t.groupEntry)
	}
	t.handle = &Handle{node: t}
	p.schedule(t, duration, p.tick())
	p.activeNum++
	return t.handle
}

// GetMinExpires 返回最近一个定时器的到期滴答数，无活跃定时器返回MaxInt64
func (p *Timer) GetMinExpires() int64 {
	if p.activeNum < 1 {
		return math.MaxInt64
	}
	if p.minExpires <= p.base {
		p.rescanMin()
	}
	return p.minExpires
}

// rescanMin 从base当前位置位掩码回绕扫描时间轮，遇非空槽取firstEntry，
// 并与红黑树最小key比较，取较小值更新minExpires缓存。
func (p *Timer) rescanMin() {
	p.minExpires = math.MaxInt64
	idx := int(p.base & p.wheelMask)
	for i := idx; i < idx+int(p.wheelSize); i++ {
		if !p.wheel[i&int(p.wheelMask)].empty() {
			p.minExpires = p.wheel[i&int(p.wheelMask)].firstEntry().expires
			break
		}
	}
	if n := p.tree.Left(); n != nil {
		if e := n.Key.(int64); e < p.minExpires {
			p.minExpires = e
		}
	}
}

// recycle 将定时器节点归还空闲列表以便复用
func (p *Timer) recycle(t *timerList) {
	p.freeList.addTail(&t.entry)
	p.freeNum++
}

// PreAlloc 预分配n个定时器节点到空闲链表，后续After/Every调用复用免分配
func (p *Timer) PreAlloc(n int) {
	for i := 0; i < n; i++ {
		t := new(timerList)
		t.init(p)
		p.freeList.addTail(&t.entry)
		p.freeNum++
	}
}

// dequeue 从时间轮槽位移除定时器，若其所在红黑树链表为空则从树中删除对应expires条目
func (p *Timer) dequeue(t *timerList) {
	t.entry.delInit()
	if t.rbNodeHead != nil {
		if t.rbNodeHead.empty() {
			p.tree.Remove(t.expires)
		}
	} else {
		p.wheelNum--
	}
	t.rbNodeHead = nil
}

// enqueue 根据定时器到期时间将其加入时间轮或红黑树。
// expires-base < wheelSize 时直接入轮槽；否则按expires分组插入红黑树。
func (p *Timer) enqueue(t *timerList) {
	if t.expires-p.base < p.wheelSize {
		p.wheel[t.expires&p.wheelMask].addTail(&t.entry)
		p.wheelNum++
	} else {
		if head, found := p.tree.Get(t.expires); found {
			t.rbNodeHead = head.(*listHead[*timerList])
			t.rbNodeHead.addTail(&t.entry)
		} else {
			head := &listHead[*timerList]{}
			head.init(nil)
			t.rbNodeHead = head
			head.addTail(&t.entry)
			p.tree.Put(t.expires, head)
		}
	}
}

// alloc 分配或从空闲列表复用定时器列表节点
func (p *Timer) alloc() *timerList {
	if !p.freeList.empty() {
		t := p.freeList.firstEntry()
		t.entry.delInit()
		p.freeNum--
		return t
	} else {
		t := new(timerList)
		t.init(p)
		return t
	}
}

// CacheNum 获取空闲定时器缓存数量上限
func (p *Timer) CacheNum() int32 {
	return p.cacheNum
}

// ActiveNum 获取当前活跃定时器数量
func (p *Timer) ActiveNum() int32 {
	return p.activeNum
}

// SetCacheNum 设置空闲定时器缓存数量上限，负数视为0
func (p *Timer) SetCacheNum(cacheNum int32) {
	if cacheNum < 0 {
		cacheNum = 0
	}
	p.cacheNum = cacheNum
}

// gc 定时器垃圾回收：限制空闲列表中的缓存数量，单次最多回收GcBatch个
func (p *Timer) gc() {
	for i := 0; i < GcBatch && p.freeNum > p.cacheNum; i++ {
		t := p.freeList.firstEntry()
		t.entry.delInit()
		p.freeNum--
	}
	if p.freeNum > p.cacheNum {
		p.gcHandle = p.After(GcReschedMin+(p.tick()&1023), p.gc)
	}
}

// remove 删除定时器，归还空闲列表以便复用
func (p *Timer) remove(t *timerList) {
	if t.expires == p.minExpires {
		p.minExpires = p.base
	}
	p.dequeue(t)
	t.groupEntry.delInit()
	t.cb = nil
	isGC := t.handle == p.gcHandle
	t.handle.node = nil
	t.handle = nil
	p.activeNum--
	p.recycle(t)
	if !isGC && p.freeNum > p.cacheNum && !p.gcHandle.Valid() {
		p.gcHandle = p.After(GcReschedMin+(p.tick()&1023), p.gc)
	}
}

// schedule 计算到期时间并将定时器加入时间轮或红黑树
func (p *Timer) schedule(t *timerList, duration int64, tick int64) {
	t.duration = duration
	t.expires = tick + duration
	if t.expires < p.minExpires {
		p.minExpires = t.expires
	}
	p.enqueue(t)
}

// reschedule 重新安排定时器的到期时间，先移除再按新duration调度
func (p *Timer) reschedule(t *timerList, duration int64) {
	p.dequeue(t)
	p.schedule(t, duration, p.tick())
}
