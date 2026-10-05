# 朋友复查逐项关闭表

版本 interactive-v1.2-backend2-reviewed；冻结源 21a723b1443f33e9e9fcbfa766060f0100df780b5a8c069f21cac172a8396d1b。8 项后端缺陷/文档问题有对应证据；F07 保留未修状态。不是全部需求或全面安全认证通过。

|编号|状态|实际修改|修前证据|修后与正对照|
|---|---|---|---|---|
|F01|CLOSED_WITH_EVIDENCE|独立 cmd/gateway 本地身份/加载世代失效阻止准入并关闭拥有连接；正常与单设备吊销保持隔离|before_cli_local|final_regression/commands/go_test；cmd/gateway/TestFriendCLIGatewayLocalLifecycle：T/H 各 20 台五种场景、持久吊销/退役重启|
|F02|CLOSED_WITH_EVIDENCE|共享受控轮换事务；失败仅回滚本次未引用且内容未变的新文件；不删除赢家/不确定文件；成功提交不回滚|before_rotation|api_rotation_recovery；added_transaction_auth；final_regression（真实写入失败、CAS、赢家引用、故障恢复同 owner 重试、提交后清理失败）|
|F03|CLOSED_WITH_EVIDENCE|legacy 缺世代/0 精确推进到 1，随机后缀避免复用首代文件；CLI/API 共用服务|before_rotation|after_rotation；cli_legacy_shared；final_regression|
|F04|CLOSED_WITH_EVIDENCE|双方显式等待状态拒绝异常 ACK；未完成且 nil 返回转为业务错误，真实保护消息验证|before_protected；before_gateway_ACK_state_r2|after_wire_auth_protected；after_gateway_ACK_state；final_regression|
|F05|CLOSED_WITH_EVIDENCE|同素材真实握手/业务正对照先通过，三种目标拒绝分别核对原因；过期/未来/超时为前提失败|before_P_rejections|after_P_rejections_r3；P_actual_rejections/p_rejection_baseline.json + p_rejections.jsonl|
|F06|CLOSED_WITH_EVIDENCE|每个 owner 稳定独立 Cookie 名，保留一次口令/HttpOnly/SameSite/CSRF/Host/Origin；不要求清 Cookie|before_wire_auth|added_transaction_auth；final_regression：同 CookieJar 双真实 console、过期/停止/重启/跨来源|
|F07|PENDING_FRONTEND_AUTHORIZATION|日志选择项刷新未改，前端继续冻结|朋友静态证据|FRONTEND_FREEZE_RECEIPT.json；不是已修|
|F08|CLOSED_WITH_EVIDENCE|前缀/正文/tag 截断保留 wire/handshake/io typed 链；设备与网关保留不完整终态|before_wire_auth；before_protected|after_wire_auth_protected；final_regression|
|F09|CLOSED_WITH_EVIDENCE|当前 README/版本/交接/实现状态不再沿用历史 pending；原字节归档|review_input/SOURCE_EVIDENCE.md|README.md；docs/history；SOURCE_CHANGES.json|

证据相对 results/raw/backend_friend_20261002。before_gateway_ACK_state 首次是测试驱动编译失败，不算缺陷证明；r2 才是真实状态机失败。provisional_all_go 是开发期输入目录/测试调用兼容问题，日志原样保留；after_P_rejections/r2 是开发期基线记录路径/Go TLS 原因文本匹配问题，r3 后同例通过。所有早期失败保留，不用修后 PASS 覆盖原记录。

异常 ACK 的攻击前提是通过认证的端点发出受保护消息；不宣称未认证任意远程崩溃。Cookie 名隔离防覆盖，不是对同机恶意进程的强隔离。轮换跨进程管理仍单写者，CAS 保障同 Registry 内的并发，未声称跨进程锁。
