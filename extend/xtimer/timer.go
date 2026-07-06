package xtimer

// 时间轮几何参数
const (
	TVN_BITS = 6             // 高层轮位数（2^6=64槽）
	TVR_BITS = 8             // 底层轮位数（2^8=256槽）
	TVN_SIZE = 1 << TVN_BITS // 高层轮容量
	TVR_SIZE = 1 << TVR_BITS // 底层轮容量
	TVN_MASK = TVN_SIZE - 1  // 高层轮掩码
	TVR_MASK = TVR_SIZE - 1  // 底层轮掩码
)

// 定时器运行参数
const (
	GcReschedMin  = 4000         // gc重新调度最小间隔
	GcBatch       = 1000         // gc单次回收上限
	MaxScanDelta  = TVR_SIZE * 2 // 扫描最大前瞻跨度（512滴答）
	MaxScanLevels = 1            // rescanMin最多向上扫描的层级数
)

// 定时器类型
const (
	TIMER_TYPE_AFTER = 0    // 一次性定时器，到期后自动删除
	TIMER_TYPE_EVERY = iota // 间隔定时器，到期后自动重置
)

type TimerCb func()

// PanicHandler 定时器回调发生panic时调用，返回true吞掉panic继续运行
type PanicHandler func(h *Handle, recovered any) bool

type Timer struct {
	base       int64                          // 时间轮已追到的基准滴答
	tick       func() int64                   // 获取当前滴答数
	cacheNum   int32                          // 空闲定时器缓存数量上限，超出部分由GC回收
	gcHandle   *Handle                        // gc定时器句柄
	onPanic    PanicHandler                   // panic处理回调
	freeNum    int32                          // 空闲定时器链表中可复用的节点数量
	freeList   listHead[*timerList]           // 空闲定时器链表，用于复用timerList节点
	activeNum  int32                          // 当前活跃定时器数量（时间轮中已调度未触发）
	minExpires int64                          // 最近到期滴答数缓存
	tv1        [TVR_SIZE]listHead[*timerList] // 时间轮第一层（256槽，8bit精度）
	tv2        [TVN_SIZE]listHead[*timerList] // 时间轮第二层（64槽）
	tv3        [TVN_SIZE]listHead[*timerList] // 时间轮第三层（64槽）
	tv4        [TVN_SIZE]listHead[*timerList] // 时间轮第四层（64槽）
	tv5        [TVN_SIZE]listHead[*timerList] // 时间轮第五层（64槽）
}

// 遍历所有时间轮槽位，对每个槽位执行fn，零堆分配
func (p *Timer) forEachSlot(fn func(slot *listHead[*timerList])) {
	for i := range p.tv1 {
		fn(&p.tv1[i])
	}
	for i := range p.tv2 {
		fn(&p.tv2[i])
	}
	for i := range p.tv3 {
		fn(&p.tv3[i])
	}
	for i := range p.tv4 {
		fn(&p.tv4[i])
	}
	for i := range p.tv5 {
		fn(&p.tv5[i])
	}
}

// 初始化时间轮定时器系统
func (p *Timer) Init(tick func() int64) {
	p.freeList.init(nil)
	p.forEachSlot(func(slot *listHead[*timerList]) {
		slot.init(nil)
	})
	p.base = tick()
	p.tick = tick
	p.minExpires = p.base
}

// 反初始化定时器系统，清除所有定时器（含gc）
func (p *Timer) UnInit() {
	p.Clear()
	p.activeNum = 0
}

// 清除单个槽位链表中的所有定时器
func (p *Timer) drain(head *listHead[*timerList]) {
	for !head.empty() {
		t := head.firstEntry()
		t.entry.delInit()
		t.groupEntry.delInit()
	}
}

// 清除时间轮中所有定时器
func (p *Timer) Clear() {
	p.drain(&p.freeList)
	p.forEachSlot(func(slot *listHead[*timerList]) {
		p.drain(slot)
	})
	p.activeNum = 0
	p.minExpires = p.base
}

