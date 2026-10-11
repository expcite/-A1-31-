# Codex 测试补丁说明 (Overlay Change Note)

**任务**：EH-EVIDENCE-TEST-UPGRADE-v1  
**修改范围**：仅修改测试 Overlay 文件，生产代码 0 修改，生产协议/哈希/EXE 0 修改。  
**修改文件**：`internal/evidence/evx_throughput_test.go` 第 168 行  

## 1. 现象与原因分析
在首次试运行 `smoke` 阶段，9 个顶层测试组及绝大部分子测试全部 PASS，仅 1 项子测试报错：
```
--- FAIL: TestEVX07ThroughputEvidenceIntegrity/missing_events (0.02s)
evx_throughput_test.go:193: missing rejection "confirmation lacks preceding facts"; 
got [open ... gateway_events.jsonl: The system cannot find the file specified. 
confirmed observation without matching remote ACK event/timing ... round counts/bytes do not recompute ... round rates do not recompute]
```

### 根本原因
在 `evx_throughput_test.go` 中，该用例对磁盘上的事件文件执行 `os.Remove(filepath.Join(dir, ef))`（即删除整个网关事件文件）。
生产函数 `ValidateThroughput` 在事件文件不存在时，调用 `LoadEvents` 失败返回 `os.PathError`，导致解析出的事件索引为空字典。
在 `CheckThroughputRows` 中：
- `"confirmation lacks preceding facts"` 的判定逻辑为 `if cok { if !sok || !aok { ... } }`。当事件文件被整体删除时，`cok` (`control-confirmed` 事件) 根本不存在，因此该分支不会触发。
- 相反，当 `ConfirmedObserved` 为 true 但 `cok` 不存在时，触发的是第 173 行的 `"confirmed observation without matching remote ACK event/timing"`。
- 原测试代码在第 168 行期望 `"confirmation lacks preceding facts"`，实为从第 114 行（`missing_sent`，即保留 `control-confirmed` 但单独删除 `control-sent` 的内存用例）复制时遗留的断言匹配错误。

## 2. 补丁内容 (Diff)
```diff
--- a/internal/evidence/evx_throughput_test.go
+++ b/internal/evidence/evx_throughput_test.go
@@ -165,4 +165,4 @@
 		{"missing_events", func(dir string, m Manifest, rows []ThroughputRound) {
 			_, _, ef := ThroughputFiles(m)
 			if err := os.Remove(filepath.Join(dir, ef)); err != nil {
 				t.Fatal(err)
 			}
-		}, "confirmation lacks preceding facts"},
+		}, "confirmed observation without matching remote ACK"},
```

## 3. 验证与影响评估
- 该修改精确对应事件文件缺失时的真实验证拒绝项 `"confirmed observation without matching remote ACK event/timing"`。
- 不修改任何生产代码逻辑。
- 不降低测试安全性，不放宽安全边界，不删除该测试用例。
- 经单独 `go test -run=^TestEVX` 验证，10 个顶层测试组全部 PASS。
