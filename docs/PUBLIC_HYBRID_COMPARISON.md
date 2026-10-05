# 公开混合对照（P）实现说明（M5B）

## 定位
P 是**独立实现层面**的公开混合对照：Go TLS 1.3 + X25519MLKEM768，完整双向证书认证。
它不是国密 Suite 3，不替代 T/H，也不改变 T/H 的任何密码路径（AES/SHA 等 TLS 标准依赖
只出现在 P 路径）。业务处理与 T/H 共用 `app.HandleControl`（同一种 control 语义与幂等），
因此吞吐差值反映**整体实现**差异，不能归因于 ML-KEM 单项，也不声称等价后量子认证强度。

## 实现要点（internal/pbench + cmd/pbench）
- TLS 1.3 only（MinVersion=MaxVersion=VersionTLS13）；双方 CurvePreferences 仅
  X25519MLKEM768。
- 服务端 ClientAuth=RequireAndVerifyClientCert，ClientCAs=测试 CA；
  客户端 RootCAs=同 CA，ServerName 绑定 `gateway.edgehybrid.test`；
  客户端证书 CN=`D001`（即预期设备身份），服务端在握手后核对 PeerCertificate CN。
- 会话票据禁用、ClientSessionCache=nil、无 0-RTT；`DidResume=true` 不计完整握手样本。
- 未使用 InsecureSkipVerify / KeyLogWriter / 秘密导出。
- 帧格式：TLS 之上的换行分隔 JSON（与 T/H 的 SM4-GCM 记录帧不同，单列说明）。
- 连接标识：独立公开 `connection_id`（`p-<run_id>-<n>`），不冒充国密 SID。

## 实测协商（本机 Go 1.27.1，10/10 全过）
Version=TLS1.3；CipherSuite=TLS_AES_128_GCM_SHA256；CurveID=X25519MLKEM768；
HandshakeComplete=true；DidResume=false；PeerCN=gateway.edgehybrid.test。
结果中记录的是 ConnectionState 实读值，非配置推断。

## 拒绝测试（全部符合预期）
- 错误 ServerName（wrong.host.test）→ 客户端证书校验失败；
- 缺客户端证书 → 服务端 `certificate required` alert（TLS1.3 下客户端经写读探测观察到）；
- 组不匹配（客户端仅 MLKEM768 vs 服务端仅 P-256）→ `handshake failure`。
原始记录：`results/m5b/p-rejections/p_rejections.jsonl`。

## 与 T/H 的协议差异清单
| 项 | T/H | P |
|---|---|---|
| 身份认证 | SM2 证书/签名 | ECDSA P-256 证书 |
| 密钥协商 | SM2MQV（T）+ ML-KEM-768（H） | X25519 + ML-KEM-768 |
| KDF | HKDF-SM3 | TLS 1.3 HKDF-SHA256 |
| 记录保护 | SM4-GCM | AES-128-GCM |
| 帧 | 4 字节长度前缀二进制帧 | 换行分隔 JSON |
| 会话标识 | SM2 派生 16B SID（32 hex） | 独立 connection_id |
