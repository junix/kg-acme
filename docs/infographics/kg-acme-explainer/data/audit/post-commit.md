# 提交后复核留痕（指纹豁免层）

本文件按定点规则指纹豁免（`fingerprint.py` 跳过 `data/audit/`，
`vacuum.py` 封闭普查允许 `data/audit/*.md`）：把运行记录写进被指纹
的文件会迫使再提交、再运行，没有定点。此处的 HEAD 是**活的**提交后
HEAD，仅用于留痕，不进任何构建产物。

## 2026-09-06 精化后廉价门禁（交付提交 1788ad6 之上）

引擎仓 HEAD：`1788ad69584c903f2efcf469c388dd136f8beb97`（> 冻结
`9cea0c72`，冻结提交仍在仓内 → 引擎演进，非证据漂移）。
树状态：精化改动未提交（由主会话统一提交）。

| 门禁 | 结果 |
|---|---|
| build.py 六禁门禁（KG_ACME_ROOT=当前仓） | PASS：6/6 正向对照咬住、0 违规、引擎演进 NOTE（stderr）后照常完成；双跑 index.html 与 display-exemptions.json 逐字节一致 |
| build.py 字体门禁 | PASS：423 条文本 100% ≥ 11px，CJK < 12px 为 0 |
| 守护分支 | 缺 KG_ACME_ROOT → rc=1；冻结提交不可解析（临时仓实测）→ rc=1「evidence drifted」 |
| svg-linter（逐张 --plain） | PASS：11 张 rc=0、0 findings |
| render.py（CDP 切片 + 高度等式） | PASS：1200×9979 CSS，full-2x 2400×19958，15 裁片 |
| fingerprint.py --check | PASS：22 文件零漂移，全部 sha 逐字在 VERIFICATION.md |

待主会话最终提交后补做（需 ~90s Go 离线构建，按 README「提交后复现」
冻结工作树配方）：`extract.py`（一次性层已存在，预期拒绝覆盖）、
`vacuum.py` 全链（预期 VACUUM-OK）。
