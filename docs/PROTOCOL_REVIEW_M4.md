# M4 协议审查：多设备、生命周期与换钥

**状态：这是 M2/M3 之上的增量。H1–H4 与密钥日程均未改动。**

本文档记录 M4 的**协议与系统层面**审查；生命周期细节见 `docs/LIFECYCLE.md`。

---

## 0. 明确未改动

| 保留项 | 说明 |
|---|---|
| H1–H4 四消息结构 | 消息顺序、`wire.Kind`、线格式全部未变 |
| Suite 1 / Suite 2 密钥日程 | **M4 不重新设计**（`ikm` 编码、T2/T4、一次 Extract 多次 Expand，全部保持） |
| 双向 SM2 签名、完整 SM2 协商、HKDF-SM3、SM4-GCM | 未变 |
| 防降级机制（客户端重算、能力列表入转录、`kem_ct` 入转录） | 未变，测试继续回归 |
| 隐式拒绝由 Finished 捕获 | 未变，测试继续回归 |
| 禁止混合失败后自动回退传统 | 未变 |
| M2.1 的取消/超时/截断/验收器/三态观测 | 未变 |

---

## 1. 一般 K 个设备（不写死 20）

**系统中不存在任何逐设备分支。** 代码中没有 `D001`、`D010` 之类的字面量开关；
设备集是数据（模式串 + 数量），不是代码路径。

- 网关维护 `map[device_id]*SessionState`（`internal/app/sessions.go`），线程安全；
- 每台设备独立拥有：可信身份、当前密钥世代、当前会话 SID、收发记录状态、吊销状态、生命周期状态；
- **不共享**：会话计数器、AEAD 状态、记录密钥、nonce 前缀、序列号。

`cmd/fleet` 用**同一套构造逻辑**按 id 生成每台设备；`-devices` 是参数。
竞赛验收规模 20 只是传入的一个值。

### 隔离保证

两个连接不可能都认为它们拥有同一台设备的状态：`Register` 对同一 device_id 的第二个活跃会话**直接拒绝**。

---

## 2. 并发服务模型

M4 起，网关为每个接受的连接开独立 goroutine：

- 慢的或对端恶意的连接只占用一个 goroutine 和一个握手槽，**不会拖住其他设备**；
- 准入控制发生在**握手开始之前**（`Admit()`），所以连接洪泛无法造成无界的在途握手。

### 资源边界（不是完整 DoS 防御）

| 边界 | 字段 |
|---|---|
| 最大并发握手数 | `Limits.MaxHandshakes`（默认 32） |
| 最大活动会话数 | `Limits.MaxSessions`（默认 64） |
| 单身份连接上限 | `Limits.MaxPerIdentity`（默认 2） |
| 握手超时 | 每会话独立 deadline（M2.1 已有） |
| 最大帧长 | `MaxFrameBytes` |
| 断连清理 | `Unregister` + 每会话 cancel；`Admit` 的 release 保证在途计数回落 |

**这是"避免明显无界增长"，不是抗 DoS 论证。** 见 `docs/THREAT_MODEL.md`。

---

## 3. 生命周期与吊销的观测机制

详见 `docs/LIFECYCLE.md`。协议层面要点：

- 生命周期状态持久化到注册表文件（原子写，写完前用同一解析器自检）；
- **网关在每次握手前 reload** → 变更在下一次连接尝试时生效；
- **watchLifecycle 周期 reload + sweep** → 处理变更发生前已建立的会话；
- **吊销不能通过 reconnect 恢复**（状态已落盘）。

因此不需要 Web 管理界面：注册表文件就是控制面，reload 由网关内部驱动。

---

## 4. 换钥与跨会话重放

换钥 = 完整新握手（不做 in-band rekey）。安全性论证：

- 新 SID（由新的 T4 派生）；
- 新记录密钥（PRK 变了，因为转录和临时材料都变了）；
- 新 nonce 前缀；
- seq 从 0。

**即使 seq 回到 0，也不存在旧 `(key, nonce)` 重用**：密钥和前缀同时换新。

### 跨会话重放

- Session A 的记录送给 Session B → **全部拒绝**（首条、中间、最后一条）。
  B 的密钥与前缀都是新的，A 的记录在 B 下无法验证。
- **D001 的合法记录送给 D002 的会话** → **拒绝**。
  网关把载荷里的 `device_id` 与**已认证**的 peer id 绑定；不匹配即终止会话。

---

## 5. 重连

- 正常断线 → 完整重新握手 → 新会话；
- 断线过程中有未确认控制 → 记为未完成，**不**声称已确认；
- 旧连接残留记录到达 → 拒绝；
- seq 重新从 0，但 SID/密钥/nonce 前缀**全部新鲜**；
- **混合失败不自动回退传统**（M3 测试继续保留）。

---

## 6. 20 台设备验收（机器可读）

`scripts/m4-fleet-e2e.sh` + `scripts/verify_fleet_events.py`：

断言 `devices_expected == devices_authenticated == hybrid_sessions == kem_used`，
每台都有唯一 device_id、唯一 32 位十六进制 SID、`suite == 2`、`kem_used == true`；
并检查遥测与控制/ACK 的 `operation_id` 归属正确、SID 无重复、无跨设备串线。

**故障场景**：`BAD_DEVICE` 指定一台用不可信身份运行 —— 该台失败，其余全部成功。

summary 记录 `run_id`、`expected/successful/failed_devices`、`suite_2_count`、
`kem_used_count`、`unique_sid_count`、遥测与控制计数，以及
`duration`（**仅作联调耗时，不当 benchmark**）。

---

## 7. 并发正确性

新增针对会话注册表、信任 reload、吊销、轮换、Send/Close、重连的功能性并发测试。

**若 `-race` 仍因工具链环境无法运行，如实记 `ENVIRONMENT_BLOCKED`；
普通并发测试不得当作 race detector PASS。**

---

## 8. M4 不做

M5 正式性能数据、1000 次握手 benchmark、与 Go TLS 混合方案对比、GUI/Web、
数据库、0-RTT、ticket/session resumption、最终论文图表。
`cmd/bench` 继续 fail-closed。

---

## 9. 不声称的事项

- 这不是标准协议，没有形式化证明，没有外部审计；
- **Suite 2 存在 ≠ 系统抗量子**（SM2 身份认证不是后量子认证）；
- 密钥销毁是 best effort，**不是**安全擦除；
- 跨进程重启 exactly-once **未实现**；
- 资源边界**不是**抗 DoS 论证；
- `-race` 若环境阻塞，等级继续带 `PENDING`，不谎报 full verified。
