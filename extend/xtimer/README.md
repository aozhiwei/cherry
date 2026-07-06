# xtimer — Linux 内核时间轮的 Go 移植

## 概述

xtimer 是 Linux 内核**多级时间轮（Hierarchical Timer Wheel）**的 Go 语言移植，提供 O(1) 插入/删除的高性能定时器实现。

## 运行模型

xtimer 运行于**单线程环境**，所有操作在同一个 goroutine 内串行执行：

- `Update()` / `After()` / `Delete()` 等所有 API 必须在同一线程调用
- 无锁设计，不包含 `sync.Mutex` 或原子操作
- 回调函数在 `Update()` 的调用栈中同步执行，非异步回调
- 多 goroutine 并发调用会导致数据竞争，需外部加锁

**单线程设计优势：** 零锁开销，O(1) 操作不受竞争影响；回调重入安全无需考虑并发重入；代码简单可控。

---

## 抽象定时器

xtimer 是一个**与真实时间解耦**的抽象滴答定时器：

- `tick` 函数由外部注入，滴答的含义完全由调用方定义
- 不依赖 `time.Now()`、`time.Ticker` 等实时时钟
- 定时器精度为 1 滴答，滴答粒度可任意设定

> **必须使用单调时间：** 当滴答代表真实时间时，`tick` 函数必须基于**单调时钟**（monotonic clock）实现，严禁使用 `time.Now().UnixMilli()` 等挂钟时间。
>
> **原因：** 挂钟时间受 NTP 校时、闰秒、用户手动调时等因素影响，可能**回退**或**跳跃**。
>
> - **时间回退** — NTP 向后校准导致 `tick()` 返回值变小，`tick < base` 打破时间轮 `tick >= base` 的基本假设，定时器到期检测失效
> - **闰秒** — 闰秒插入/删除使同一秒出现两次或缺失，定时器可能重复触发或漏触发
>
> Go 中获取单调时间的推荐方式：
> ```go
> start := time.Now()
> tick := func() int64 { return time.Since(start).Milliseconds() }
> ```
> `time.Since()` 内部使用 `runtime.nanotime()` 单调时钟源，不受系统时间调整影响。

**适用场景：**

| 场景 | 滴答含义 | tick 函数示例 |
|------|----------|---------------|
| 回合制游戏 | 回合数 | `func() int64 { return turnCount }` |
| 帧同步 | 帧号 | `func() int64 { return frameID }` |
| 技能冷却 | 逻辑帧 | `func() int64 { return logicTick }` |
| AI 决策间隔 | 决策步 | `func() int64 { return step }` |
| 实时系统 | 毫秒 | `func() int64 { return time.Since(start).Milliseconds() }` |

**回合制综合示例：**

```go
var turn int64
t := &Timer{}
t.Init(func() int64 { return turn })

// Group: 所有 Buff/Debuff 归组管理
buffs := NewGroup()
debuffs := NewGroup()

// 每 3 回合回血
heal := t.EveryGroup(3, func() { fmt.Println("+10 HP") }, buffs)

// 10 回合后火焰护盾过期
shield := t.AfterGroup(10, func() { fmt.Println("护盾消失") }, buffs)

// 每 5 回合中毒扣血
poison := t.EveryGroup(5, func() { fmt.Println("-5 HP 中毒") }, debuffs)

// ---------- 回合制操作演示 ----------

// 回合 8: 获得加速 Buff → 回血从 3 回合缩短到 2 回合
turn = 8
t.Update()
heal.Remain()             // 查询剩余时间（输出 1）
heal.Reschedule(2)        // 修改间隔为 2 回合

// 回合 10: 护盾被驱散 → 提前删除
turn = 10
t.Update()
shield.Delete()           // 护盾消失（不再触发过期回调）

// 回合 12: 中毒被净化 → 清除所有 debuff
turn = 12
t.Update()
debuffs.Clear()           // 一键清除中毒

	// 回合 15: 战斗结束 → 清除所有 buff
	turn = 15
	t.Update()
	buffs.Clear()

// ---------- 回调重入演示 ----------

// 1. 回调中 Reschedule 自身：重伤触发重置倒计时
var deathWish *Handle
deathWish = t.After(3, func() {
	fmt.Println("濒死触发，重置倒计时")
	deathWish.Reschedule(3)
})

// 2. 回调中 Delete 自身：一次性陷阱，触发后自动移除
// forEach 预缓存 next 节点，删除当前节点不影响后续迭代，安全
var trap *Handle
trap = t.After(5, func() {
	fmt.Println("陷阱触发，陷阱消失")
	trap.Delete()
})

// 3. 回调中创建新定时器：连击链式触发
t.After(1, func() {
	fmt.Println("第1击")
	t.After(2, func() {
		fmt.Println("第2击（连击）")
	})
})


// 驱动循环
for turn = 16; turn <= 100; turn++ {
    t.Update()
}
```

