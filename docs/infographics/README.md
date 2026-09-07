# kg-acme 信息图目录

仓库里已经有**两套论点不同的图**。不要合并，也不要在本目录再画一张分层架构信息图。

本目录只收现行长图 `kg-acme-explainer/`。旧分层图住在上一级 `docs/architecture-infographic.*`，本波原样保留、不重画。本目录没有第三张图。

## 分工

| 图 | 路径 | 论点 | 本波 |
| --- | --- | --- | --- |
| 旧分层图 | `docs/architecture-infographic.*`（tex / png / svg / pdf） | kg-acme: Knowledge-Graph Capability Hub 架构总览。代码标识符上台。 | 旧图。保留。不重画、不编译、不改样式。 |
| 现行长图 | `docs/infographics/kg-acme-explainer/` | 中枢只做集成：发现、协议、目录、策略、路由五件事；图谱算法全部住在 provider 里。三条入口共享一份不可变能力快照。 | 现行交付。证据冻结。页面零代码坐标。细节以该目录 `README.md` 为准。 |

两套图回答的问题不同，合成一张会同时丢掉两边的论点：

- 旧分层图回答：能力中枢模块怎么分层。
- 现行长图回答：五职责内核、不可变快照、副作用四门与纯编排流水线怎样接上。

## 先看哪一张

先看 `kg-acme-explainer/`。它是现行交付：数字来自冻结证据，页面本身不带代码坐标。入口是该目录的 `index.html` 与 `README.md`。

只有需要静态分层总览、且能接受代码标识符上台时，才打开旧图。旧图把标识符画上台不是开工重画的许可；要修，另开任务，且须先从 `architecture-infographic.tex` 复现已提交产物再改同一份源。

`docs/architecture.html` 是同主题的交互概览页，不是本目录的第三张信息图；本波不改。

## 禁止

- 禁止再做第三张「架构分层信息图」。
- 禁止用重画分层蛋糕的方式「修复」旧 TikZ 或 `architecture.html`。
- 禁止把 `kg-acme-explainer` 扩成第二份分层总览，或把旧图的分层面板抄进现行长图。
- 禁止把两套图合并成一张「更完整」的架构图。
