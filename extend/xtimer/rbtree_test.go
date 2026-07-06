package xtimer

import (
	"sort"
	"testing"
)

func TestRBTreePutGet(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	tr.Put(5, 50)
	tr.Put(3, 30)
	tr.Put(7, 70)

	v, ok := tr.Get(5)
	if !ok || v != 50 {
		t.Fatalf("Get(5): ok=%v, v=%d", ok, v)
	}
	v, ok = tr.Get(3)
	if !ok || v != 30 {
		t.Fatalf("Get(3): ok=%v, v=%d", ok, v)
	}
	v, ok = tr.Get(7)
	if !ok || v != 70 {
		t.Fatalf("Get(7): ok=%v, v=%d", ok, v)
	}
	_, ok = tr.Get(99)
	if ok {
		t.Fatal("Get(99) should not exist")
	}
}

func TestRBTreePutReplace(t *testing.T) {
	tr := &RBTree[int64, string]{}
	tr.Put(1, "a")
	tr.Put(1, "b")
	v, ok := tr.Get(1)
	if !ok || v != "b" {
		t.Fatalf("Put should replace: ok=%v, v=%s", ok, v)
	}
}

func TestRBTreeRemove(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	tr.Put(5, 50)
	tr.Put(3, 30)
	tr.Put(7, 70)

	tr.Remove(3)
	_, ok := tr.Get(3)
	if ok {
		t.Fatal("key 3 should be removed")
	}
	v, ok := tr.Get(5)
	if !ok || v != 50 {
		t.Fatal("key 5 should remain")
	}
	v, ok = tr.Get(7)
	if !ok || v != 70 {
		t.Fatal("key 7 should remain")
	}
}

func TestRBTreeRemoveRoot(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	tr.Put(5, 50)
	tr.Remove(5)
	_, ok := tr.Get(5)
	if ok {
		t.Fatal("root should be removed")
	}
	tr.Put(5, 55)
	v, ok := tr.Get(5)
	if !ok || v != 55 {
		t.Fatal("should reinsert after remove root")
	}
}

func TestRBTreeLeft(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	if tr.Min() != nil {
		t.Fatal("empty tree should return nil")
	}
	tr.Put(5, 50)
	tr.Put(3, 30)
	tr.Put(7, 70)
	n := tr.Min()
	if n == nil || n.key != 3 {
		t.Fatalf("Left should be 3, got %v", n)
	}
}

func TestRBTreeClear(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	tr.Put(1, 10)
	tr.Put(2, 20)
	tr.Clear()
	_, ok := tr.Get(1)
	if ok {
		t.Fatal("tree should be empty after Clear")
	}
	if tr.Min() != nil {
		t.Fatal("Left should be nil after Clear")
	}
}

func TestRBTreeForEach(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	for i := int64(0); i < 100; i++ {
		tr.Put(i, i*10)
	}

	var keys []int64
	tr.ForEach(func(k int64, v int64) bool {
		keys = append(keys, k)
		return true
	})

	if len(keys) != 100 {
		t.Fatalf("ForEach should visit 100 nodes, got %d", len(keys))
	}
	if !sort.SliceIsSorted(keys, func(i, j int) bool { return keys[i] < keys[j] }) {
		t.Fatal("ForEach should be in-order")
	}
}

func TestRBTreeForEachStop(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	for i := int64(0); i < 50; i++ {
		tr.Put(i, i)
	}

	count := 0
	tr.ForEach(func(k int64, v int64) bool {
		count++
		return k < 5
	})

	if count != 6 {
		t.Fatalf("should stop at key 5 (0..5 = 6 calls), got %d", count)
	}
}

func TestRBTreeLargeScale(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	const N = 10000
	for i := int64(0); i < N; i++ {
		tr.Put(i, i*2)
	}
	for i := int64(0); i < N; i++ {
		v, ok := tr.Get(i)
		if !ok || v != i*2 {
			t.Fatalf("Get(%d) failed", i)
		}
	}
	// Remove even keys
	for i := int64(0); i < N; i += 2 {
		tr.Remove(i)
	}
	// Odd keys remain
	for i := int64(0); i < N; i++ {
		_, ok := tr.Get(i)
		if i%2 == 0 && ok {
			t.Fatalf("key %d should be removed", i)
		}
		if i%2 == 1 && !ok {
			t.Fatalf("key %d should remain", i)
		}
	}
}

