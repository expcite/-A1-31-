你现在在 EdgeHybrid 项目根目录工作。请直接修改代码并执行验证，不要只给方案。

【项目】
A1-31 园区边缘设备国密与后量子混合安全通信软件设计实现。
当前是工程骨架，不是完整安全系统。用户没有基础，需要你给出清楚的运行步骤和真实验证结果。

【先读】
依次阅读 README.md、AGENTS.md、TASKS.md、docs/REQUIREMENTS.md、
docs/PROTOCOL_DRAFT.md、docs/TEST_MATRIX.csv、internal/cryptosuite/README.md。

【这一轮的范围】
只完成 M0 和 M1：核验环境、复现骨架检查、接入并验证真实密码组件。
不要这一轮实现全部握手、Web 页面、复杂会话恢复或最终性能报告。

【具体工作】
1. 检查操作系统、Go 精确版本和依赖下载条件，运行 go test ./...、go vet ./...、go build ./cmd/...。
2. 运行 go run ./cmd/lab -devices 20 -messages 3。明确这是明文通信实验，不是安全握手。
3. 使用当前受维护且提供 crypto/mlkem 的 Go 版本。访问官方文档核验 github.com/emmansun/gmsm 的接口、版本和许可证。
   固定实际采用版本，更新 go.mod/go.sum 和 docs/DEPENDENCIES.md。不要猜测版本/API，不采用同名未知 fork。
4. 在 internal/cryptosuite 实现真实 SM2 身份签名、完整 SM2 密钥协商、SM3/HMAC-SM3/HKDF-SM3、SM4-GCM 和 ML-KEM-768 适配。
   不要用普通 ECDH 替代 SM2 协商，不要用 AES/SHA-256 代替 SM4/SM3。
5. 使用可追溯的官方/上游测试向量和独立交叉检查验证组件；只有双端 round-trip 不足以确认标准一致性。
   没有可靠向量时标记这一项待核验，不编造“官方测试向量”。
6. 增加正常、错误公钥、错误身份、篡改、长度边界、随机性错误处理等测试。
   ML-KEM 的等长非法密文可能不会返回 error，要验证其共享结果不同，并为后续 Finished 验证保留测试。
7. 在测试中验证 SM2 两端共享结果一致、角色/身份绑定正确，HKDF 标签/身份/套件改变会改变输出，SM4 篡改拒绝。
8. 保留 gateway/device/bench 的未完成拒绝行为。本轮组件通过不代表完整协议已完成。
9. 更新 results/milestone_M1.md，逐项写明修改文件、执行命令、真实退出码、通过/未通过/未执行测试和未完成事项。

【硬限制】
不要记录真实密钥，不要提交 runtime/secrets，不要关闭校验绕过依赖错误，不要杜撰结果。
不要自动降级，不要把一个 readiness 布尔值改成 true 来宣布完成，不要声称原型经过安全审计。
组件验证遇到问题就给出证据与可运行复现，并保留安全失败状态。

完成后用中文告诉我：这一轮完成了什么、还有什么没完成、我在本地具体运行哪几条命令。