**协程集成示例：**

使用 `Update` 驱动定时器、`GetMinExpires` 计算睡眠时间，实现缺省休眠、到期唤醒的事件循环：

```go
// 以毫秒为滴答的实时定时器，使用单调时钟不受 NTP 校时影响
start := time.Now()
t := &Timer{}
t.Init(func() int64 { return time.Since(start).Milliseconds() })

// 注册定时任务
t.Every(5000, func() { fmt.Println("每5秒执行") })    // 每 5 秒
t.After(30000, func() { fmt.Println("30秒后一次性") })  // 30 秒后
t.Every(1000, func() { fmt.Println("心跳") })          // 每秒心跳

// 事件循环
for {
	t.Update()                // 1. 处理到期定时器
	idle := t.GetMinExpires()          // 2. 计算离下一个定时器还有多少滴答
	if idle > 1000 {          // 3. 限制单次睡眠上限（可选）
		idle = 1000
	}
	time.Sleep(time.Duration(idle) * time.Millisecond)
}
```

**要点：**
- `Update()` 先处理到期定时器，再通过 `GetMinExpires()` 获取最近到期时间
- `GetMinExpires()` 返回绝对滴答数，调用方用 `expires - tick()` 计算睡眠时间
- 限制单次睡眠上限可避免无定时器时长时间阻塞，便于优雅退出
- 若需支持外部通知（如新定时器加入），可用 `chan` 替代 `time.Sleep`：

```go
timer := time.NewTimer(0)
<-timer.C // 排空初始触发
for {
	t.Update()
	idle := t.GetMinExpires()
	timer.Reset(time.Duration(idle) * time.Millisecond)
	select {
	case <-timer.C:
	case <-newTimerCh:      // 新定时器加入，立即重新计算
		if !timer.Stop() {
			<-timer.C       // Stop 返回 false 时需排空，防止泄露
		}
	}
}
```

**独立 goroutine 线程安全示例：**

xtimer 为单线程设计，多 goroutine 访问需通过 channel 将操作串行化到专用 goroutine：

```go
// 命令类型
type cmd struct {
	kind     string         // "after" / "every" / "delete"
	duration int64
	cb       func()
	handle   **Handle       // delete 时传入待删句柄
	resultCh chan *Handle   // 返回创建的句柄
}

// 启动定时器 goroutine，返回命令通道
func startTimer() chan<- cmd {
	ch := make(chan cmd, 64)
	go func() {
		t := &Timer{}
		start := time.Now()
		t.Init(func() int64 { return time.Since(start).Milliseconds() })
		idleTimer := time.NewTimer(0)
		<-idleTimer.C // 排空初始触发
		for {
			// 1. 处理到期定时器
			t.Update()

			// 2. 计算空闲滴答
			idle := t.GetMinExpires()
			if idle > 1000 {
				idle = 1000
			}

			// 3. 等待到期或新命令
			idleTimer.Reset(time.Duration(idle) * time.Millisecond)
			select {
			case <-idleTimer.C:
				// 到期，下一轮循环执行 Update

			case c := <-ch:
				if !idleTimer.Stop() {
					<-idleTimer.C // Stop 返回 false 时排空，防止泄露
				}
				switch c.kind {
				case "after":
					h := t.After(c.duration, c.cb)
					if c.resultCh != nil {
						c.resultCh <- h
					}
				case "every":
					h := t.Every(c.duration, c.cb)
					if c.resultCh != nil {
						c.resultCh <- h
					}
				case "delete":
					if c.handle != nil && *c.handle != nil {
						(*c.handle).Delete()
					}
				}
			}
		}
	}()
	return ch
}

// 使用示例
func main() {
	cmdCh := startTimer()

	// 从任意 goroutine 添加定时器
	resultCh := make(chan *Handle, 1)
	cmdCh <- cmd{kind: "after", duration: 5000, resultCh: resultCh,
		cb: func() { fmt.Println("5秒后触发") }}
	h := <-resultCh

	// 删除定时器
	cmdCh <- cmd{kind: "delete", handle: &h}

	// 添加间隔定时器
	cmdCh <- cmd{kind: "every", duration: 3000,
		cb: func() { fmt.Println("每3秒触发") }}
}
```

