package xtimer

import "cmp"

// rbNode 红黑树节点，嵌入key/value/颜色/父子指针
type rbNode[K cmp.Ordered, V any] struct {
	key    K
	value  V
	red    bool
	parent *rbNode[K, V]
	left   *rbNode[K, V]
	right  *rbNode[K, V]
}

// RBTree 泛型红黑树，参考Linux rbtree实现。
// 支持Put/Get/Remove/ForEach，非并发安全。
type RBTree[K cmp.Ordered, V any] struct {
	root *rbNode[K, V]
	min  *rbNode[K, V] // 缓存最小节点，O(1)获取
}

// Put 插入key-value，key已存在时替换value
func (t *RBTree[K, V]) Put(key K, value V) {
	n := &rbNode[K, V]{key: key, value: value, red: true}
	if t.root == nil {
		t.root = n
		t.min = n
		t.root.red = false
		return
	}
	if t.min == nil || key < t.min.key {
		t.min = n
	}
	p := t.root
	for {
		if key < p.key {
			if p.left == nil {
				p.left = n
				n.parent = p
				break
			}
			p = p.left
		} else if key > p.key {
			if p.right == nil {
				p.right = n
				n.parent = p
				break
			}
			p = p.right
		} else {
			p.value = value
			if t.min == nil || key < t.min.key {
				t.min = p
			}
			return
		}
	}
	t.insertFixup(n)
}

// Get 按key查找，返回value和是否找到
func (t *RBTree[K, V]) Get(key K) (V, bool) {
	p := t.root
	for p != nil {
		if key < p.key {
			p = p.left
		} else if key > p.key {
			p = p.right
		} else {
			return p.value, true
		}
	}
	var zero V
	return zero, false
}

// Remove 按key删除节点，key不存在时无操作
func (t *RBTree[K, V]) Remove(key K) {
	n := t.root
	for n != nil {
		if key < n.key {
			n = n.left
		} else if key > n.key {
			n = n.right
		} else {
			if n == t.min {
				t.min = t.successor(n)
			}
			t.deleteNode(n)
			return
		}
	}
}

// successor 返回n的中序后继，用于删除时重建min缓存
func (t *RBTree[K, V]) successor(n *rbNode[K, V]) *rbNode[K, V] {
	if n.right != nil {
		s := n.right
		for s.left != nil {
			s = s.left
		}
		return s
	}
	p := n.parent
	for p != nil && n == p.right {
		n = p
		p = p.parent
	}
	return p
}

// Min 返回最小关键字的节点，空树返回nil（O(1)）
func (t *RBTree[K, V]) Min() *rbNode[K, V] {
	return t.min
}

// ExtractBefore 将树在bound处分裂，左半（<=bound）回调后丢弃，右半成为新树。
// O(log n)分裂，O(k)遍历裁剪部分，无须逐个deleteNode。
func (t *RBTree[K, V]) ExtractBefore(bound K, cb func(key K, value V) bool) {
	left, right := t.split(bound)
	t.root = right
	t.min = nil
	if right != nil {
		right.parent = nil
		right.red = false
		t.min = right
		for t.min.left != nil {
			t.min = t.min.left
		}
	}
	t.forEach(left, &cb)
}

// split 在bound处分裂树，返回左树（<=bound）和右树（>bound）
func (t *RBTree[K, V]) split(bound K) (left, right *rbNode[K, V]) {
	if t.root == nil {
		return nil, nil
	}
	p := t.root
	for p != nil {
		if bound < p.key {
			// p > bound: p及右子树归右树
			r := p
			p = p.left
			r.left = nil
			right = joinRight(right, r)
		} else {
			// p <= bound: p及左子树归左树
			l := p
			p = p.right
			l.right = nil
			left = joinLeft(left, l)
		}
	}
	return left, right
}

// joinRight 将节点r连接到right树的最左侧
func joinRight[K cmp.Ordered, V any](right, r *rbNode[K, V]) *rbNode[K, V] {
	if right == nil {
		return r
	}
	s := right
	for s.left != nil {
		s = s.left
	}
	s.left = r
	r.parent = s
	return right
}

// joinLeft 将节点l连接到left树的最右侧
func joinLeft[K cmp.Ordered, V any](left, l *rbNode[K, V]) *rbNode[K, V] {
	if left == nil {
		return l
	}
	s := left
	for s.right != nil {
		s = s.right
	}
	s.right = l
	l.parent = s
	return left
}

// Clear 清空红黑树
func (t *RBTree[K, V]) Clear() {
	t.root = nil
	t.min = nil
}

// ForEach 中序遍历，回调返回false提前终止
func (t *RBTree[K, V]) ForEach(cb func(key K, value V) bool) {
	t.forEach(t.root, &cb)
}

