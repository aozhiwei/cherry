package xtimer

type listHead[T any] struct {
	next *listHead[T] // 后继节点指针
	prev *listHead[T] // 前驱节点指针
	data T            // 节点存储的数据
}

// 初始化链表头结点，next和prev指向自身
func (p *listHead[T]) init(data T) {
	p.next = p
	p.prev = p
	p.data = data
}

// 从链表中删除当前节点
func (p *listHead[T]) del() {
	p.next.prev = p.prev
	p.prev.next = p.next
}

// 在尾部添加新节点（插入到prev和当前节点之间）
func (p *listHead[T]) addTail(pnew *listHead[T]) {
	prev := p.prev
	next := p

	next.prev = pnew
	pnew.next = next
	pnew.prev = prev
	prev.next = pnew
}

// 获取链表第一个元素的数据（非头结点）
func (p *listHead[T]) firstEntry() T {
	if !p.empty() {
		return p.next.data
	}
	var zero T
	return zero
}

// 用新节点替换当前节点在链表中的位置
func (p *listHead[T]) replace(pnew *listHead[T]) {
	pnew.next = p.next
	pnew.next.prev = pnew
	pnew.prev = p.prev
	pnew.prev.next = pnew
}

// 用新节点替换当前节点，并将当前节点初始化为自环
func (p *listHead[T]) replaceInit(pnew *listHead[T]) {
	p.replace(pnew)
	p.next = p
	p.prev = p
}

// 判断链表是否为空（只有头结点自环）
func (p *listHead[T]) empty() bool {
	return p.next == p
}

// 从链表中删除当前节点并初始化为自环
func (p *listHead[T]) delInit() {
	p.del()
	p.next = p
	p.prev = p
}

// 遍历链表，对每个非头结点的数据执行回调，返回false则提前终止。
// 回调前缓存next，允许回调中修改或删除当前节点而不影响迭代。
func (p *listHead[T]) forEach(cb func(T) bool) {
	for pos := p.next; pos != p; {
		next := pos.next
		if !cb(pos.data) {
			break
		}
		pos = next
	}
}
