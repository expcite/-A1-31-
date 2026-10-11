# M6 工作记录

目标：执行冻结的真实握手/记录结构化篡改 A/B 对照并完成最终交付材料。已实际读取AGENTS、TASKS M6、REQUIREMENTS、FORMAL_EXPERIMENT_PLAN、M5 milestone/readiness和本轮REVIEW/AGENT_NEXT_M6/M6_ACCEPTANCE；审查副本见freeze/audit。

修改文件：新增 internal/handshake/m6_structured_test.go；scripts/m6_*.py、m6_deck.mjs；docs的INNOVATION_PLAN/REPORT、FINAL_DESIGN_REPORT、USER_GUIDE、REPRODUCE、DEFENSE_QA、FINAL_DELIVERY_INDEX、REQUIREMENT_EVIDENCE_MAP。更新当前README索引和PACKAGE_INFO，results新增M6独立目录/清单/readiness和实际PPT/PDF。原五个基线测试文件、所有非_test.go生产文件、FORMAL计划及M5旧raw逐字不变。go.mod/go.sum与依赖不改。

run_id m6-20261001T021616Z；测量源码 9d0576f5ea7210752bd32ff6ed549a58fa99356f1ba5bd70a87cfed4c7fe9f8c；最终交付源码 cacc09b653a2a95e9e27ea838aa2ab181cab792373b3a1765588b672fa970778。全部旧测量文件在source_resolution.json按hash归档，新增M6来源不会追填M5。compare脚本采样后只补可选新run_id参数和同次已通过的SM2认证绑定索引；旧版本归档，不改旧原始结果。

A完整28个原顶层测试、60叶用例，B39叶用例。共同规则均16/16；三个缺陷A/B各检出3/3，漏检/不可达均0。D1/D3后续防线仍拒绝，记录阶段退化；D2非法获得Channel但业务未知null。三条独立复现一致。B统一双端阶段、用例规则、公共帧摘要和来源，日志约112KB对A约92KB。有限成本与覆盖不能推导安全或维护效率提升。实际文本行计数A=1803，B测试文件=352，B还需脚本且A断言更多，不能换算工时。详见comparison_v2/comparison.json及INNOVATION_REPORT。

命令与真实退出码：structured_initial=0；compare、compare_r2=1（开发语法/缩进）；compare_r3=1（Go长行分块提取失败，首轮子B真实系统码未及时保存为未知）；compare_final=0。最终六个正常命令和复现=0，六个缺陷测试=1且目标M6REACH+断言实际失败。全量Go JSON=0，严格汇总=0（353顶层PASS、340子PASS、1SKIP，23包17通过6无测试）；mod verify/vet=0。四个原Python验收自测=0，新5个坏M6证据副本拒绝自测=0（不是协议攻击）。M5重算和16个实际验收命令全0。命令、stdout/stderr、timeout及exit见commands/regression。

材料：六项MD/JSON最终说明/手册/问答/需求索引、真实12页PPTX（4原生图表3表格）、4页PDF已生成逐页渲染检查。首deck两次失败是Excel数字精度/子进程runtime环境；第三次通过。字体/图表轴修订后的首次交付因旧receipt同名失败，最终deck_delivery=0。Word生成0，render=1，缺LibreOffice且无Word注册；未验证草稿保留包外，不交付未检查的DOCX。PDF生成/渲染=0，不冒称PowerPoint桌面或跨软件编辑验证。

风险与未验证：公共hash是中继收到/计划转发帧，不证明对端完整收到或内部transcript；业务未观测null。0ns原样、成功率分母和批次资源限制随所有性能材料保留。race/独立SM2交换pending，OpenSSL签名SKIP，Linux未测；不推断全面后量子认证、抗DoS、侧信道或低资源硬件效果。原Demo本轮未重跑/重发布。源码包无exe、运行秘密、缓存和旧ZIP。

交付验收：最终新增材料完成后再保存final_go_test_json及严格汇总，检查M5/Demo/原生产源码hash、源清单、ZIP全MANIFEST，解压副本重建验收程序、16批原M5复验与B正常执行。最终ZIP hash/大小和真实外部结果见相邻verification.json及日志，避免自引用改写包。下一阶段：按用户指令完成M6后停止，不自行开展后续功能或验证。