**要点：**
- 所有 `Timer` API（`After`/`Every`/`Delete`/`Update`/`GetMinExpires`）仅在专用 goroutine 内调用
- 外部 goroutine 通过 `chan cmd` 传递操作指令，channel 作为天然同步屏障
- 回调函数在专用 goroutine 内执行，如需通知外部，通过额外的 channel 发送
- `GetMinExpires()` 返回值可限制上限，避免无定时器时长时间阻塞 channel 接收

**每日固定时刻循环执行：**

计算到下一次目标时刻的滴答数，首次用 `After`，回调中用 `Every` 按 24 小时间隔循环：

```go
// 计算到下一次 05:00:00 的毫秒数
func nextDailyTrigger(hour, min, sec int) int64 {
	now := time.Now()
	target := time.Date(now.Year(), now.Month(), now.Day(), hour, min, sec, 0, now.Location())
	if !target.After(now) {
		target = target.AddDate(0, 0, 1) // 已过今天的目标时刻，取明天
	}
	return target.Sub(now).Milliseconds()
}

start := time.Now()
t := &Timer{}
t.Init(func() int64 { return time.Since(start).Milliseconds() })

// 首次在下一个 05:00:00 触发，之后每 24 小时循环
t.After(nextDailyTrigger(5, 0, 0), func() {
	fmt.Println("每日 05:00 重置")
	t.Every(24*3600*1000, func() {
		fmt.Println("每日 05:00 重置")
	})
})

// 多个固定时刻
t.After(nextDailyTrigger(12, 0, 0), func() {
	fmt.Println("每日 12:00 午间刷新")
	t.Every(24*3600*1000, func() {
		fmt.Println("每日 12:00 午间刷新")
	})
})
```

**要点：**
- `nextDailyTrigger` 计算距下一次目标时刻的毫秒滴答数，已过今天则取明天
- 首次用 `After` 锚定到目标时刻，回调中用 `Every` 以 `24*3600*1000` 毫秒（24 小时）循环
- `time.Time.Sub()` 使用单调时钟计算时间差，不受 NTP 影响
- 若需支持跨天夏令时切换，用 `target.AddDate(0, 0, 1)` 而非 `+24h` 可正确处理

**固定分秒循环执行：**

计算到下一次目标分秒的毫秒数，适合"每小时第 30 分钟刷新""每 5 分钟整点执行"等场景：

```go
// 计算到下一次 mm:ss 的毫秒数（周期 < 1 小时）
func nextMinSecTrigger(min, sec int) int64 {
	now := time.Now()
	target := time.Date(now.Year(), now.Month(), now.Day(),
		now.Hour(), min, sec, 0, now.Location())
	if !target.After(now) {
		target = target.Add(time.Hour) // 已过本小时，取下一小时
	}
	return target.Sub(now).Milliseconds()
}

// 计算到下一次指定秒数的毫秒数（周期 < 1 分钟）
func nextSecTrigger(sec int) int64 {
	now := time.Now()
	target := time.Date(now.Year(), now.Month(), now.Day(),
		now.Hour(), now.Minute(), sec, 0, now.Location())
	if !target.After(now) {
		target = target.Add(time.Minute) // 已过本分钟，取下一分钟
	}
	return target.Sub(now).Milliseconds()
}
```

```go
start := time.Now()
t := &Timer{}
t.Init(func() int64 { return time.Since(start).Milliseconds() })

// 每小时第 30 分刷新（10:30, 11:30, 12:30...）
t.After(nextMinSecTrigger(30, 0), func() {
	fmt.Println("整半点刷新")
	t.Every(3600*1000, func() {
		fmt.Println("整半点刷新")
	})
})

// 每 5 分钟整点执行（10:00, 10:05, 10:10...）
t.After(nextMinSecTrigger(0, 0), func() {
	fmt.Println("每5分钟刷新")
	t.Every(300*1000, func() {
		fmt.Println("每5分钟刷新")
	})
})

// 每 30 秒执行
t.After(nextSecTrigger(0), func() {
	fmt.Println("每30秒刷新")
	t.Every(30*1000, func() {
		fmt.Println("每30秒刷新")
	})
})
```