// forEach 递归中序遍历，返回false停止遍历
func (t *RBTree[K, V]) forEach(n *rbNode[K, V], cb *func(K, V) bool) bool {
	if n == nil {
		return true
	}
	if !t.forEach(n.left, cb) {
		return false
	}
	if !(*cb)(n.key, n.value) {
		return false
	}
	return t.forEach(n.right, cb)
}

// insertFixup 插入后自底向上修复红黑性质
func (t *RBTree[K, V]) insertFixup(n *rbNode[K, V]) {
	for n.parent != nil && n.parent.red {
		if n.parent == n.parent.parent.left {
			u := n.parent.parent.right
			if u != nil && u.red {
				n.parent.red = false
				u.red = false
				n.parent.parent.red = true
				n = n.parent.parent
			} else {
				if n == n.parent.right {
					n = n.parent
					t.rotateLeft(n)
				}
				n.parent.red = false
				n.parent.parent.red = true
				t.rotateRight(n.parent.parent)
			}
		} else {
			u := n.parent.parent.left
			if u != nil && u.red {
				n.parent.red = false
				u.red = false
				n.parent.parent.red = true
				n = n.parent.parent
			} else {
				if n == n.parent.left {
					n = n.parent
					t.rotateRight(n)
				}
				n.parent.red = false
				n.parent.parent.red = true
				t.rotateLeft(n.parent.parent)
			}
		}
	}
	t.root.red = false
}

// rotateLeft 以x为轴左旋，将x.right提升为子树根
func (t *RBTree[K, V]) rotateLeft(x *rbNode[K, V]) {
	y := x.right
	x.right = y.left
	if y.left != nil {
		y.left.parent = x
	}
	y.parent = x.parent
	if x.parent == nil {
		t.root = y
	} else if x == x.parent.left {
		x.parent.left = y
	} else {
		x.parent.right = y
	}
	y.left = x
	x.parent = y
}

// rotateRight 以x为轴右旋，将x.left提升为子树根
func (t *RBTree[K, V]) rotateRight(x *rbNode[K, V]) {
	y := x.left
	x.left = y.right
	if y.right != nil {
		y.right.parent = x
	}
	y.parent = x.parent
	if x.parent == nil {
		t.root = y
	} else if x == x.parent.right {
		x.parent.right = y
	} else {
		x.parent.left = y
	}
	y.right = x
	x.parent = y
}

// deleteNode 删除节点z，找到后继替换后修复颜色
func (t *RBTree[K, V]) deleteNode(z *rbNode[K, V]) {
	var x, y *rbNode[K, V]
	if z.left == nil || z.right == nil {
		y = z
	} else {
		y = z.right
		for y.left != nil {
			y = y.left
		}
	}
	if y.left != nil {
		x = y.left
	} else {
		x = y.right
	}
	if x != nil {
		x.parent = y.parent
	}
	if y.parent == nil {
		t.root = x
	} else if y == y.parent.left {
		y.parent.left = x
	} else {
		y.parent.right = x
	}
	if y != z {
		z.key = y.key
		z.value = y.value
	}
	if !y.red {
		t.deleteFixup(x, y.parent)
	}
}

// deleteFixup 删除后自底向上修复红黑性质
func (t *RBTree[K, V]) deleteFixup(x, parent *rbNode[K, V]) {
	for (x == nil || !x.red) && x != t.root {
		if x == parent.left {
			w := parent.right
			if w.red {
				w.red = false
				parent.red = true
				t.rotateLeft(parent)
				w = parent.right
			}
			if (w.left == nil || !w.left.red) && (w.right == nil || !w.right.red) {
				w.red = true
				x = parent
				parent = x.parent
			} else {
				if w.right == nil || !w.right.red {
					if w.left != nil {
						w.left.red = false
					}
					w.red = true
					t.rotateRight(w)
					w = parent.right
				}
				w.red = parent.red
				parent.red = false
				if w.right != nil {
					w.right.red = false
				}
				t.rotateLeft(parent)
				x = t.root
				break
			}
		} else {
			w := parent.left
			if w.red {
				w.red = false
				parent.red = true
				t.rotateRight(parent)
				w = parent.left
			}
			if (w.right == nil || !w.right.red) && (w.left == nil || !w.left.red) {
				w.red = true
				x = parent
				parent = x.parent
			} else {
				if w.left == nil || !w.left.red {
					if w.right != nil {
						w.right.red = false
					}
					w.red = true
					t.rotateLeft(w)
					w = parent.left
				}
				w.red = parent.red
				parent.red = false
				if w.left != nil {
					w.left.red = false
				}
				t.rotateRight(parent)
				x = t.root
				break
			}
		}
	}
	if x != nil {
		x.red = false
	}
}
