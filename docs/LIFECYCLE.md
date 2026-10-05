# 长期身份与密钥生命周期（M4）

**状态：本项目设计。不是 PKI/CA，没有外部审计。继续使用本地可信注册表。**

---

## 1. 状态机

每个**已登记身份**处于以下之一：

| 状态 | 含义 | 能否建立新会话 |
|---|---|---|
| `ACTIVE` | 当前密钥世代可用 | ✅ |
| `ROTATING` | 已准备好新世代、正在提交 | ❌（必须先提交） |
| `REVOKED` | 已吊销 | ❌ |
| `RETIRED` | 已退出服务 | ❌ |

`REVOKED` 与 `RETIRED` 刻意分开：操作员需要区分"我们停用了这台设备"和"我们禁用了这台设备"。

空值按 `ACTIVE` 处理，因此 M4 之前写好的注册表**无需改动即可继续加载**。

### 密钥世代

每个条目另带 `key_generation` 与 `retired_key_ids`：

- `key_generation` 只通过轮换递增；
- `retired_key_ids` 列出**不得再建立新会话**的 key_id——即使身份本身仍是 `ACTIVE`。

**key_id 是信任绑定的一部分**：一个出示已退役 key_id 的端点按标识符被拒绝，而不是按密钥值比较。

---

## 2. 不变式（轮换不能破坏的）

1. **签名密钥与协商密钥持续分离。** 一次设备轮换**同时**产生新的 signing *和* agreement 密钥
   （避免"新签名钥 + 旧协商钥"的混合世代，它比"全旧"或"全新"都更难推理）；
   但两类**始终是不同的密钥** —— 注册表在加载、`Replace`、`CompareAndReplace` 时都强制二者不得相同。
2. **轮换不得合并两类密钥。** `Rotation.Validate()` 拒绝 `sign_key_id == agreement_key_id`，
   也拒绝两个公钥相同。
3. **私钥绝不写日志。** 事件只含公开元数据。
4. **key_id 参与信任绑定。** 出示的 key_id 必须与注册表当前记录一致；退役的 key_id 被拒绝。
5. **过渡期结束后旧 key_id 不能建立新会话。** 由 `retired_key_ids` 强制。

---

## 3. 轮换策略：注册表原子提交 + failure-safe 密钥文件顺序

```
generation N
    ↓ 1. 写入并验证两把新密钥文件（按 key_id 命名，不动旧文件）
    ↓ 2. ApplyRotation：原子安装新 key_id + 新公钥
    ↓ 3. best-effort 退役 N 的密钥文件
generation N+1
```

### "原子"一词的准确范围（不要夸大）

| 操作 | 是否原子 |
|---|---|
| 注册表提交（`Replace`，含持久化） | ✅ 原子：全部写入或全部不写 |
| **"私钥文件 + 注册表"的整个轮换** | ❌ **不是 crash-atomic**。它是 **failure-safe**：任一步失败都留下**旧世代可用**的状态，但磁盘上可能短暂同时存在两代密钥文件 |
| 旧文件退役 | best effort，失败只是待清理，不影响安全 |

**为什么不做双密钥过渡窗口**：为一个身份同时保留两个**可用**世代，会把可被攻击的密钥面翻倍，
并让事后"这条消息到底用哪把密钥签的"变得含糊。上面的顺序里，第 2 步一旦提交，
旧 key_id 就被注册表拒绝——磁盘上即使残留旧文件，也只是待清理，不是可用的备选身份。
需求中"若保留 N 必须明确写出策略"这一点，**由记录该选择来满足，而不是实现更复杂选项**。

### 密钥文件按 key_id 命名

```
keys/<device>/<sign-key-id>.key
keys/<device>/<agreement-key-id>.key
```

两代密钥文件因此可以共存于磁盘而互不覆盖。加载时优先读按 key_id 命名的文件，
找不到再回退到 M4.1 之前的按类别命名（`signing.key` / `agreement.key`），
所以旧目录无需迁移。

### 轮换必须携带公钥

`Rotation` 同时要求新的 key_id **和新的公钥**。只装 key_id 而留下旧公钥，
会让注册表声称一把密钥文件里并不存在的密钥，之后每次加载都会在绑定检查处失败。
（`Validate` 会拒绝缺少公钥的轮换。）

### 并发语义：compare-and-replace（M4.2）

`ApplyRotation` **不是** "读出来、改一下、写回去"。它先按快照准备轮换，
然后由 `Registry.CompareAndReplace` 在**注册表写锁临界区内**校验前置条件：

