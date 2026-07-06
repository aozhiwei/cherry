package xtimer

type timerList struct {
	entry      listHead[*timerList] // 通用链表节点，在时间轮槽位或空闲链表中
	groupEntry listHead[*timerList] // 定时器组关联链表节点
	timerType  int8                 // 定时器类型: TIMER_TYPE_AFTER/TIMER_TYPE_EVERY
	duration   int64                // 定时/间隔（滴答数）
	expires    int64                // 绝对到期时间戳

	cb     TimerCb // 定时器回调函数
	handle *Handle // 句柄，外部用于引用和管理此定时器
	timer  *Timer  // 所属定时器实例
}

// 设置定时器列表的基本属性
func (p *timerList) set(timerType int8, duration int64, cb TimerCb) {
	p.timerType = timerType
	p.duration = duration
	p.cb = cb
}

// 判断定时器是否已被删除，通过handle是否为nil来判断
func (p *timerList) isDeleted() bool {
	return p.handle == nil
}

// 初始化定时器列表的所有链表头结点
func (p *timerList) init(timer *Timer) {
	p.timer = timer
	p.entry.init(p)
	p.groupEntry.init(p)
}