**要点：**
- `nextMinSecTrigger` 锚定本小时内的 mm:ss，已过则推至下一小时；`nextSecTrigger` 锚定本分钟内的秒数
- 周期固定为 3600s / 300s / 30s，`Every` 间隔与自然时间节拍对齐，不会累积漂移

**每周固定星期几执行：**

```go
// 计算到下一个星期几 HH:mm:ss 的毫秒数
func nextWeeklyTrigger(weekday time.Weekday, hour, min, sec int) int64 {
	now := time.Now()
	target := time.Date(now.Year(), now.Month(), now.Day(),
		hour, min, sec, 0, now.Location())
	// 向前推到目标星期几
	days := int(weekday - target.Weekday())
	if days <= 0 {
		days += 7
	}
	target = target.AddDate(0, 0, days)
	if !target.After(now) {
		target = target.AddDate(0, 0, 7) // 今天即是目标星期但时间已过
	}
	return target.Sub(now).Milliseconds()
}

start := time.Now()
t := &Timer{}
t.Init(func() int64 { return time.Since(start).Milliseconds() })

// 每周一 05:00 重置
t.After(nextWeeklyTrigger(time.Monday, 5, 0, 0), func() {
	fmt.Println("周一 05:00 周重置")
	t.Every(7*24*3600*1000, func() {
		fmt.Println("周一 05:00 周重置")
	})
})

// 每周五 20:00 活动结算
t.After(nextWeeklyTrigger(time.Friday, 20, 0, 0), func() {
	fmt.Println("周五 20:00 结算")
	t.Every(7*24*3600*1000, func() {
		fmt.Println("周五 20:00 结算")
	})
})
```

**每月初固定日期执行：**

```go
// 计算到下一个月初几号 HH:mm:ss 的毫秒数
func nextMonthlyTrigger(day, hour, min, sec int) int64 {
	now := time.Now()
	target := time.Date(now.Year(), now.Month(), day,
		hour, min, sec, 0, now.Location())
	if !target.After(now) {
		target = time.Date(now.Year(), now.Month()+1, day,
			hour, min, sec, 0, now.Location())
	}
	return target.Sub(now).Milliseconds()
}

// 每月 1 号 05:00 月重置 — 回调中用 Reschedule 重新计算下月间隔
h := t.Every(nextMonthlyTrigger(1, 5, 0, 0), func() {
	fmt.Println("月初 05:00 月重置")
	h.Reschedule(nextMonthlyTrigger(1, 5, 0, 0)) // 每月天数不同，重新计算
})

// 每月 15 号 12:00 结算（2 月不足 29 天时 15 号必然存在；业务可自行判断）
```

**要点：**
- 周期跨度为 7 天或 1 月的场景不适用固定 `Every` 间隔：月天数不固定（28~31 天），`7*24*3600*1000` 虽固定但在夏令时切换日可能偏移 1 小时
- 月度用 `Every` + 回调内 `h.Reschedule(nextMonthlyTrigger(...))` 模式，每次触发后重新计算下月间隔（28/29/30/31 天不等）
- `nextMonthlyTrigger` 使用 `now.Month()+1` 推至下月，Go 的 `time.Date` 会自动处理跨年（12 月 + 1 → 次年 1 月）

## 特性

| 特性 | 说明 |
|------|------|
| **海量定时器** | 5 级时间轮覆盖约 70 年滴答范围，支持百万级同时活跃定时器 |
| **Timer Group** | 批量管理定时器生命周期，一键清除组内所有定时器 |

| **回调重入** | 回调中可安全增删改定时器（自身或他人） |
| **防死循环** | `processTimerList` 先 `replaceInit` 清空槽位再处理，回调中新增定时器进入空槽而非当前批次，天然规避回调中持续添加定时器导致 `Update` 无限循环 |
| **节点复用** | 空闲链表缓存 timerList，减少 GC 压力 |
| **PanicHandler** | 可注册 panic 处理回调，决定吞掉或重新抛出异常 |
| **无堆分配** | 除 `alloc` 新建节点外，其他操作均零堆分配 |

