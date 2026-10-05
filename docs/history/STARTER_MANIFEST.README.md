# STARTER_MANIFEST.sha256 — 历史文件，请勿用于校验

这是 **原始 Starter 骨架（47 个文件）** 的清单，生成于项目开始阶段
（`2026-09-24`）。它**与当前源码无关**，且因为路径基准是当时的项目根目录，
在当前目录结构下运行校验会大量失败。

```text
$ sha256sum -c MANIFEST.sha256        # 不要这样做
... 25 FAILED
```

它保留在这里仅供追溯：**用来证明本工程是从那个骨架出发改造的**，
而不是用来确认任何一次运行对应的源码。

## 正确的做法

每一次运行都有**自己的**源码清单，位于 `results/raw/` 下：

| 里程碑 | 清单 |
|---|---|
| M2.1 | `results/raw/m2_1_source_manifest.sha256` |
| M3 | `results/raw/m3_source_manifest.sha256` |
| M4 | `results/raw/m4_source_manifest.sha256` |
| M4.1 | `results/raw/m4_1_source_manifest.sha256` |
| M4.2 | `results/raw/m4_2_source_manifest.sha256` |

审查包的根目录 `MANIFEST.sha256` 是**该次交付的唯一有效清单**，
它列出包内全部文件的 SHA-256，可以直接 `sha256sum -c` 校验。

## 为什么不用文件修改时间

哈希只证明文件与清单一致；**文件修改时间不是把日志关联到源码的替代品**。
项目不是 Git 仓库，没有 commit hash 可用，所以每次运行都必须自带源码清单。
