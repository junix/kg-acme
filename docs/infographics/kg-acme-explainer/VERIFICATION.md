# VERIFICATION — kg-acme 可审计技术长图

本文是验证层（审计层，不受六禁约束）：声明↔锚点对照、门禁判例留痕、
指纹表、偏差披露与敌意复核入口。页面显示层（index.html + svg/）零代码
细节，坐标级信息只进本文。

- 引擎仓：`KG_ACME_ROOT`（只读，除本交付树外未写入任何内容）
- 引擎 HEAD（冻结）：`9cea0c72ec89e67a8347e11ad60f08fb2b16f602`
- 冻结时 tracked 文件数：50（extract 启动即校验，数目不符即拒）
- 证据来源：真二进制（在 /tmp 工作拷贝离线构建）对 4 个 shell 假提供者
  （fake/ver/ugly/bad）实测；零真实网络、零模型下载、零真实外部调用。

## 一、声明↔锚点对照（C01–C30）

「冻结证据」列给出 data/ 文件与字段路径；「源锚点」列给出引擎仓
file:line（由 data/source-anchors.json 冻结，逐条含当行摘录与额外命中）。

| 声明 | 页面主张（摘要） | 冻结证据 | 源锚点 |
|---|---|---|---|
| C01 | 三入口三职责、共享一份快照：kg 执行 / kgctl 管理 / kg-mcp 发布 | engine-contract.help_contract / mcp / refresh | README.md:7 ; README.md:8 ; README.md:9 ; CLAUDE.md:6 |
| C02 | 铁律一：中枢只做集成（发现/协议/目录/策略/路由五件事），算法在 provider | repo-metrics.role_loc（职责分层）；catalog-facts | spec/00-overview.md:12 ; internal/discover/discover.go:94 |
| C03 | 铁律二：参数面来自 provider 自描述；中枢兼容表只是兜底，冲突以 provider 为准 | engine-contract.snapshot.providers（fake 探测通过）；probe_taxonomy | spec/00-overview.md:16 ; internal/bridge/bridge.go:46 |
| C04 | 不可变能力快照由管理面刷新生成，带内容指纹（重建即复算） | engine-contract.snapshot.fingerprint / refresh | README.md:11 ; internal/surface/surface.go:64 |
| C05 | 只读面零启动：10 次只读操作连跑，provider 启动 0 次（日志实测） | engine-contract.readonly_surface.provider_starts = 0 | tests/e2e_test.go:34 ; tests/e2e_test.go:58 |
| C06 | 执行面只复核选中者：恰好 describe → available → invoke 三次（日志实测） | engine-contract.execution_revalidation（3 次，顺序冻结） | tests/e2e_test.go:62 ; tests/e2e_test.go:77 |
| C07 | 目录是稳定命令面，加载期强校验（非法即拒绝启动） | catalog-facts（17 命令全量冻结） | spec/02-catalog-and-routing.md:3 ; internal/catalog/catalog.go:21 |
| C08 | 语义标识逐条镜像命令路径（点分 ↔ 空格分段），实测 17/17 | catalog-facts.semantic_id_mirrors_command_path = true | CLAUDE.md:29 ; internal/catalog/catalog.go:47 |
| C09 | 公共标识去 kg. 前缀；分组命名空间 curated 11 个 | repo-metrics.source_constants.curated_group_namespaces（11 项） | internal/surface/surface.go:176 ; internal/surface/surface.go:209 ; internal/surface/surface.go:236 |
| C10 | provider 发现五级顺序；第五级扫描协议原生前缀可执行文件（实测 3 个原生二进制） | repo-metrics.source_constants.protocol_native_bins（3 项） | spec/02-catalog-and-routing.md:63 ; internal/discover/discover.go:9 |
| C11 | 探测失败两分：版本无交集 / 清单畸形；失败不降级（未知 ≠ 不可用） | engine-contract.probe_taxonomy（ver/ugly/bad 实测错误码冻结） | CLAUDE.md:32 ; internal/protocol/negotiate.go:5 ; spec/01-provider-protocol.md:71 |
| C12 | 路由三键排序确定：探测过的 > 权重降序 > 提供者标识字典序 | engine-contract.route_explain_* | internal/surface/surface.go:125 ; spec/02-catalog-and-routing.md:93 |
| C13 | 兜底命令行渲染序：常驻 + 子命令 + 位置参数 + 旗标（序号升序、平手字典序） | engine-contract.dry_run_net_no_flags.result.argv（渲染结果实测） | spec/01-provider-protocol.md:51 ; internal/bridge/bridge.go:240 ; internal/bridge/bridge.go:245 |
| C14 | 副作用四门默认全拒；四把 --allow-* 旗标开门；拒绝文案点名旗标 | engine-contract.policy_denied（rc=1 + 消息逐字冻结） | spec/03-policy-gates.md:6 ; internal/policy/policy.go:31 |
| C15 | 未知副作用 fail-closed：清单枚举封闭拒收 + 策略默认拒（两道闸） | engine-contract.probe_taxonomy.ugly（清单畸形实测） | internal/schema/provider-v1.schema.json:43 ; internal/policy/policy.go:57 |
| C16 | 干跑渲染完整计划（denied 清单 / would_execute），零副作用且永远成功退出 | engine-contract.dry_run_net_no_flags / dry_run_echo | spec/03-policy-gates.md:37 ; internal/router/router.go:384 |
| C17 | 九个机器错误码固定集合（含两个清单类错误不可混用） | engine-contract.error_envelope / pipeline_bad_edge | internal/protocol/types.go:164 ; internal/protocol/types.go:174 |
| C18 | 输出纪律：标准输出恰好一个信封；失败 rc=1 + 一行人话；盘点越权被拒 | engine-contract.cli_failure_contract（5 例 rc=1 逐例冻结） | CLAUDE.md:34 ; internal/cli/cli.go:89 |
| C19 | 流水线 hub 仍只做编排：每步一次完整路由调用，按拓扑序执行 | engine-contract.pipeline_validate_plan / pipeline_run | spec/05-pipeline.md:4 ; internal/pipeline/run.go:113 |
| C20 | 类型边三层校验（可接线 / 种类一致 / 通道兼容），违规计划期拒绝 | engine-contract.pipeline_bad_edge（违规边实测 rc 冻结） | spec/05-pipeline.md:50 ; internal/pipeline/pipeline.go:338 |
| C21 | 图校验：拓扑排序；成环或引用未知阶段即拒 | engine-contract.pipeline_validate_plan（拓扑序冻结） | spec/05-pipeline.md:70 ; internal/pipeline/pipeline.go:292 |
| C22 | 门预检：收集全部阶段副作用并集一次过门，缺旗标快速失败零启动 | engine-contract.pipeline_dry_run_gates（provider_starts=0） | spec/05-pipeline.md:76 ; internal/pipeline/pipeline.go:241 |
| C23 | 产物先验校验和，复制进工作目录并重算；下游注入工作目录副本（实测注入值冻结） | engine-contract.pipeline_run.injected_downstream_input / work_dir_files | spec/05-pipeline.md:79 ; internal/pipeline/run.go:5 |
| C24 | 断点重跑：校验和一致全部复用（实测 invoke 0 次、复核 2 次） | engine-contract.pipeline_resume | spec/05-pipeline.md:92 ; internal/pipeline/run.go:159 |
| C25 | 发布面工具清单由同一份快照运行时派生（前缀 + 语义标识转下划线，实测 5 工具） | engine-contract.mcp.tool_names / tool_count | spec/06-mcp.md:31 ; cmd/kg-mcp/main.go:65 |
| C26 | 门注入：启动旗标 OR 环境允许表；未知令牌启动即报错；无门调用为结构化错误 | engine-contract.mcp.policy_denied_call（isError + structuredContent 冻结） | spec/06-mcp.md:78 ; internal/policy/policy.go:77 ; internal/policy/policy.go:95 |
| C27 | 帧级契约：未知方法与坏帧固定错误码、连接不断；通知帧静默（实测发 8 回 6 静默 2） | engine-contract.mcp（requests/responses/silent_frames 冻结） | spec/06-mcp.md:81 ; cmd/kg-mcp/main.go:89 |
| C28 | 错误信封契约实测：rc=1、恰好一个对象、消息与机器码逐字冻结 | engine-contract.error_envelope | tests/e2e_test.go:385 ; tests/e2e_test.go:477 |
| C29 | 仓库规模事实：50 个跟踪文件（先枚举逐类再求和）、实现 4,223 行 / 测试 3,394 行 | repo-metrics（by_ext / ext_sum_check / role_loc 全量冻结） | （计算类：git ls-files + 行数统计，无固定行号锚点） |
| C30 | 测试两层：内部单测 + 端到端假提供者对打；冻结运行 172 通过 / 0 失败 / 88.7s | unit-tests（一次性冻结层，含逐包计时） | CLAUDE.md:23 ; tests/e2e_test.go:507 |

