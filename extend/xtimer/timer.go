package xtimer

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

// Timer 定时器接口，提供单次/循环定时、分组管理、panic处理等能力
type Timer interface {
	Init(tick func() int64, backend Backend)                 // 初始化定时器，绑定tick函数和后端存储
	UnInit()                                                 // 反初始化，清除所有定时器
	Update()                                                 // 驱动时间轮，处理到期定时器
	Clear()                                                  // 清除所有定时器
	After(duration int64, cb TimerCb) *Handle                // 创建一次性定时器
	Every(duration int64, cb TimerCb) *Handle                // 创建间隔定时器
	AfterGroup(duration int64, cb TimerCb, g *Group) *Handle // 创建一次性定时器并加入组
	EveryGroup(duration int64, cb TimerCb, g *Group) *Handle // 创建间隔定时器并加入组
	PreAlloc(n int)                                          // 预分配n个节点到空闲链表
	SetOnPanic(handler PanicHandler)                         // 设置panic回调
	CacheNum() int32                                         // 获取空闲缓存上限
	ActiveNum() int32                                        // 获取活跃定时器数量
	SetCacheNum(cacheNum int32)                              // 设置空闲缓存上限
	NextExpire() int64                                       // 获取最近到期时间
}

// Host 后端依赖的宿主接口，提供时间、base操作、批量处理等能力
type Host interface {
	Tick() int64                        // 获取当前滴答
	Base() int64                        // 获取当前基准滴答
	SetBase(int64)                      // 设置基准滴答
	Step()                              // 基准滴答前进一步
	ProcessBatch(head *ListHead[*Node]) // 批量处理到期定时器
	Drain(head *ListHead[*Node])        // 清空链表中的所有定时器
}

// Backend 时间轮存储后端接口
type Backend interface {
	Init(host Host)    // 初始化，传入宿主
	UnInit()           // 反初始化
	Update()           // 驱动一轮
	Clear()            // 清除所有定时器
	Enqueue(t *Node)   // 入队定时器节点
	Dequeue(t *Node)   // 出队定时器节点
	NextExpire() int64 // 重新扫描最小过期时间
}

// timer 定时器公共基类，包含共享字段与纯公共方法。
type timer struct {
	base       int64           // 已追到的基准滴答
	tick       func() int64    // 获取当前滴答数
	cacheNum   int32           // 空闲定时器缓存数量上限，超出部分由GC回收
	gcHandle   *Handle         // gc定时器句柄
	onPanic    PanicHandler    // panic处理回调
	freeNum    int32           // 空闲定时器链表中可复用的节点数量
	freeList   ListHead[*Node] // 空闲定时器链表，用于复用Node节点
	activeNum  int32           // 当前活跃定时器数量
	nextExpire int64           // 最近到期滴答数缓存
	backend    Backend         // 时间轮后端引擎（级联轮/红黑树轮）
}

func NewTimer() Timer {
	return &timer{}
}

func (p *timer) Init(tick func() int64, backend Backend) {
	p.tick = tick
	p.freeList.Init(nil)
	p.base = p.tick()
	p.nextExpire = p.base
	p.backend = backend
	backend.Init(p)
}

// UnInit 反初始化定时器系统，清除所有定时器
func (p *timer) UnInit() {
	p.activeNum = 0
	p.backend.UnInit()
}

// Tick 返回当前滴答数，委托给tick函数
func (p *timer) Tick() int64 { return p.tick() }

// Base 返回时间轮已追到的基准滴答
func (p *timer) Base() int64 { return p.base }

// SetBase 设置基准滴答
func (p *timer) SetBase(v int64) { p.base = v }

// Step 基准滴答前进一步
func (p *timer) Step() { p.base++ }

// Clear 清除所有定时器，重置backend和空闲链表
func (p *timer) Clear() {
	p.activeNum = 0
	p.nextExpire = p.base
	p.backend.Clear()
	p.Drain(&p.freeList)
}

// Update 驱动时间轮一个tick，处理到期定时器
func (p *timer) Update() {
	p.backend.Update()
}

// drain 清除链表中的所有定时器节点（不回收，仅解除链表关系）
func (p *timer) Drain(head *ListHead[*Node]) {
	for !head.Empty() {
		t := head.FirstEntry()
		t.Entry.DelInit()
		t.groupEntry.DelInit()
		t.rootEntry.DelInit()
		t.cb = nil
		t.sched = nil
		t.handle = nil
	}
}

// processTimerList 处理槽位链表中所有到期定时器的回调
func (p *timer) ProcessBatch(head *ListHead[*Node]) {
	if head.Empty() {
		return
	}

	batch := &ListHead[*Node]{}
	head.ReplaceInit(batch)
	for !batch.Empty() {
		first := batch.First()
		t := first.data
		if t.sched != nil {
			first.DelInit()
			t.sched.yield(t)
		} else {
			p.safeCall(t)
			p.finish(t.handle)
		}
	}
}

// After 设置一次性定时器
func (p *timer) After(duration int64, cb TimerCb) *Handle {
	return p.add(TIMER_TYPE_AFTER, duration, cb, nil)
}

