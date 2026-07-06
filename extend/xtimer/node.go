package xtimer

type Node struct {
	Entry      ListHead[*Node] // 通用链表节点，在时间轮槽位或空闲链表中
	groupEntry ListHead[*Node] // 定时器组关联链表节点
	rootEntry  ListHead[*Node] // 根组关联链表节点
	timerType  int8            // 定时器类型: TIMER_TYPE_AFTER/TIMER_TYPE_EVERY
	duration   int64           // 定时/间隔（滴答数）
	expires    int64           // 绝对到期时间戳

	cb     TimerCb // 定时器回调函数
	sched  Channel // 自定义调度
	handle *Handle // 句柄，外部用于引用和管理此定时器
	timer  *timer  // 所属定时器实例
}

// set 设置定时器列表的基本属性
func (p *Node) set(timerType int8, duration int64, cb TimerCb) {
	p.timerType = timerType
	p.duration = duration
	p.cb = cb
}

// isDeleted 判断定时器是否已被删除，通过handle是否为nil来判断
func (p *Node) isDeleted() bool {
	return p.handle == nil
}

// init 初始化定时器列表的所有链表头结点
func (p *Node) Init(timer *timer) {
	p.timer = timer
	p.Entry.Init(p)
	p.groupEntry.Init(p)
	p.rootEntry.Init(p)
}

// reschedule 委托timer重新安排
func (p *Node) reschedule(duration int64) {
	p.timer.reschedule(p, duration)
}

// remove 委托timer删除自身
func (p *Node) remove() {
	p.timer.remove(p)
}

// GetExpires 返回到期滴答数
func (p *Node) GetExpires() int64 {
	return p.expires
}

// remain 返回剩余滴答数
func (p *Node) remain() int64 {
	r := p.expires - p.timer.tick()
	if r < 0 {
		return 0
	}
	return r
}
