package xtimer

const (
	TVN_BITS = 6
	TVR_BITS = 8
	TVN_SIZE = 1 << TVN_BITS
	TVR_SIZE = 1 << TVR_BITS
	TVN_MASK = TVN_SIZE - 1
	TVR_MASK = TVR_SIZE - 1
)

const (
	MaxScanLevels = 1
)

type (
	casadeWheel struct {
		host Host
		tv1  [TVR_SIZE]ListHead[*Node]
		tv2  [TVN_SIZE]ListHead[*Node]
		tv3  [TVN_SIZE]ListHead[*Node]
		tv4  [TVN_SIZE]ListHead[*Node]
		tv5  [TVN_SIZE]ListHead[*Node]
	}
)

// NewCascadeWheel 创建5层级联时间轮定时器
func NewCascadeWheel() Backend {
	return &casadeWheel{}
}

func (p *casadeWheel) Init(host Host) {
	p.host = host
	p.forEachSlot(func(slot *ListHead[*Node]) { slot.Init(nil) })
}

func (p *casadeWheel) UnInit() {
	p.Clear()
}

func (p *casadeWheel) Update() {
	tick := p.host.Tick()
	for tick >= p.host.Base() {
		index := p.host.Base() & TVR_MASK
		if index == 0 &&
			p.cascade(&p.tv2, p.slot(0)) == 0 &&
			p.cascade(&p.tv3, p.slot(1)) == 0 &&
			p.cascade(&p.tv4, p.slot(2)) == 0 {
			p.cascade(&p.tv5, p.slot(3))
		}
		p.host.ProcessBatch(&p.tv1[index])
		p.host.Step()
	}
}

func (p *casadeWheel) Clear() {
	p.forEachSlot(func(slot *ListHead[*Node]) { p.host.Drain(slot) })
}

func (p *casadeWheel) forEachSlot(fn func(slot *ListHead[*Node])) {
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

func (p *casadeWheel) Enqueue(t *Node) {
	idx := t.expires - p.host.Base()
	switch {
	case idx < TVR_SIZE:
		p.tv1[t.expires&TVR_MASK].AddTail(&t.Entry)
	case idx < 1<<(TVR_BITS+TVN_BITS):
		p.tv2[(t.expires>>TVR_BITS)&TVN_MASK].AddTail(&t.Entry)
	case idx < 1<<(TVR_BITS+2*TVN_BITS):
		p.tv3[(t.expires>>(TVR_BITS+TVN_BITS))&TVN_MASK].AddTail(&t.Entry)
	case idx < 1<<(TVR_BITS+3*TVN_BITS):
		p.tv4[(t.expires>>(TVR_BITS+2*TVN_BITS))&TVN_MASK].AddTail(&t.Entry)
	default:
		p.tv5[(t.expires>>(TVR_BITS+3*TVN_BITS))&TVN_MASK].AddTail(&t.Entry)
	}
}

func (p *casadeWheel) Dequeue(t *Node) {
	t.Entry.DelInit()
}

func (p *casadeWheel) cascade(tv *[TVN_SIZE]ListHead[*Node], index uint32) uint32 {
	var head ListHead[*Node]
	tv[index].ReplaceInit(&head)
	head.ForEach(func(t *Node) bool { p.Enqueue(t); return true })
	return index
}

func (p *casadeWheel) slot(index uint32) uint32 {
	return uint32((p.host.Base() >> (TVR_BITS + index*TVN_BITS)) & TVN_MASK)
}

func scanLevel[TV [TVR_SIZE]ListHead[*Node] | [TVN_SIZE]ListHead[*Node]](tv *TV, idx int, found *bool, expires *int64) bool {
	arr := tv
	mask := len(*arr) - 1
	slot := idx
	for {
		(*arr)[slot].ForEach(func(t *Node) bool {
			*found = true
			if t.expires < *expires {
				*expires = t.expires
				return false
			}
			return true
		})
		if *found {
			return idx == 0 || slot < idx
		}
		slot = (slot + 1) & mask
		if slot == idx {
			break
		}
	}
	return false
}

func (p *casadeWheel) NextExpire() int64 {
	var (
		tick    = p.host.Base()
		expires = tick + 1024
		found   = false
		idx     = int(tick & TVR_MASK)
	)
	cas := scanLevel(&p.tv1, idx, &found, &expires)
	if found && !cas {
		return expires
	}
	if idx != 0 {
		tick += TVR_SIZE - int64(idx)
	}
	tick >>= TVR_BITS
	varray := [...]*[TVN_SIZE]ListHead[*Node]{&p.tv2, &p.tv3, &p.tv4, &p.tv5}
	for i := 0; i < MaxScanLevels; i++ {
		idx = int(tick & TVN_MASK)
		if cas = scanLevel(varray[i], idx, &found, &expires); found && !cas {
			return expires
		}
		if idx != 0 {
			tick += TVN_SIZE - int64(idx)
		}
		tick >>= TVN_BITS
	}
	return expires
}
