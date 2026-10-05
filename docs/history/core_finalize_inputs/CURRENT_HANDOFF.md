# 当前交接

当前interactive-v1.2-backend2-status1-releasefix2。本轮仅修用户提供Verification_Result_Package的六项缺陷，细节见docs/VERIFICATION_BUGFIX.md。旧五项发布结果属于releasefix历史来源，不替代新版复验。

前端所有资源冻结；协议和算法不改。已运行修前/修后定向测试，最终全量/真实EXE/发布验收状态以results/raw/verification_bugfix_20261003和外置回执为准，不预写全部通过。

保留已有工作区修改。旧M5/M6/backend1原始数据及采样源不变，不重新采样1000次/1800秒；标准SM2 S1/S2独立确认仍未执行，M6覆盖/检出相同。六项修复验证和发布完成后停止，不增加模块或改版界面。

## releasefix3 交接

当前interactive-v1.2-backend2-status1-releasefix3。在releasefix2基础上复核六项修复成立，另修十四项：CLI-02（尾随位置参数）、CLI-03（lifecycle拒绝条目丢状态）、BENCH-01（输出目录误报）、IDENT-01（旋转前置条件lifecycle断言失效）、API-01（写接口接受null体）、JOB-01（输出超限误记取消、回执先落盘）、AUDIT-01/AUDIT-02（轮询与被拒审计的有界化）、API-02（缺字段400）、LIFE-03（ROTATING可终止）、TRANS-01（名额耗尽改为等待）、FMT-01（全量gofmt），见docs/VERIFICATION_BUGFIX3.md。EXE必须用scripts/backend_build.py重建，旧的releasefix2二进制与新版源码不再匹配。race在本机以0xc0000139阻塞，未声称通过；协议、算法、前端资源均未改动。

