# 当前发布契约

版本interactive-v1.6.1-reviewfix1。本轮主源码为当前完整Work/edgehybrid-agent-starter，不从旧ZIP覆盖。其19项原始前端资源已与待审EXE的实际HTTP资源比对一致；待审旧1.6包的源码声明不一致记录为已知发布缺陷，不能追填成正确来源。

源码审查包根：source/edgehybrid-agent-starter/；本轮原始记录在evidence/，报告在reports/，修复前反例与来源在audit-before/，差异在patches/。活动DELIVERY_INDEX以最终发布成员相对包根生成exists/hash，C盘路径只作来源说明。旧索引和报告保留为历史，不冒充当前活动验收。

Windows包包含同一源码冻结后重新构建的8个程序、内嵌资源、启动1台/20台/独立窗口及停止入口、无秘密模板、手册、构建来源和有效MANIFEST。Windows用户不依赖开发工具；scenario.exe仅为隔离固定测试，不被gateway/device生产路径引入。

排除运行私钥、会话秘密、访问凭据、缓存、.git、历史ZIP嵌套。源码审查包不含exe；运行包不预置身份材料。MANIFEST排除自身，针对ZIP的哈希回执放ZIP外。

本轮不修改H1–H4、KDF、记录编码、真实建钥、生命周期CAS、安全检查或API契约。不调整布局/样式/导航。旧M5/M6/raw及历史验证按原来源保留；当前没有新结果的项目注明未执行，不用文件名宣布全部通过。