// 驱动时间轮，执行到期定时器的回调
func (p *Timer) Update() {
	tick := p.tick()
	for tick >= p.base {
		index := p.base & TVR_MASK
		if index == 0 &&
			p.cascade(&p.tv2, p.slot(0)) == 0 &&
			p.cascade(&p.tv3, p.slot(1)) == 0 &&
			p.cascade(&p.tv4, p.slot(2)) == 0 {
			p.cascade(&p.tv5, p.slot(3))
		}
		p.processTimerList(&p.tv1[index])
		p.base++
	}
}

// 执行槽位链表中所有到期定时器的回调
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

// 安全调用定时器回调，已注册PanicHandler时捕获panic并按返回值决定吞掉或重抛
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

// 设置panic处理回调，返回true吞掉panic继续运行，返回false则重新panic
func (p *Timer) SetOnPanic(handler PanicHandler) {
	p.onPanic = handler
}

// 设置一次性定时器，到期后自动删除
func (p *Timer) After(duration int64, cb TimerCb) *Handle {
	return p.add(TIMER_TYPE_AFTER, duration, cb, nil)
}

// 设置一次性定时器并关联定时器组
func (p *Timer) AfterGroup(duration int64, cb TimerCb, g *Group) *Handle {
	return p.add(TIMER_TYPE_AFTER, duration, cb, g)
}

// 设置间隔定时器，每隔duration滴答执行一次回调
func (p *Timer) Every(duration int64, cb TimerCb) *Handle {
	return p.add(TIMER_TYPE_EVERY, duration, cb, nil)
}

// 设置间隔定时器并关联定时器组
func (p *Timer) EveryGroup(duration int64, cb TimerCb, g *Group) *Handle {
	return p.add(TIMER_TYPE_EVERY, duration, cb, g)
}

// 添加定时器到时间轮
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

// 获取最近一个定时器的到期滴答数
func (p *Timer) GetMinExpires() int64 {
	if p.activeNum < 1 {
		return p.base + MaxScanDelta
	}
	if p.minExpires <= p.base {
		p.rescanMin()
	}
	return p.minExpires
}

// scanLevel 从index正向扫描时间轮数组，泛型兼容tv1(256槽)和tvN(64槽)。
// 定时器命中时*found=true且*expires更新。
// 返回true表示发生级联（index==0||slot<index），需继续向上级扫描。
func scanLevel[TV [TVR_SIZE]listHead[*timerList] | [TVN_SIZE]listHead[*timerList]](tv *TV, index int, found *bool, expires *int64) bool {
	arr := *tv
	mask := len(arr) - 1
	slot := index
	for {
		arr[slot].forEach(func(t *timerList) bool {
			*found = true
			if t.expires < *expires {
				*expires = t.expires
				return false
			}
			return true
		})
		if *found {
			return index == 0 || slot < index
		}
		slot = (slot + 1) & mask

		if slot == index {
			break
		}
	}
	return false
}

// rescanMin 参考Linux __next_timer_interrupt，从base当前位置正向扫描各级时间轮，
// 找到最近非空槽的首个定时器到期滴答。逐级前向扫描，以级联索引为起点，
// 位掩码自动回绕完成循环扫描，未命中则向上一级继续。
func (p *Timer) rescanMin() int64 {
	var (
		tick    = p.base
		expires = tick + MaxScanDelta
		found   = false
		index   = int(tick & TVR_MASK)
	)

	cascade := scanLevel(&p.tv1, index, &found, &expires)
	if found && !cascade {
		return expires
	}

	// tv1未命中或已级联，逐级扫描tv2~tv5（受MaxScanLevels限制）
	if index != 0 {
		tick += int64(TVR_SIZE - index)
	}
	tick >>= TVR_BITS

	varray := [...]*[TVN_SIZE]listHead[*timerList]{
		&p.tv2,
		&p.tv3,
		&p.tv4,
		&p.tv5,
	}
	for i := 0; i < MaxScanLevels; i++ {
		index = int(tick & TVN_MASK)
		cascade = scanLevel(varray[i], index, &found, &expires)
		if found && !cascade {
			return expires
		}

		if index != 0 {
			tick += int64(TVN_SIZE - index)
		}
		tick >>= TVN_BITS
	}
	return expires
}

