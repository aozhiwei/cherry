package xtimer

type ListHead[T any] struct {
	next *ListHead[T] // 后继节点指针
	prev *ListHead[T] // 前驱节点指针
	data T            // 节点存储的数据
}

func (p *ListHead[T]) Init(data T) {
	p.next = p
	p.prev = p
	p.data = data
}

func (p *ListHead[T]) Del() {
	p.next.prev = p.prev
	p.prev.next = p.next
}

func (p *ListHead[T]) AddTail(pnew *ListHead[T]) {
	prev := p.prev
	next := p

	next.prev = pnew
	pnew.next = next
	pnew.prev = prev
	prev.next = pnew
}

func (p *ListHead[T]) Prev() *ListHead[T] {
	if p.prev == p {
		return nil
	}
	return p.prev
}

func (p *ListHead[T]) First() *ListHead[T] {
	return p.next
}

func (p *ListHead[T]) FirstEntry() T {
	if !p.Empty() {
		return p.next.data
	}
	var zero T
	return zero
}

func (p *ListHead[T]) Replace(pnew *ListHead[T]) {
	pnew.next = p.next
	pnew.next.prev = pnew
	pnew.prev = p.prev
	pnew.prev.next = pnew
}

func (p *ListHead[T]) ReplaceInit(pnew *ListHead[T]) {
	p.Replace(pnew)
	p.next = p
	p.prev = p
}

func (p *ListHead[T]) Empty() bool {
	return p.next == p
}

func (p *ListHead[T]) DelInit() {
	p.Del()
	p.next = p
	p.prev = p
}

func (p *ListHead[T]) ForEach(cb func(T) bool) {
	for pos := p.next; pos != p; {
		next := pos.next
		if !cb(pos.data) {
			break
		}
		pos = next
	}
}

func (p *ListHead[T]) SpliceTail(other *ListHead[T]) {
	if other.Empty() {
		return
	}
	first := other.next
	last := other.prev
	p.prev.next = first
	first.prev = p.prev
	last.next = p
	p.prev = last
	other.next = other
	other.prev = other
}
