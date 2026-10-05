# GitHub 当前源码上传说明

本次上传是当前工作区的源码快照，软件版本 interactive-v1.5-minimalui1，外观修订 reference-ui-20261005。
475 项源码清单 SHA-256：`5330ac977d60422492215d3b5fed6d83162283b067ef33741d88a2edb68fde28`。CURRENT_SOURCE_MANIFEST.sha256 是本次源码清单，不是此前遗留清单。

包含完整 cmd、internal（含内嵌前端）、configs 无秘密模板、scripts、docs、测试、Go 依赖文件，以及历史 results 非 raw 报告和 deliverables 来源记录。GitHub 上传附加文件不属于上述 475 项冻结源码。

当前前端说明和验证摘要见 [delivery/reference-ui-20261005/IMPLEMENTATION_AND_VERIFICATION.md](delivery/reference-ui-20261005/IMPLEMENTATION_AND_VERIFICATION.md)。该目录明确本轮实际执行范围；原历史文档保留原字节、版本与局限。

不上传 runtime、真实私钥、会话秘密、访问令牌、缓存、EXE、历史 ZIP 或数万份实验 raw。历史报告中的本地绝对路径只是执行来源，不代表 GitHub 随附了这些原始数据。完整当前审查包和软件包仍保留于本地交付目录；此仓库不是独立的全部历史实验资料归档。

构建/运行说明见项目 README、docs/user/MINIMALUI_MANUAL.md 和 scripts/backend_build.py。用户运行包无需 Go/Python/Node，源码构建需要开发工具。现有 .github/workflows/race.yml 保留历史草案；本次提交使用 [skip ci]，未安排远端执行，不能将上传本身当作新的 CI/race 通过证据。

上传仅复制文件，不改原工作区代码，也不初始化或重置原工作区 Git。
