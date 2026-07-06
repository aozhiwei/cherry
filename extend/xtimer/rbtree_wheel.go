package xtimer

import "math/bits"

// rbTreeWheel 单轮+红黑树定时器后端。
// 近程（expires-base < wheelSize）O(1)入轮槽触发，
// 远程按expires分组存入红黑树，轮槽归零时批量搬入。
type rbTreeWheel struct {
	host      Host                             // 宿主接口，提供tick/base/ProcessBatch
	wheelSize int64                            // 轮容量（须为2的幂）
	wheelMask int64                            // wheelSize - 1，位运算用
	wheel     []ListHead[*Node]                // 时间轮槽位切片
	tree      *RBTree[int64, *ListHead[*Node]] // 红黑树，key=expires, value=同expires的定时器链表头
}

// NewRbTreeWheel 创建单轮+红黑树定时器后端，wheelSize自动修正为2的幂（最小2）
func NewRbTreeWheel(wheelSize int64) Backend {
	wheelSize = ceilPow2(wheelSize)
	return &rbTreeWheel{
		wheelSize: wheelSize,
		wheelMask: wheelSize - 1,
	}
}

// ceilPow2 向上取整到2的幂，最小2
func ceilPow2(n int64) int64 {
	if n <= 2 {
		return 2
	}
	return 1 << bits.Len64(uint64(n-1))
}

// Init 初始化轮槽和红黑树，绑定host
func (p *rbTreeWheel) Init(host Host) {
	p.host = host
	p.wheel = make([]ListHead[*Node], p.wheelSize)
	for i := range p.wheel {
		p.wheel[i].Init(nil)
	}
	p.tree = &RBTree[int64, *ListHead[*Node]]{}
}

// rebase 跳过空转区间，将base推进到tick或最近到期时间的较小值
func (p *rbTreeWheel) rebase(tick int64) {
	p.host.SetBase(min(tick, p.NextExpire()))
}

// UnInit 反初始化，清空所有定时器
func (p *rbTreeWheel) UnInit() { p.Clear() }

// Update 驱动时间轮，每tick处理对应槽位并在归零时搬入树中定时器
func (p *rbTreeWheel) Update() {
	tick := p.host.Tick()
	if tick-p.host.Base() > p.wheelSize*2 {
		p.rebase(tick)
	}
	for tick >= p.host.Base() {
		index := p.host.Base() & p.wheelMask
		if index == 0 {
			p.flushTree()
		}
		p.host.ProcessBatch(&p.wheel[index])
		p.host.Step()
	}
}

// Clear 清空轮槽和红黑树中所有定时器
func (p *rbTreeWheel) Clear() {
	for i := range p.wheel {
		p.host.Drain(&p.wheel[i])
	}
	p.tree.ForEach(func(_ int64, head *ListHead[*Node]) bool {
		p.host.Drain(head)
		return true
	})
	p.tree.Clear()
}

// Enqueue 根据到期时间入轮槽（近程）或插入红黑树并合并同expires链表（远程）
func (p *rbTreeWheel) Enqueue(t *Node) {
	if t.expires-p.host.Base() < p.wheelSize {
		p.wheel[t.expires&p.wheelMask].AddTail(&t.Entry)
	} else {
		if head, found := p.tree.Get(t.expires); found {
			head.AddTail(&t.Entry)
		} else {
			head := &ListHead[*Node]{}
			head.Init(nil)
			head.AddTail(&t.Entry)
			p.tree.Put(t.expires, head)
		}
	}
}

// Dequeue 从链表中移除定时器，惰性清理树中最小的空分组。
func (p *rbTreeWheel) Dequeue(t *Node) {
	t.Entry.DelInit()
}

// flushTree 将红黑树中落入下一轮范围的定时器搬入对应轮槽。
// 用ExtractBefore从min起截取expires<bound的节点，搬入轮后自动从树移除。
func (p *rbTreeWheel) flushTree() {
	bound := p.host.Base() + p.wheelSize
	p.tree.ExtractBefore(bound-1, func(key int64, head *ListHead[*Node]) bool {
		if !head.Empty() {
			p.wheel[key&p.wheelMask].SpliceTail(head)
		}
		return true
	})
}

// NextExpire 位掩码回绕扫描轮槽，与红黑树Min比较，返回最近到期滴答
func (p *rbTreeWheel) NextExpire() int64 {
	nextExpire := p.host.Base() + p.wheelSize
	idx := int(p.host.Base() & p.wheelMask)
	for i := idx; i < idx+int(p.wheelSize); i++ {
		if !p.wheel[i&int(p.wheelMask)].Empty() {
			nextExpire = p.wheel[i&int(p.wheelMask)].FirstEntry().expires
			break
		}
	}
	if n := p.tree.Min(); n != nil {
		if n.key < nextExpire {
			nextExpire = n.key
		}
	}
	return nextExpire
}