func TestRBTreeStringKey(t *testing.T) {
	tr := &RBTree[string, int]{}
	tr.Put("c", 3)
	tr.Put("a", 1)
	tr.Put("b", 2)

	n := tr.Min()
	if n == nil || n.key != "a" || n.value != 1 {
		t.Fatal("Left should be 'a':1")
	}
	v, _ := tr.Get("b")
	if v != 2 {
		t.Fatalf("Get(b) = %d", v)
	}
}

func TestRBTreeExtractBefore(t *testing.T) {
	tr := &RBTree[int64, string]{}
	for i := int64(0); i < 100; i++ {
		tr.Put(i, "x")
	}

	// 裁剪前50个
	var count int
	tr.ExtractBefore(49, func(k int64, v string) bool {
		count++
		return true
	})

	if count != 50 {
		t.Fatalf("should extract 50 nodes (0-49), got %d", count)
	}

	// 剩余节点应存在
	v, ok := tr.Get(50)
	if !ok || v != "x" {
		t.Fatal("key 50 should remain")
	}
	// 裁剪掉的不存在
	_, ok = tr.Get(25)
	if ok {
		t.Fatal("key 25 should be pruned")
	}
	// min 应更新
	n := tr.Min()
	if n == nil || n.key != 50 {
		t.Fatalf("min should be 50, got %v", n)
	}
}

func TestRBTreeExtractBeforeAll(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	for i := int64(0); i < 10; i++ {
		tr.Put(i, i)
	}

	var keys []int64
	tr.ExtractBefore(100, func(k int64, v int64) bool {
		keys = append(keys, k)
		return true
	})

	if len(keys) != 10 {
		t.Fatalf("should extract all 10, got %d", len(keys))
	}
	if tr.Min() != nil {
		t.Fatal("tree should be empty after extracting all")
	}
}

func TestRBTreeExtractBeforeStop(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	for i := int64(0); i < 50; i++ {
		tr.Put(i, i)
	}

	count := 0
	tr.ExtractBefore(49, func(k int64, v int64) bool {
		count++
		return k < 10
	})

	if count != 11 { // 0..10 = 11 nodes
		t.Fatalf("should stop at key 10, got %d", count)
	}
}

func TestRBTreeExtractBeforeNone(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	tr.Put(10, 10)
	tr.Put(20, 20)

	count := 0
	tr.ExtractBefore(5, func(k int64, v int64) bool {
		count++
		return true
	})

	if count != 0 {
		t.Fatal("should extract nothing below min key")
	}
	if tr.Min().key != 10 {
		t.Fatal("tree should be unchanged")
	}
}

func TestRBTreeExtractBeforeEmpty(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	tr.ExtractBefore(10, func(k int64, v int64) bool {
		t.Fatal("should not be called on empty tree")
		return true
	})
}

// --- 百万级压测 ---

func TestRBTreeMillionPutGet(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	const N = 1_000_000

	for i := int64(0); i < N; i++ {
		tr.Put(i, i*2)
	}
	if tr.Min() == nil || tr.Min().key != 0 {
		t.Fatal("min should be 0")
	}

	for i := int64(0); i < N; i++ {
		v, ok := tr.Get(i)
		if !ok || v != i*2 {
			t.Fatalf("Get(%d) failed", i)
			return
		}
	}
}

func TestRBTreeMillionRemove(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	const N = 1_000_000

	for i := int64(0); i < N; i++ {
		tr.Put(i, i)
	}
	for i := int64(0); i < N; i++ {
		tr.Remove(i)
	}
	if tr.Min() != nil {
		t.Fatal("tree should be empty after removing all")
	}
	if tr.root != nil {
		t.Fatal("root should be nil")
	}
}

func TestRBTreeMillionExtract(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	const N = 1_000_000

	for i := int64(0); i < N; i++ {
		tr.Put(i, i)
	}

	count := 0
	tr.ExtractBefore(N/2-1, func(k int64, v int64) bool {
		count++
		return true
	})

	if count != N/2 {
		t.Fatalf("should extract %d nodes, got %d", N/2, count)
	}
	if tr.Min().key != N/2 {
		t.Fatalf("min should be %d after extract, got %d", N/2, tr.Min().key)
	}
}

func TestRBTreeMillionForEach(t *testing.T) {
	tr := &RBTree[int64, int64]{}
	const N = 1_000_000

	for i := int64(0); i < N; i++ {
		tr.Put(i, i)
	}

	count := 0
	tr.ForEach(func(k int64, v int64) bool {
		count++
		return true
	})

	if count != N {
		t.Fatalf("ForEach should visit %d nodes, got %d", N, count)
	}
}
