package xtimer

type Handle struct {
	node *Node // 定时器节点，定时器被删除后置nil
}

// 判断定时器句柄是否有效（nil或关联的定时器已被删除均视为无效）
func (p *Handle) Valid() bool {
	return p != nil && p.node != nil
}

// 重新安排定时器的到期时间
func (p *Handle) Reschedule(duration int64) {
	if p.Valid() {
		p.node.reschedule(duration)
	}
}

// 删除定时器
func (p *Handle) Delete() {
	if p.Valid() {
		p.node.remove()
	}
}

// 获取定时器剩余滴答数
func (p *Handle) Remain() int64 {
	if !p.Valid() {
		return 0
	}
	return p.node.remain()
}

// setSched 设置异步调度通道
func (p *Handle) setSched(sched Channel) {
	if !p.Valid() {
		return
	}
	p.node.sched = sched
}

// setRootGroup 将句柄加入根组
func (p *Handle) setRootGroup(g *Group) {
	if !p.Valid() {
		return
	}
	g.entries.AddTail(&p.node.rootEntry)
}
