package xtimer

// Group 定时器组，用于批量管理定时器生命周期，不能作为值类型嵌入其他结构体。
type Group struct {
	_       noCopy
	entries ListHead[*Node]
}

type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// 创建定时器组
func NewGroup() *Group {
	g := &Group{}
	g.entries.Init(nil)
	return g
}

// 清除组内关联的所有定时器（nil接收者直接返回）
// Clear 清除组内所有定时器
func (p *Group) Clear() {
	if p == nil || p.entries.Empty() {
		return
	}
	for !p.entries.Empty() {
		t := p.entries.FirstEntry()
		// Delete函数中会把定时器从Group中删除，所以无需再Group中再做entries删除处理
		t.handle.Delete()
	}
}

// Empty 判断组是否为空
func (p *Group) Empty() bool {
	return p == nil || p.entries.Empty()
}