---

## 多级时间轮原理

```
        tv5[64]    tv4[64]    tv3[64]    tv2[64]    tv1[256]
        ──────     ──────     ──────     ──────     ──────
Tick ────────────────────────────────────────────────────────►
        级联 ↓      级联 ↓      级联 ↓      级联 ↓
        tv4[slot]  tv3[slot]  tv2[slot]  tv1[0..255]  到期执行

每一级覆盖范围：
  tv1: 0 ~ 255 滴答        (8bit, 256 槽, 每槽 1 滴答)
  tv2: 256 ~ 16K 滴答      (6bit, 64 槽, 每槽 256 滴答)
  tv3: 16K ~ 1M 滴答       (6bit, 64 槽, 每槽 16K 滴答)
  tv4: 1M ~ 67M 滴答       (6bit, 64 槽, 每槽 1M 滴答)
  tv5: 67M ~ 4.3G 滴答     (6bit, 64 槽, 每槽 67M 滴答)
```

**级联（Cascade）：** 当 tv1 走完一轮（index 归零），从 tv2 当前槽位搬运定时器到 tv1；tv2 归零时从 tv3 搬运，以此类推。类似钟表的秒针→分针→时针传动。

---

## 单级 vs 多级时间轮

### 单级时间轮

使用**单层数组**实现：数组大小 = 最大超时滴答数。

```
单级时间轮：[slot0][slot1][slot2]...[slotN]   N = maxDuration
                                        ↑
                                    定时器到期直接取出
```

| 项目 | 单级时间轮 |
|------|-----------|
| 插入 | O(1) |
| 删除 | O(1) |
| 空间 | O(maxDuration) |
| 长超时 | 需 N 极大，内存爆炸 |
| 短超时 | 浪费大量空槽 |

**问题：** 若最大超时为 100 万滴答，需要 100 万个槽位（~8MB）；若为 10 亿滴答，需要 10 亿槽位（~8GB）— 不可接受。

### 多级时间轮

使用**多层级联数组**：精度和范围分层解耦。

```
tv1 [256]  ← 每槽 1 滴答，精度最高
tv2 [64]   ← 每槽 256 滴答
tv3 [64]   ← 每槽 16K 滴答
tv4 [64]   ← 每槽 1M 滴答
tv5 [64]   ← 每槽 67M 滴答，范围最大
```

| 项目 | 多级时间轮 |
|------|-----------|
| 插入 | O(1) |
| 删除 | O(1) |
| 空间 | O(固定 512 槽) |
| 长超时 | 通过层级扩展，槽数不变 |
| 短超时 | tv1 提供精细粒度 |
| 级联开销 | 每 256 滴答级联一次，均摊 O(1) |

### 空间对比

```
单级 1亿滴答范围：100,000,000 × 8B ≈ 800 MB
多级 1亿滴答范围：512 × 8B ≈ 4 KB （压缩比 200,000:1）
```

**核心优势：** 用固定 512 个槽位覆盖任意范围的超时，通过层级级联实现"精度在低层，范围在高层"的分离，内存恒定不随 maxDuration 增长。

---

## Handle 设计：弱引用

`Handle` 是一个**弱引用句柄**，不阻止定时器被删除或回收：

| 对比 | 强引用 | Handle（弱引用） |
|------|--------|------------------|
| 持有者删除 | 定时器存活 | `node` 被置 nil |
| 内存管理 | 持有者负责 | Timer 自动管理 |
| 访问前 | 无需检查 | 必须 `Valid()` 检查 |
| 过期后 | 泄漏风险 | GC 友好 |

**弱引用设计原因：**

```
Without Handle (strong ref):
  持有者 ──→ timerList ←── Timer
                ↑
           Timer 无法回收（循环引用 / 引用计数）

With Handle (weak ref):
  持有者 ──→ Handle → node(timerList) ←── Timer
                ↕
          Delete 时 node = nil（自动断开）
```

