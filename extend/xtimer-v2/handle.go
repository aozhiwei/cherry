package xtimer

// Handle 定时器句柄，外部通过句柄管理定时器的生命周期和参数调整
type Handle struct {
	node *timerList // 定时器节点，定时器被删除后置nil
}

// Valid 判断定时器句柄是否有效（nil或关联的定时器已被删除均视为无效）
func (p *Handle) Valid() bool {
	return p != nil && p.node != nil
}

// Reschedule 重新安排定时器的到期时间
func (p *Handle) Reschedule(duration int64) {
	if p.Valid() {
		p.node.timer.reschedule(p.node, duration)
	}
}

// Delete 删除定时器
func (p *Handle) Delete() {
	if p.Valid() {
		p.node.timer.remove(p.node)
	}
}

// Remain 获取定时器剩余滴答数
func (p *Handle) Remain() int64 {
	if !p.Valid() {
		return 0
	}
	r := p.node.expires - p.node.timer.tick()
	if r < 0 {
		return 0
	}
	return r
}
