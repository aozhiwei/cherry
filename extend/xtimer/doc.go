// Package xtimer 是 Linux 内核多级时间轮（Hierarchical Timer Wheel）的 Go 语言移植。
//
// 核心设计：
//   - 单线程运行，无锁设计。所有操作必须在同一个 goroutine 内串行调用。
//   - 与真实时间解耦：滴答（tick）的含义由外部注入的 tick 函数定义，
//     可以是毫秒、帧号、回合数等任意抽象单位。
//   - 5 级时间轮覆盖约 43 亿滴答范围，固定 512 个槽位，内存恒定。
//   - 侵入式双向循环链表，零额外堆分配。
//   - 空闲链表复用 timerList 节点，减少 GC 压力。
//   - pendingNum 计数活跃定时器，Idle() 无定时器时返回 math.MaxInt64，
//     调用方可借此实现长休眠优化。
//
// 文件结构：
//   list.go            — 泛型侵入式双向循环链表
//   timer_list.go      — 定时器节点（嵌入两个链表头，分别挂在时间轮槽位和 Group 中）
//   timer.go           — 时间轮主体：Init / Update / After / Every / Idle / cascade
//   handle.go          — 弱引用句柄，外部持有者通过 Handle 管理定时器生命周期
//   group.go           — 定时器组，支持批量删除
//   list_test.go       — 链表单元测试
//   timer_test.go      — 定时器单元测试
//   timer_bench_test.go — 性能基准测试
//   README.md          — 使用文档
package xtimer
