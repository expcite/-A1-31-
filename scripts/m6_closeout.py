"""Write final milestone, readiness and delivery-source freeze without changing M5."""
import hashlib,json
from pathlib import Path
import source_manifest
ROOT=Path(__file__).resolve().parents[1];RAW=ROOT/'results/raw/m6_20261001'
def read(p):return json.loads(p.read_text(encoding='utf-8'))
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def write(p,x):p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
def main():
 c=read(RAW/'comparison_v2/comparison.json');summary=read(RAW/'regression/test_summary.json')
 artifacts=[dict(path=p.relative_to(ROOT).as_posix(),bytes=p.stat().st_size,sha256=sha(p)) for p in (ROOT/'deliverables/M6').iterdir() if p.suffix in ['.pdf','.pptx']]
 assert len(artifacts)==2
 visual=dict(pptx=dict(path='deliverables/M6/EdgeHybrid_Defense_v2.pptx',slides=12,charts=4,tables=3,all_slides_rendered_and_visually_inspected=True,native_office_execution=False),pdf=dict(path='deliverables/M6/EdgeHybrid_Final_Design.pdf',pages=4,all_pages_rendered_and_visually_inspected=True),word=dict(generated=True,render_verified=False,reason='LibreOffice unavailable; Word.Application absent; unverified draft excluded from delivery',draft_sha256=sha(ROOT.parent/'EdgeHybrid_M6_Unverified_Drafts/EdgeHybrid_Final_Design.docx')))
 write(RAW/'materials/visual_review.json',visual)
 info='''# EdgeHybrid M6 审查包说明

本包为当前完整源码、测试、脚本与公开证据。M6到此停止。生产Go、旧M5数据和原Windows用户Demo未改；本包不含Demo可执行文件，也没有重新发布用户包。

首读 docs/FINAL_DESIGN_REPORT.md、docs/INNOVATION_REPORT.md、docs/USER_GUIDE.md、docs/REPRODUCE.md、docs/DEFENSE_QA.md。需求对应表 docs/FINAL_DELIVERY_INDEX.json 与 REQUIREMENT_EVIDENCE_MAP.md。正式设计说明书为 deliverables/M6/EdgeHybrid_Final_Design.pdf，答辩为 EdgeHybrid_Defense_v2.pptx（12页、可编辑图表/表格）。Word生成但渲染失败，草稿保留包外，不当作已验证材料交付。

M6冻结计划 docs/INNOVATION_PLAN.md；raw results/raw/m6_20261001。comparison_v2是完整三轮对照、三个隔离缺陷A/B和三条复现，保留实际命令/Go JSON/退出码。A/B共同覆盖16/16、缺陷检出3/3，未显示覆盖或检出优势。B改善统一诊断与公共证据，日志更大，未测人工效率。compare_v1及开发提取失败、构建和渲染失败仍保存，不能宣称所有历史命令全部通过。

M5来源在 results/raw/m5_formal_20261001/data/results_index.json 与 report_verified。原采样source hash fad0abe7326003b0b1b1ce51f0699acdb3b7f5bcf6bbc9f636026ada2cf996fd，原交付11c49ff72ec758a5a2d6c2f1d2d46b0c88894ed00bbaa441785e5b1922b0227d，均不替换为M6版本。source_resolution.json为M5采样/交付与M6测量每条源码提供实际hash验证归档。最终当前源码清单另见M6 delivery_source_manifest.sha256和delivery_source_info.json。

regression存全量Go JSON/真实退出码、严格汇总、go mod verify/vet、四组原Python自测、新证据拒绝自测，以及16批M5实际完整验收重算。本轮未重采1000次、未新跑Demo。M5原文件hash逐一未变，Demo zip原hash 96bc6b44d21b2a44e25f13f7f181845934d2173373687bf4f56cf25d187226ac。

0ns观测保留；1000/1000是有限观测。CPU/峰值为100握手进程批次，缺测null+原因。P与T/H的整体差异不全归因ML-KEM。race、独立SM2交换pending，OpenSSL签名SKIP，Linux资源NOT_TESTED。没有独立密码安全证明、标准合规认证或新增跨机/低资源硬件验证。

包内MANIFEST.sha256覆盖自身外全部文件。实际最终ZIP校验、解压重建与16批复验结果在包外 EdgeHybrid_M6_Review_Package.verification.json 和同名 verification_logs；它们标明是重建验收程序，不是原采样二进制。包内已有原采样/完整回归和重算验证结果。不含运行秘密、缓存、exe/dll或历史嵌套ZIP。

依赖及许可见 go.mod/go.sum、docs/DEPENDENCIES.md、THIRD_PARTY_NOTICES.md。自定义协议与本项目未获整体安全认证；项目对外开源许可未由权利人指定。
'''
 (ROOT/'PACKAGE_INFO.md').write_text(info,encoding='utf-8')
 readme=ROOT/'README.md';old=readme.read_text(encoding='utf-8')
 if not old.startswith('<!-- M6 current index -->'):
  readme.write_text('<!-- M6 current index -->\n# 当前交付状态 M6\n\nM5正式实验与M6测试改进/最终材料已完成。最新事实以 [PACKAGE_INFO.md](PACKAGE_INFO.md)、[最终设计说明](docs/FINAL_DESIGN_REPORT.md)、[M6报告](docs/INNOVATION_REPORT.md) 和 results/m6_readiness.json 为准。下文保留原阶段说明，含历史未完成状态，不能据此覆盖最新证据。Demo与M5原始文件未修改，race与独立SM2交换仍pending。\n\n---\n\n'+old,encoding='utf-8')
 manifest=''.join(f'{sha(Path(p))}  {Path(p).relative_to(ROOT).as_posix()}\n' for p in source_manifest.collect(str(ROOT)))
 (RAW/'delivery_source_manifest.sha256').write_text(manifest,encoding='utf-8',newline='\n');source=hashlib.sha256(manifest.encode()).hexdigest()
 write(RAW/'delivery_source_info.json',dict(run_id=c['run_id']+'/delivery',source_manifest_sha256=source,files=len(manifest.splitlines()),measurement_source_manifest_sha256=c['measurement_source_manifest_sha256'],measurement_resolution='source_resolution.json'))
 base=read(RAW/'freeze/baseline.json');A_lines=sum(len((ROOT/b['path']).read_text(encoding='utf-8').splitlines()) for b in base);B_lines=len((ROOT/'internal/handshake/m6_structured_test.go').read_text(encoding='utf-8').splitlines())
 maintenance=dict(A_five_test_file_lines=A_lines,B_generator_test_file_lines=B_lines,scope='actual text line counts only; A has extra assertions and B needs separate scripts, not maintenance efficiency or human hours')
 write(RAW/'materials/maintenance_scope.json',maintenance)
 ready=dict(stage='M6',status='COMPLETE',run_id=c['run_id'],measurement_source_manifest_sha256=c['measurement_source_manifest_sha256'],delivery_source_manifest_sha256=source,normal_comparison='PASS',common_rule_coverage=dict(A='16/16',B='16/16'),known_defects=dict(A_detected=3,B_detected=3,missed=0,unreachable=0,scope='3 isolated test-only variants; finite detection, not proof'),reproduction='3/3 same semantics',regression=summary,original_production_go_unchanged=True,m5_original_files_unchanged=True,m5_revalidated_batches=16,demo_zip_unchanged=True,artifacts=artifacts,visual_review=visual,historical_gaps=dict(race='pending',independent_SM2_exchange='pending',OpenSSL_signature='SKIP',Linux_resources='NOT_TESTED'),current_M5_sampling_repeated=False,Demo_republished=False,external_delivery_verification='../EdgeHybrid_M6_Review_Package.verification.json',stop_after_M6=True)
 write(ROOT/'results/m6_readiness.json',ready)
 milestone=f'''# M6 工作记录

目标：执行冻结的真实握手/记录结构化篡改 A/B 对照并完成最终交付材料。已实际读取AGENTS、TASKS M6、REQUIREMENTS、FORMAL_EXPERIMENT_PLAN、M5 milestone/readiness和本轮REVIEW/AGENT_NEXT_M6/M6_ACCEPTANCE；审查副本见freeze/audit。

修改文件：新增 internal/handshake/m6_structured_test.go；scripts/m6_*.py、m6_deck.mjs；docs的INNOVATION_PLAN/REPORT、FINAL_DESIGN_REPORT、USER_GUIDE、REPRODUCE、DEFENSE_QA、FINAL_DELIVERY_INDEX、REQUIREMENT_EVIDENCE_MAP。更新当前README索引和PACKAGE_INFO，results新增M6独立目录/清单/readiness和实际PPT/PDF。原五个基线测试文件、所有非_test.go生产文件、FORMAL计划及M5旧raw逐字不变。go.mod/go.sum与依赖不改。

run_id {c['run_id']}；测量源码 {c['measurement_source_manifest_sha256']}；最终交付源码 {source}。全部旧测量文件在source_resolution.json按hash归档，新增M6来源不会追填M5。compare脚本采样后只补可选新run_id参数和同次已通过的SM2认证绑定索引；旧版本归档，不改旧原始结果。

A完整28个原顶层测试、60叶用例，B39叶用例。共同规则均16/16；三个缺陷A/B各检出3/3，漏检/不可达均0。D1/D3后续防线仍拒绝，记录阶段退化；D2非法获得Channel但业务未知null。三条独立复现一致。B统一双端阶段、用例规则、公共帧摘要和来源，日志约112KB对A约92KB。有限成本与覆盖不能推导安全或维护效率提升。实际文本行计数A={A_lines}，B测试文件={B_lines}，B还需脚本且A断言更多，不能换算工时。详见comparison_v2/comparison.json及INNOVATION_REPORT。

命令与真实退出码：structured_initial=0；compare、compare_r2=1（开发语法/缩进）；compare_r3=1（Go长行分块提取失败，首轮子B真实系统码未及时保存为未知）；compare_final=0。最终六个正常命令和复现=0，六个缺陷测试=1且目标M6REACH+断言实际失败。全量Go JSON=0，严格汇总=0（353顶层PASS、340子PASS、1SKIP，23包17通过6无测试）；mod verify/vet=0。四个原Python验收自测=0，新5个坏M6证据副本拒绝自测=0（不是协议攻击）。M5重算和16个实际验收命令全0。命令、stdout/stderr、timeout及exit见commands/regression。

材料：六项MD/JSON最终说明/手册/问答/需求索引、真实12页PPTX（4原生图表3表格）、4页PDF已生成逐页渲染检查。首deck两次失败是Excel数字精度/子进程runtime环境；第三次通过。字体/图表轴修订后的首次交付因旧receipt同名失败，最终deck_delivery=0。Word生成0，render=1，缺LibreOffice且无Word注册；未验证草稿保留包外，不交付未检查的DOCX。PDF生成/渲染=0，不冒称PowerPoint桌面或跨软件编辑验证。

风险与未验证：公共hash是中继收到/计划转发帧，不证明对端完整收到或内部transcript；业务未观测null。0ns原样、成功率分母和批次资源限制随所有性能材料保留。race/独立SM2交换pending，OpenSSL签名SKIP，Linux未测；不推断全面后量子认证、抗DoS、侧信道或低资源硬件效果。原Demo本轮未重跑/重发布。源码包无exe、运行秘密、缓存和旧ZIP。

交付验收：最终新增材料完成后再保存final_go_test_json及严格汇总，检查M5/Demo/原生产源码hash、源清单、ZIP全MANIFEST，解压副本重建验收程序、16批原M5复验与B正常执行。最终ZIP hash/大小和真实外部结果见相邻verification.json及日志，避免自引用改写包。下一阶段：按用户指令完成M6后停止，不自行开展后续功能或验证。
'''
 (ROOT/'results/milestone_M6.md').write_text(milestone,encoding='utf-8')
 print('M6 final source and materials frozen:',source)
if __name__=='__main__':main()