// Every 设置间隔定时器
func (p *timer) Every(duration int64, cb TimerCb) *Handle {
	return p.add(TIMER_TYPE_EVERY, duration, cb, nil)
}

// AfterGroup 设置一次性定时器并关联组
func (p *timer) AfterGroup(duration int64, cb TimerCb, g *Group) *Handle {
	return p.add(TIMER_TYPE_AFTER, duration, cb, g)
}

// EveryGroup 设置间隔定时器并关联组
func (p *timer) EveryGroup(duration int64, cb TimerCb, g *Group) *Handle {
	return p.add(TIMER_TYPE_EVERY, duration, cb, g)
}

// PreAlloc 预分配n个定时器节点到空闲链表
func (p *timer) PreAlloc(n int) {
	for i := 0; i < n; i++ {
		t := new(Node)
		t.Init(p)
		p.freeList.AddTail(&t.Entry)
		p.freeNum++
	}
}

// alloc 分配或从空闲列表复用定时器节点
func (p *timer) alloc() *Node {
	if !p.freeList.Empty() {
		t := p.freeList.FirstEntry()
		t.Entry.DelInit()
		p.freeNum--
		return t
	}
	t := new(Node)
	t.Init(p)
	return t
}

// gc 定时器垃圾回收
func (p *timer) gc() {
	for i := 0; i < GcBatch && p.freeNum > p.cacheNum; i++ {
		t := p.freeList.FirstEntry()
		t.Entry.DelInit()
		p.freeNum--
	}
	if p.freeNum > p.cacheNum {
		p.gcHandle = p.After(GcReschedMin+(p.tick()&1023), p.gc)
	}
}

// reschedule 重新安排到期时间（子类覆盖）
func (p *timer) reschedule(t *Node, duration int64) {
	p.backend.Dequeue(t)
	p.schedule(t, duration, p.tick())
}

// schedule 计算到期时间并加入时间轮
func (p *timer) schedule(t *Node, duration int64, tick int64) {
	t.duration = duration
	t.expires = tick + duration
	if t.expires < p.nextExpire {
		p.nextExpire = t.expires
	}
	p.backend.Enqueue(t)
}

// safeCall 安全调用定时器回调，已注册PanicHandler时捕获panic并按返回值决定吞掉或重抛
func (p *timer) safeCall(t *Node) {
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
func (p *timer) SetOnPanic(handler PanicHandler) {
	p.onPanic = handler
}

// recycle 将定时器节点归还空闲列表以便复用
func (p *timer) recycle(t *Node) {
	p.freeList.AddTail(&t.Entry)
	p.freeNum++
}

// CacheNum 获取空闲定时器缓存数量上限
func (p *timer) CacheNum() int32 {
	return p.cacheNum
}

// ActiveNum 获取当前活跃定时器数量
func (p *timer) ActiveNum() int32 {
	return p.activeNum
}

// SetCacheNum 设置空闲定时器缓存数量上限，负数视为0
func (p *timer) SetCacheNum(cacheNum int32) {
	if cacheNum < 0 {
		cacheNum = 0
	}
	p.cacheNum = cacheNum
}

// add 添加定时器，返回句柄供外部管理
func (p *timer) add(typ int8, duration int64, cb TimerCb, g *Group) *Handle {
	if p.activeNum < 1 {
		p.base = p.tick()
	}
	t := p.alloc()
	t.set(typ, duration, cb)
	if g != nil {
		g.entries.AddTail(&t.groupEntry)
	}
	t.handle = &Handle{node: t}
	p.schedule(t, duration, p.tick())
	p.activeNum++
	return t.handle
}

// remove 删除定时器，归还空闲列表以便复用
func (p *timer) remove(t *Node) {
	if t.expires == p.nextExpire {
		p.nextExpire = p.base
	}
	p.backend.Dequeue(t)
	t.groupEntry.DelInit()
	t.rootEntry.DelInit()
	t.cb = nil
	isGC := t.handle == p.gcHandle
	t.sched = nil
	t.handle.node = nil
	t.handle = nil
	p.activeNum--
	p.recycle(t)
	if p.freeNum > p.cacheNum && !isGC && !p.gcHandle.Valid() {
		p.gcHandle = p.After(GcReschedMin+(p.tick()&1023), p.gc)
	}
}

// finish 根据定时器类型执行回调后的清理：After删除，Every重新调度
func (p *timer) finish(h *Handle) {
	if !h.Valid() {
		return
	}
	t := h.node
	if !t.isDeleted() {
		switch t.timerType {
		case TIMER_TYPE_AFTER:
			p.remove(t)
		case TIMER_TYPE_EVERY:
			p.reschedule(t, t.duration)
		}
	}
}

// NextExpire 返回最近到期滴答数，缓存失效时调用backend重新扫描
func (p *timer) NextExpire() int64 {
	if p.nextExpire <= p.base {
		p.nextExpire = p.backend.NextExpire()
	}
	return p.nextExpire
}