- 当前 `key_generation == FromGeneration`
- 当前 `sign_key_id == RetiredSignKeyID`
- 当前 `agreement_key_id == RetiredAgreementKeyID`

不满足则返回 `ErrStaleRotation` 且**不写任何东西**。
校验放在锁内是重点：先校验再写会让两个并发轮换都通过自己的检查，
后到者覆盖先到者，甚至把世代**倒退**回已退役的密钥材料。

另外 `Rotation.Validate()` 要求 `ToGeneration == FromGeneration + 1`（严格 +1），
不允许跳号或回退。

### 作用域声明（不要夸大）

上述保证**只在同一个 Registry 实例内成立**。

> **trusted admin control plane is single-writer across processes.**
> 跨进程场景（例如两个 `secctl` 进程同时改注册表）**没有文件锁**，
> 因此**不声称**跨进程并发更新是安全的。运维上必须保证单写者。

### key_id 退役由标识符判定

`Peer.UsableForNewSessions(signKeyID, agreementKeyID)` 是**唯一判定点**：
注册表查找与握手的 key_id 绑定检查都调用它，因此二者不可能给出不同答案。

---

## 4. 吊销

```
管理员吊销 D005
    ↓ registry.Replace（持久化，原子写文件）
    ↓ 网关：握手前 reload + 周期 sweep
    ↓ D005 的现有会话被终止；新连接被拒绝
```

**机制不是"下次启动时读一次 trust.json"。** 两处保证：

1. `serve` 在**每次握手之前** reload —— 吊销/轮换在下一次连接尝试时即生效；
2. `watchLifecycle` 每 250ms reload 并 sweep —— 处理**变更发生前就已经建立**的会话。

reload 失败时保留旧视图：半写的注册表不能一次性把所有设备都踢掉。

**吊销不能通过 reconnect 恢复**：状态已持久化到注册表文件，重连会重新读到它。

---

## 5. 会话换钥

首版**不做 in-band rekey**。换钥 = 完整新握手：

```
达到阈值 → REKEY_REQUIRED，停止发送业务记录
        → 关闭旧会话
        → 完整 H1..H4
        → 新 SID / 新记录密钥 / 新 nonce 前缀 / seq 从 0
        → 继续业务
```

### 为什么即使 seq 回到 0 也不存在 (key, nonce) 重用

新会话**同时**换了密钥和 nonce 前缀：

- 密钥不同——整个密钥日程用**新的转录**和**新的临时材料**重跑；
- nonce 前缀不同——由同一 PRK 按 T4 派生，T4 也变了。

因此旧会话的任何 `(key, nonce)` 对都不会重现。旧会话的记录在新会话下也无法验证（见跨会话重放测试）。

### 触发条件

| 触发 | 字段 |
|---|---|
| A. 消息数阈值 | `RekeyPolicy.MaxRecords` |
| B. 时间阈值 | `RekeyPolicy.MaxAge` |

两者至少配置一个（`Validate` 要求）。测试使用很小的阈值（如 4 条记录）以便自动触发。

**计数器只属于当前会话**：序列与记录密钥都是每会话独立的，所以换过 5 次的设备在当前会话仍从 0 计。

---

## 6. 重连

重连**就是**一次完整新握手——从不恢复会话、从不自动改套件。

- 正常断线 → 完整重新握手 → 新会话；
- 断线时有未确认控制 → 该控制记为未完成，业务侧不会声称已确认；
- 旧连接的残留记录到达 → 被拒绝；
- 混合失败**不**自动回退传统（M3 性质继续保留）。

---

## 7. 密钥销毁

`PrivateHandle.Destroy()` 与 `FileStore.Retire()` 都是 **best effort**：
Go 不提供针对该材料的擦除原语，`Retire` 删除文件、`Destroy` 丢弃可达引用。
**不声称安全擦除。**

`Retire` 在删除前**校验文件仍持有被退役的 key_id**，因此不会误删已经替换它的新密钥。

---

## 8. 明确不做

- PKI / CA 后台；
- Web 管理界面（注册表文件即控制面）；
- 跨进程重启 exactly-once（见下）。

### operation_id 幂等

| 范围 | 状态 |
|---|---|
| 同一会话内重复 operation_id | ✅ 已实现：不重复产生副作用 |
| 跨进程重启 exactly-once | ❌ **NOT IMPLEMENTED**，除非引入持久化状态 |

M4 **不**为此引入数据库。