- **避免循环引用：** 如果外部持有 `*timerList` 强指针，Timer 回收时必须遍历所有持有者通知失效。弱引用由 Timer 单方面置 nil
- **自动失效：** `Delete()` 或 `Clear()` 后 `Handle.Valid()` 立即返回 false，无需外部协同
- **零泄漏：** `Handle` 仅 8 字节（一个指针），不计入 Timer 生命周期管理

使用模式：
```go
h := timer.After(100, task)
// ...
if !h.Valid() { return }  // 定时器已被删除
h.Reschedule(50)           // 安全：内部已 Valid 检查
h.Remain()                 // 安全
h.Delete()                 // 幂等：已失效则无操作
```

---

## 接口性能分析

### Timer（定时器主体）

| 函数 | 时间复杂度 | 说明 |
|------|-----------|------|
| `Init` | O(K) | K = 所有槽位数 = 256+64+64+64+64+1 = 513，常数级 |
| `Update` | O(1) + O(M·C) | 每滴答处理 M 个到期定时器，C 为级联重分布开销 |

| `After` | O(1) | 分配或复用节点 → 入队 |
| `Every` | O(1) | 同上，类型为 EVERY |
| `AfterGroup` | O(1) | 同上 + Group 链表追加 |
| `EveryGroup` | O(1) | 同上 |
| `GetMinExpires` | O(1) | 返回 1 或 tick-base 差距 |
| `NewGroup` | O(1) | 仅初始化链表头 |

### Handle（定时器句柄）

| 函数 | 时间复杂度 | 说明 |
|------|-----------|------|
| `Valid` | O(1) | 两次指针判空 |
| `Delete` | O(1) | 从时间轮和 Group 链表双卸载 |
| `Reschedule` | O(1) | 卸载→修改→重新入队 |
| `Remain` | O(1) | expires - tick() 即为剩余滴答 |

### Group（定时器组）

| 函数 | 时间复杂度 | 说明 |
|------|-----------|------|
| `Clear` | O(N) | N 为组内定时器数量，逐个 Delete |

### 空间复杂度

| 项目 | 复杂度 | 说明 |
|------|--------|------|
| 时间轮静态槽位 | O(512) | 5 级轮固定数组，约 4KB |
| 单个定时器节点 | O(1) | timerList + Handle 约 200B |
| 空闲节点池 | O(cacheNum) | 可配置上限，超出 GC 回收 |

### 批量定时器全流程实测（1000 次平均）

百万定时器同一时刻到期，测试函数内部循环 1000 次，三阶段对比：

| 阶段 | 10万 (受GC干扰) | 10万 (不受GC干扰) | 10万 (+PreAlloc) | 100万 (受GC干扰) | 100万 (不受GC干扰) | 100万 (+PreAlloc) |
|------|------------|------------|-----------------|-------------|-------------|-----------------|
| create | 13.6ms | 16.4ms | **4.0ms** | 142.3ms | 179.8ms | **61.2ms** |
| cascade | 3.7ms | 1.4ms | **0.9ms** | 32.3ms | 30.8ms | **17.1ms** |
| cas+fire | 4.9ms | 5.9ms | **2.9ms** | 48.3ms | 45.7ms | **39.8ms** |
| fire | 1.2ms | 4.5ms | 2.1ms | 16.0ms | 14.9ms | 22.7ms |

- **PreAlloc 全维度最优**：百万级 create 3x 快（180→61ms），cascade 1.9x 快（31→17ms）——预分配节点连续排列，免堆分配 + cache 局部性
- cascade 不受 GC 干扰更快：`runtime.GC()` 在计时前完成，`Update()` 区间内零 STW 打断
- create/fire 不受 GC 干扰更慢：GC 清空分配器缓存，整理堆打散节点连续性
- 百万级不受 GC 干扰时 cascade 仅快 5%——工作集 ~200MB 远超 L3，内存墙主导
- 测试文件：`timer_test.go`（`TestHundredThousandTimersCascade` / `TestMillionTimersCascade`）

---

## 回调重入支持

```
                 ┌──────────────────────┐
                 │  timer.cb() 回调执行  │
                 ├──────────────────────┤
                 │ ✓ After() 添加新定时器 │
                 │ ✓ h.Delete() 删自身   │
                 │ ✓ h2.Delete() 删其他  │
                 │ ✓ h.Reschedule() 改期 │
                 │ ✓ h2.Reschedule()     │
                 └──────────────────────┘
```