## 二、六禁门禁与判例留痕

门禁由 build.py 每次运行时重建禁集（文件基名来自**冻结提交树**
`git ls-tree -r --name-only <frozen>`、逐字行集来自引擎 .go/.md、
标识符集来自 Go 声明与 Capitalized 词；语料钉在 provenance 的
frozen_head 上并排除本交付树自身路径），显示层 = index.html +
svg/*.svg。守护区分两种情况：引擎 HEAD 前进但冻结提交仍在仓内
（引擎演进）→ 只打 NOTE 并继续用冻结语料；冻结提交不可解析
（证据漂移）→ 硬失败。任何构建产物不含活的 HEAD（交付提交后重跑
逐字节不变）。

【2026-09-06 修正】原表述「从引擎仓实时重建禁集（git ls-files）」
后证不实，已修正：交付提交落地后 `git ls-files` 会吞入本树自身文件
（自咬），且正向对照文本会随 HEAD 漂移；已改为钉死冻结提交树。

正向对照（自造六例，必须 6/6 咬住；留痕于 data/display-exemptions.json）：

| 禁令 | 自造违规样例 | 判定 |
|---|---|---|
| ① 基名 | 「看看 <引擎 tracked 文件基名> 这个文件」 | 咬住 |
| ② file:line | 「实现在 <基名>:42 附近」 | 咬住 |
| ③ 逐字摘录 | 引擎规格文件 ≥40 字符整行 | 咬住 |
| ④ 内部标识符 | 「函数 <引擎 Go 声明名> 负责路由」 | 咬住 |
| ⑤ 内部路径 | 「路径 <引擎 tracked 路径>」 | 咬住 |
| ⑥ 生成器/重建命令 | 「重建命令 python3 render.py」 | 咬住 |

实测：6/6 咬住，显示层 0 违规（含自我封闭检查：零 script、零外链、
零事件属性；11 张面板引用齐全【后证不实，已修正：交付版页面以
`<img src="svg/…">` 外链面板文件，既非嵌入也未在本文如实披露；
2026-09-06 精化已改为 11 张面板 SVG 逐字节内联 index.html，自我
封闭检查相应改为断言零 `<img>`、内联 `<svg>` 数 == 11、唯一放行的
URL 字符串是 SVG 命名空间属性】；C01–C30 全覆盖且无多余编号；
字体门禁（CJK ≥ 12px、≥ 90% 文本 ≥ 11px）实测 423 条文本 100%
≥ 11px、CJK < 12px 为 0）。

ban④ 豁免登记（data/display-exemptions.json）：CLI 动词与旗标、协议
三动词、JSON 契约键值、九个错误码、架构标识（kg.error/v1 等）、四类
副作用、生态名（JSON/MCP/CLI/Go/git/shell/stdio）、10 个发布命名空间，
以及冻结数据字符串衍生的 608 个词元（页面插值冻结证据所致；词元走查
只扫 6 份冻结证据 JSON，不含本登记簿与指纹表自身——首次真空跑发现
把自身输出喂回走查会造成 2 字节漂移，已修复并回归）。
ban③ 逐字豁免：无。

## 三、指纹表（machine check）

`data/fingerprints.json` 自身 sha256：

```
c342391305e18939df59bde753a3fca3049cecd29caf0121ae4d06792ef1d2a1
```

逐文件 sha256（`fingerprint.py --check` 逐条核验下表**逐字**出现在本文）：

```
1dd4deab06e95603af16df71a0641a039c77969496c27bb2060147abc01c83d4  data/catalog-facts.json
1adf763d26cf8fb4e568bcc9a723406f1853e28706475a794733988a47278202  data/display-exemptions.json
a50536795637e5cebd74db03b6aa20ed1221de7f0a3d999fd3c51d6380682285  data/engine-contract.json
09a217b5865db4f0c1c817cd8a6160da5281f089fe2ebaca800ccee43de9a6bc  data/provenance.json
535290abd0f90159112c7b9a5de3f3a0d0e6cc657da2b01c272a19902d39c640  data/repo-metrics.json
d7f21e42b8cb733f0b165cb537aa403631f56c7583f4ad02bd21edcafd7573d3  data/source-anchors.json
d7a3d0d32a5afd85aa7386956c602930a78486d586bf6cb3477406176615839c data/unit-tests.json
85f7b4677b2d0cd4c183c8a7417a78fb420f4c8927099b19dfe897e4d438435c  index.html
a225803cbf089bae2ae51e6eee72efe540a2cea071b8750a54e0d3c9ce239e5c  render/full-2x.png
0c9c055d2a0a15be2d6fb7624c63e2329dc8524ff160f8cc52416dd6b65484bb  render/full-gray.png
ba3a78bf15669a27a00b43cb171bcceb5d6115d51c9789d96cfb61dda1b50e9d  render/thumb.png
b3c57c16aed668633ef2c1a44fa94966ffb77dab1df6586f5c8901249ddeca78  svg/p0-hero.svg
68a5482d43cf8b66ef455f6d9d579e3b7a9e9abbc39a4c50eb92918662444524  svg/p1-architecture.svg
0df78a5e0a9db29d28f8f480fa446605e499bb1383a9c917aeae55d2ad1070c1  svg/p10-contract.svg
8a51bb7daa735add0537a71cd54dbe2932116ae56593ead7d1adbbb43d141628  svg/p2-snapshot.svg
985fb58ba48cc50d070503740df0d17ee9010cb1b1f73e859cfdd56c27776ad4  svg/p3-cli.svg
41caca4aa79582aea9d3fc617f0ff86bb65e42de8ffdd470315fe08f52fc0ce0  svg/p4-protocol.svg
1f4ff47663f72ca7318185e9067029e9ffec452378da4d8b47bd0243c0f241c8  svg/p5-policy.svg
7a34d9d414b7766afad8f118a894e9fbcc122829e4dd5fb698a04b408ad134fe  svg/p6-errors.svg
e204ff0c183096d400177a25e98571a15018037b3c4e9c7b3e8db3cda525e18a  svg/p7-pipeline.svg
930bd3dbcfda38b4e3c7d89f5d71cb187b9f882da35cf0315c3f32ec8eb823c3  svg/p8-mcp.svg
2de6a94fe34c9aead532df6426ba68da4c165be4ee153adbbcd60fb606425752  svg/p9-quality.svg
```

（VERIFICATION.md 与 README.md 本身不入表——前者引用本表，后者是
人读说明；render/crops/ 由 full-2x 派生，由渲染断言覆盖；
data/audit/（提交后复核留痕）按定点规则指纹豁免——把运行记录写进
被指纹的文件会迫使再提交、再运行，没有定点。 fingerprints.json 自身
的 sha 亦不入表（自哈希不可能；2026-09-06 修复了重跑时旧表自哈希行
导致的漂移，现在重复运行逐字节稳定）。）

## 四、双跑真空（vacuum.py 实测结果）

判据预先声明（README）：一次性层绝不重建且逐字节不变；确定性层删除后
全链重建逐字节一致；位图三件首选逐字节一致，降级判据为像素零差且剔除非
像素块后字节一致；六禁门禁、svg-linter 门禁、渲染三重断言、指纹 machine
check 全部重过；无残留（零 .pyc、封闭文件普查、/tmp 工作目录清除）。

实测结果：**VACUUM-OK** —— 30 个预声明产物（7 份 JSON 证据 +
display-exemptions + fingerprints + index.html + 11 张 SVG + 位图三件 +
一次性层 2 份）全部复现：文本与位图均逐字节一致（未动用降级判据），
一次性层未被触碰，门禁全过，无残留。

【提交后复跑】交付提交已落地（引擎 HEAD 前进）后，extract / vacuum
的 HEAD 锁会拒绝在移动过的树上重跑——这是设计行为（在移动的 HEAD
上重冻结=证据漂移）。提交后复核按 README「提交后复现」的冻结工作树
配方执行：`git worktree add /tmp/kgacme-frozen 9cea0c72…` 并把
`KG_ACME_ROOT` 指向该工作树。2026-09-06 精化后的廉价门禁（build 六禁 +
字体门禁、svg-linter、指纹核对）已在提交后 HEAD 实测通过，留痕于
`data/audit/post-commit.md`；extract/vacuum 全链复跑由主会话在最终
提交后按同一配方执行并补记。

## 五、偏差披露（规格 vs 实测）

以下为实测与规格文档的已登记偏差，页面按实测表述：

1. **命令行顶层错误信封的机器码折叠**：规格给出策略拒绝等机器码；
   实测顶层信封统一折叠为通用错误码 `error`，细节保留在人话消息
   （engine-contract.error_envelope / policy_denied）。阶段级错误保留
   原样机器码（invocation_failed 实测）。
2. **流水线整体失败退出码**：某阶段 status=error 时整链命令实测
   退出码为 0（信封内呈错），非非零退出。
3. **发布面结构化错误码同样折叠**：策略拒绝的 structuredContent 里
   机器码为 `error` 而非规格的策略拒绝码。
4. **能力工具无 provider 键**：发布面向能力工具注入 4 个 allow_* 门键
   与 dry_run，但不注入 provider 选择键（实测 5 工具模式逐键冻结）。
5. **规格中的目录示例表已过期**：规格 02 的示例目录含 dedup/communities
   等别名；实测内置目录为 17 条命令、10 命名空间、镜像规则全过
   （catalog-facts 全量冻结为准）。
6. **未知副作用的闸位**：规格将未知副作用写在策略门一节；实测其在
   提供者清单校验层即被拒（副作用枚举封闭，probe_taxonomy.ugly 落
   清单畸形），策略层另设默认拒分支——两道闸，页面如实双述。

## 六、敌意复核入口

```sh
TREE=<本交付树绝对路径>
ENG=<引擎仓绝对路径>          # HEAD 必须是冻结 commit

# 门禁自检（缺引擎根必须 FATAL，退出码 1）
cd /tmp && cp "$TREE/tools/build.py" . && \
  env -u KG_ACME_ROOT IG_OUT="$TREE" python3 build.py ; echo $?

# 指纹 machine check（任何漂移/未逐字登记即退出码 1）
cd /tmp && cp "$TREE/tools/fingerprint.py" . && \
  IG_OUT="$TREE" python3 fingerprint.py --check

# 双跑真空（删可再生产物 → 全链重建 → 逐字节比对）
cd /tmp && cp "$TREE/tools/vacuum.py" . && \
  KG_ACME_ROOT="$ENG" IG_OUT="$TREE" IG_WORK=/tmp/kgacme-ig-run \
  python3 vacuum.py
```

## 2026-09-06 refine

本节记录 2026-09-06 精化（按全舰队巡检结论修缺陷）。改动**未提交**，
由主会话在精化批次完成后统一提交；引擎仓其余内容未被触碰。

### 修复（按巡检缺陷类）

1. **img-external-panel（高）**：交付版 index.html 以 11 个
   `<img src="svg/*.svg">` 外链面板。已改 build.py 把每张面板 SVG
   逐字节内联进页面（`svg/` 文件保留为重建产物与独立审阅入口，
   字节与内联一致）；自我封闭断言升级为「零 `<img>`、内联 `<svg>` 数
   == 11、唯一放行 URL 字符串是 SVG 命名空间属性」。
2. **doc-drift（高）**：README 曾宣称「零外部请求，SVG 分层面板嵌入」
   ——外链版既非嵌入也非自包含，过度宣称比缺陷更糟。内联后该句成真，
   并按「撤回而非抹除」加注【2026-09-06 修正】；README 位图尺寸
   （9886→9979 CSS px / 19772→19958）与交付树说明同步重写。
3. **cjk-small（高）+ svg-text-small（中）**：panels.py 全面提字阶
   ——CJK 一律 ≥ 12px（原最小 9.5，94 处越界），全部文本 ≥ 11px
   （原 74.8% ≥ 11px）。放不下处拆行/拆层而非缩字：p6 错误码改双层
   行芯片（中文 12 + 机器码 mono 11）、p4 探测分类改四行卡、p3/p5/p8
   长句拆行；p4/p6 面板各增高 40/60px；p7 声明芯片右移避免出画布。
   build.py 新增字体门禁（CJK ≥ 12px 且 ≥ 90% 文本 ≥ 11px，实测
   423 条 100% ≥ 11px、CJK 越界 0）。位图三件与 15 裁片已重渲。
4. **gate-selfbite（中）**：交付版 extract/vacuum 锁「HEAD == 冻结
   提交 ∧ tracked == 50」，交付提交落地即双双 FATAL；build 六禁语料
   取活 `git ls-files`（吞入本树 46 个文件、正向对照随 HEAD 漂移）；
   README 无复现配方、无提交后运行记录。已修：六禁语料钉在冻结提交树
   （`git ls-tree`/`git show`，双保险排除本树路径）；守护两分——引擎
   演进（NOTE+配方指针，门禁照跑）vs 证据漂移（硬失败，临时仓实测
   rc=1）；任何产物不含活 HEAD（build 双跑逐字节一致）；README 新增
   「提交后复现（冻结工作树配方）」；提交后运行记录放指纹豁免的
   `data/audit/post-commit.md`（fingerprint 普查与 vacuum 普查同步
   放行该目录）。
5. **fingerprint 自咬（精化中发现）**：原 fingerprint.py 重跑时会把
   旧 fingerprints.json 当普通 data/*.json 哈希进表（带旧自哈希行），
   重跑即漂移、非双跑稳定。已修：自哈希不可能的文件不再入表，重跑
   逐字节稳定（实测双跑一致）。

### 缓引（不实施，按精化权限）

- no-hero / hero-not-subject（中）：11 张同权重卡片、无 ≥ 2.5× 主角
  面板——超出本次最小修复范围。
- no-claims-binding（中）：C01–C30 仅页脚范围指针，无逐声明页面绑定。
- no-sidenote-track（中）：无 680/40/288 侧注轨。
- no-poison / no-reverse-sweep / no-vacuum / form-mismatch /
  palette-mismatch / no-negative-facts：巡检未列或按类缓引；
  contract.md 缺失（低）同缓引。

### 门禁复跑（改动后、提交前，全在树内廉价档）

| 门禁 | 结果 |
|---|---|
| panels → build → render 全链重建 | PASS（面板 11 张；页面 1200×9979 CSS；13 片滚动回读全中；位图 2400×19958；15 裁片） |
| build.py 六禁 + 正向对照 | PASS：6/6 咬住、显示层 0 违规、引擎演进 NOTE 后照常完成、双跑逐字节一致 |
| build.py 字体门禁 | PASS：100% ≥ 11px（423 条），CJK < 12px = 0 |
| svg-linter 逐张 | PASS：11 张 rc=0 ∧ 0 findings |
| fingerprint.py + --check | PASS：22 文件零漂移，全部 sha 逐字在本文 |
| 提交后守护实测 | 缺 KG_ACME_ROOT → rc=1；冻结提交不可解析 → rc=1（证据漂移）；活仓（HEAD 已前进）→ NOTE + 全过，记录在 data/audit/post-commit.md |

extract / vacuum 全链提交后复跑（需冻结工作树 + ~90s 离线 Go 构建）
由主会话在最终提交后按 README 配方执行。

## 2026-09-09 reader-pass（hero 测试计数下架）

依据 create-explainer §4.5 读者面规则（fleet reader-pass Wave 2d-2）。本批
命中一处：hero 区可见的测试计数普查数字（metastrip「测试 172 通过 / 0 失败
（一次冻结运行）」与 p0-hero 面板瓦片「172 个测试全绿（冻结运行）」）。
实跑数字仍由 p9 工程事实面板（C30：「冻结运行 172 通过 / 0 失败 / 88.7s」
及其测试网砖）与 data/unit-tests.json 一次性冻结层承载，绑定强度不降。

### 改动清单（tools/build.py / tools/panels.py）

1. `build.py` metastrip 删「测试 N 通过 / M 失败（一次冻结运行）」一项
   （5 → 4 项：引擎提交、冻结证据、面板声明、跟踪文件——后三项非本批命中）；
   随删唯一使用处后 `UT = load("unit-tests.json")` 一并移除。
2. `panels.py` p0-hero mets 瓦片带删「172 个测试全绿（冻结运行）」砖
   （5 → 4 砖），瓦宽由固定 189 改按 4 枚均分原跨距（(1025−3×20)//4=241）；
   p0 面板高 640 不变。
3. 页面「172」可见残留仅剩 p9 工程事实面板 2 处（C30 认领的证据语境）；
   hero 与导读零测试计数残留。

### 门禁复跑（旧 → 新；/tmp 平面拷贝全链）

| 门禁 | 旧 | 新 |
|---|---|---|
| build.py 六禁门禁 | 0 violations · controls 6/6 | 同左；字号线 421 runs 100% ≥11px、CJK<12px 0 ✓ |
| svg-linter ×11 | 11 × (rc=0 ∧ 0 findings) | 同左（仅 svg/p0-hero.svg 变，其余 10 面板逐字节不变）✓ |
| render.py 三重断言 | 2400×19958（1200×9979 × dpr2） | 同左（页高不变；hero 区 crops 00-el0/02-s0 更新）✓ |
| 重建确定性（双跑） | — | 第二份 /tmp 拷贝 panels+build 重跑，index.html 与 p0-hero.svg 逐字节一致 ✓ |
| 指纹 machine check | 22 文件全符 · 逐字在本文 | 22 文件全符（§三 六行新 sha 随批更新，含 fingerprints.json 自身 sha）✓ |
| 冻结层 | — | unit-tests/provenance/catalog-facts/engine-contract/repo-metrics/source-anchors 六文件 sha256 逐份与 §三 旧值一致 ✓ |

### 产物指纹

§三 表已按新产物更新（index.html、svg/p0-hero.svg、render 三件、
fingerprints.json 自身 sha）；render/crops 仅 hero 区两片（00-el0、02-s0）
随内容更新，其余 crops 派生自不变区段、逐字节不变（按指纹豁免规则不入表，
由渲染断言覆盖）。