// 将定时器加入空闲列表以便复用（调用前 remove 已完成清理）
func (p *Timer) recycle(t *timerList) {
	p.freeList.addTail(&t.entry)
	p.freeNum++
}

// 预分配 n 个定时器节点到空闲链表，后续 After/Every 调用复用免分配
func (p *Timer) PreAlloc(n int) {
	for i := 0; i < n; i++ {
		t := new(timerList)
		t.init(p)
		p.freeList.addTail(&t.entry)
		p.freeNum++
	}
}

// 时间轮级联：将高层时间轮的定时器重新分配到低层
func (p *Timer) cascade(tv *[TVN_SIZE]listHead[*timerList], index uint32) uint32 {
	var head listHead[*timerList]
	tv[index].replaceInit(&head)
	head.forEach(func(t *timerList) bool {
		p.enqueue(t)
		return true
	})
	return index
}

// 从时间轮槽位移除定时器
func (p *Timer) dequeue(t *timerList) {
	t.entry.delInit()
}

// 将定时器添加到时间轮对应槽位
func (p *Timer) enqueue(t *timerList) {
	idx := t.expires - p.base
	if idx < TVR_SIZE {
		p.tv1[t.expires&TVR_MASK].addTail(&t.entry)
		return
	}
	if idx < (1 << (TVR_BITS + TVN_BITS)) {
		p.tv2[(t.expires>>TVR_BITS)&TVN_MASK].addTail(&t.entry)
		return
	}
	if idx < (1 << (TVR_BITS + 2*TVN_BITS)) {
		p.tv3[(t.expires>>(TVR_BITS+TVN_BITS))&TVN_MASK].addTail(&t.entry)
		return
	}
	if idx < (1 << (TVR_BITS + 3*TVN_BITS)) {
		p.tv4[(t.expires>>(TVR_BITS+2*TVN_BITS))&TVN_MASK].addTail(&t.entry)
		return
	}
	p.tv5[(t.expires>>(TVR_BITS+3*TVN_BITS))&TVN_MASK].addTail(&t.entry)
}

// 根据级联层级计算时间轮索引
func (p *Timer) slot(index uint32) uint32 {
	return (uint32)((p.base >> (TVR_BITS + index*TVN_BITS)) & TVN_MASK)
}

// 分配或从空闲列表复用定时器列表节点
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

// 获取空闲定时器缓存数量上限
func (p *Timer) CacheNum() int32 {
	return p.cacheNum
}

// 获取当前活跃定时器数量
func (p *Timer) ActiveNum() int32 {
	return p.activeNum
}

// 设置空闲定时器缓存数量上限，负数视为0
func (p *Timer) SetCacheNum(cacheNum int32) {
	if cacheNum < 0 {
		cacheNum = 0
	}
	p.cacheNum = cacheNum
}

// 定时器垃圾回收：限制空闲列表中的缓存数量，单次最多回收GcBatch个
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

// 删除定时器，归还空闲列表以便复用
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

// 计算到期时间并将定时器加入时间轮对应槽位
func (p *Timer) schedule(t *timerList, duration int64, tick int64) {
	t.duration = duration
	t.expires = tick + duration
	if t.expires < p.minExpires {
		p.minExpires = t.expires
	}
	p.enqueue(t)
}

// 重新安排定时器的到期时间并加入时间轮
func (p *Timer) reschedule(t *timerList, duration int64) {
	p.dequeue(t)
	p.schedule(t, duration, p.tick())
}
