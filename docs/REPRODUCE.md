# 构建与实验复现手册

解压 M6 审查包后进入 edgehybrid-agent-starter。Go实际测量版1.27.1 windows/amd64，go.mod最低1.25.0；Python原证据3.12.10。依赖按 go.mod/go.sum 固定，不关闭校验服务。所有命令都在工程根目录运行。本文的普通 Go 命令使用已加入 PATH 的 Go。

```powershell
go version
go mod verify
go test -json -count=1 ./... > current_go_tests.jsonl
# 必须立即保存真实进程码，不能用提取器状态代替。
$testExit = $LASTEXITCODE
go list ./...
go vet ./...
New-Item -ItemType Directory -Force ../EdgeHybrid_Rebuild_Bin
go build -o ../EdgeHybrid_Rebuild_Bin/ ./cmd/...
```

若运行 scripts/run_checked.py，先将脚本中 GO 常量设置为本机 Go 的绝对路径，改动会形成新源码 hash，旧 M5 不是该版本的测量。更方便的严格复核按原工具链路径在当前机运行：每命令记录 stdout/stderr/command.json/exit.txt，示例见 results/raw/m6_20261001/regression。summarize-go-test-json.py 接受 --go-exit-code、逐项 --expect-package、--require-tests、--json 输出。expected包从同一树的 go list ./... 获取，不能硬填PASS。

M6原 A/B 可离线核对：

```powershell
python scripts/m6_verify.py --out m6_local_verified.json
python scripts/m6_materials.py
```

材料重算脚本读取既有 raw，写 M6 新材料，不改 M5。正常生成式用例可用 `go test -json -count=1 -run '^TestM6Structured$' ./internal/handshake`。独立复现三个用例用 `-run '^TestM6Structured$/(T_sigB|H_finB|H_implicit)$'`。若要重新执行完整 A/B，使用 `python scripts/m6_compare.py --out results/raw/m6_reproduce_NEW --run-id m6-reproduce-NEW`，先配置脚本 GO；必须选不存在的输出目录和新的 run_id。隔离副本只执行 go test，结束由临时目录上下文清理，不构建生产应用。每个正常回归期望0；每个缺陷 A/B期望1，还须实际目标 M6REACH 和失败断言，不能单看非0码。

M5原证据索引在 results/raw/m5_formal_20261001/data/results_index.json；完整复核实际命令见本轮 regression/m5_recompute，16个验收都调用原 bench/pbench -verify 并重算。原记录内绝对路径是原机位置；外机用当前解压根替换 `/results/raw/m5_formal_20261001/` 前缀，保留相对后缀。二进制不在审查包，需先 build bench/pbench 到包外目录，freeze记录的 producer hash只用来识别原采样程序，不把新重建hash冒充原hash。M5的 measurement_source_resolution.json保留原报告版本映射，M6新增源码的跨阶段解析见 results/raw/m6_20261001/source_resolution.json。

正式重新采样不是本轮验收条件。需要新采样时遵循 FORMAL_EXPERIMENT_PLAN.md，在新目录重新prepare/freeze，停止Demo和其他实验，原参数T/H/P各1000、T/H各5×100资源、吞吐各5×200；不向旧结果目录追填。m5_formal_run.py对source/binary/plan有严格冻结守卫，交付源码新增M6后旧freeze运行入口应拒绝，不绕开守卫。失败/超时/中断保留，固定分母，禁止补成功或修改旧raw。

P包括TLS1.3真实混合组和双向证书，并非Suite3。资源是100握手进程批次，未测单次指标保持null。全部性能图表数据由 m6_materials.py 从原逐次/逐轮文件重算，chart_data.json附原路径和hash。答辩构建 scripts/m6_deck.mjs、PDF说明书构建 scripts/m6_pdf.py 使用Codex捆绑artifact-tool/reportlab，不是生产Go依赖；需本机存在对应包/字体，渲染记录仅证明本机生成和检查，不证明各版本PowerPoint行为。Word草稿m6_docx.py已执行，但render_docx.py因缺LibreOffice失败；Word也未安装，所以草稿保留在包外，不交作已验证文档。正式说明书为Markdown和PDF，PPTX可编辑。

归档验证：`python scripts/package_review.py --verify ../EdgeHybrid_M6_Review_Package.zip`。MANIFEST逐条覆盖包内文件（自身除外）。不分享runtime/private/私钥/令牌，不嵌套旧ZIP或exe。race/独立SM2交换没有新执行，继续pending。