关键实现细节：
- `processTimerList` 通过 `listHead.forEach` 的 next 预缓存机制（见 list.go bug 修复），确保当前节点被删除后迭代不受影响
- `timerList.isDeleted()` 通过 `handle == nil` 判断定时器是否已被删除，`processTimerList` 中回调后检查此标记决定是否执行 remove/reschedule
- `alloc/recycle` 使用空闲链表，新建/回收均在 O(1) 完成

---

## 自动 GC

xtimer 内置**按需 GC 机制**，在定时器删除时若空闲节点超出上限，自动创建 After 定时器触发回收，避免 `freeList` 无限增长。

### 触发条件

`remove()` 中 `recycle` 后将节点归还 `freeList`，若 `freeNum > cacheNum` 且无正在运行的 gc 定时器，则创建新的 After 定时器在 4000~5023 tick 后触发（用 `tick & 1023` 抖动）。

```go
if !isGC && p.freeNum > p.cacheNum && !p.gcHandle.Valid() {
    p.gcHandle = p.After(GcReschedMin+(p.tick()&1023), p.gc)
}
```

### gc 执行

`gc()` 单次最多回收 `GcBatch`（1000）个节点。回收后若 `freeNum` 仍超限，自动创建下一个 After 定时器继续回收。

```go
func (p *Timer) gc() {
    for i := 0; i < GcBatch && p.freeNum > p.cacheNum; i++ {
        t := p.freeList.firstEntry()
        t.entry.delInit()
        p.freeNum--
    }
    if p.freeNum > p.cacheNum {
        p.gcHandle = p.After(GcReschedMin+(p.tick()&1023), p.gc)
    }
}
```

### 设计要点

| 要点 | 说明 |
|------|------|
| **按需创建** | gc 定时器仅在 `freeNum > cacheNum` 时创建，空闲时不存在 |
| **After 类型** | 触发一次后自动删除，若需继续回收则由 `gc()` 自行创建下一个 |
| **防抖** | gc 已存在时不重复创建，避免堆积 |
| **自身豁免** | gc 定时器自身的节点回收不会触发新的 gc，防止死循环 |
| **位运算抖动** | `tick & 1023` 替代取模/随机数，单指令完成，分散不同 Timer 实例的 gc 触发时刻 |
| **分批回收** | 单次最多回收 1000 个，超出留给后续 gc 周期 |

### 相关常量

| 常量 | 值 | 说明 |
|------|-----|------|
| `GcReschedMin` | 4000 | gc 触发延迟（tick） |
| `GcBatch` | 1000 | 单次 gc 回收上限 |

---

## PanicHandler

`Timer` 提供 `SetOnPanic()` 注册异常处理回调，当定时器回调发生 panic 时，由用户决定吞掉还是重新抛出：

```go
type PanicHandler func(cb TimerCb, recovered any) bool

// 返回 true 吞掉 panic，定时器继续正常生命周期
timer.SetOnPanic(func(cb TimerCb, r any) bool { return true })

// 返回 false 重新抛出 panic
timer.SetOnPanic(func(cb TimerCb, r any) bool { return false })
```

未设置时（默认 nil），panic 直接传播，不经过 recover。

---

## duration 类型

定时器 duration 和 gcInterval 均为 `int64` 类型，与 `expires`/`base`/`tick` 保持一致，避免隐式类型转换。

---

## 文件结构

```
xtimer/
├── timer.go        ← Timer 时间轮主体（Init/Update/After/Every/GetMinExpires）
├── handle.go       ← Handle 句柄（Valid/Delete/Reschedule/Remain）
├── group.go        ← Group 定时器组（Clear）
├── list.go         ← 泛型侵入式双向循环链表
├── timerlist.go    ← timerList 定时器节点

├── timer_test.go   ← 定时器单元测试
└── README.md       ← 本文档
```

---

## 参考资料

- **Linux 内核时间轮源码：** https://github.com/torvalds/linux/blob/master/kernel/time/timer.c
- **腾讯云：Linux定时器时间轮算法（图文推导）：** https://cloud.tencent.com.cn/developer/article/1740969
- **图解｜低精度定时器原理：** https://cloud.tencent.com.cn/developer/article/2322476
- **多级时间轮定时器原理与嵌入式实现：** https://blog.csdn.net/weixin_34945060/article/details/159307053
