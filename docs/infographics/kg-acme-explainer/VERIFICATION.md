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

门禁由 build.py 每次运行时从引擎仓**实时**重建禁集（文件基名来自
`git ls-files`、逐字行集来自引擎 .go/.md、标识符集来自 Go 声明与
Capitalized 词），显示层 = index.html + svg/*.svg。

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
零事件属性；11 张面板引用齐全；C01–C30 全覆盖且无多余编号）。

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
357bffdd8cf8270f931539c33ed9feb0d7c1e2371de9e248e69564aa2e2f9eb1
```

逐文件 sha256（`fingerprint.py --check` 逐条核验下表**逐字**出现在本文）：

```
1dd4deab06e95603af16df71a0641a039c77969496c27bb2060147abc01c83d4  data/catalog-facts.json
bd46cde68607ce55d74f13f2fe691c605f34ccc501e520b86cc6261fd1384855  data/display-exemptions.json
a50536795637e5cebd74db03b6aa20ed1221de7f0a3d999fd3c51d6380682285  data/engine-contract.json
09a217b5865db4f0c1c817cd8a6160da5281f089fe2ebaca800ccee43de9a6bc  data/provenance.json
535290abd0f90159112c7b9a5de3f3a0d0e6cc657da2b01c272a19902d39c640  data/repo-metrics.json
d7f21e42b8cb733f0b165cb537aa403631f56c7583f4ad02bd21edcafd7573d3  data/source-anchors.json
d7a3d0d32a5afd85aa7386956c602930a78486d586bf6cb3477406176615839c data/unit-tests.json
4f9c783190223ba898c5055acc46de338b18ea330931f40e3fa37a4c2dd95827  index.html
219c26dc0e327cc917c4b4e97cb882c3e85723c730b79f762207961fb7f2c05b  render/full-2x.png
21a89348835ec5b19d7cc57af57cb59eee91024aa769154a3892f08cd696eaee  render/full-gray.png
6e8961c72e4fd908b2e26aea45836b4536f3f7d11afd21cc869031d43a8f3ace  render/thumb.png
6a6282eac57d0e90bb55d7dfaf43776eaceab09239a220fc430b47d296134a55  svg/p0-hero.svg
4e9b269dac70f47ba5f5ec073be4247c98c37e3505bff0d7e6561eeadf636eb9  svg/p1-architecture.svg
f6f17e7864f825f1a6b8cfc5ad7b998f8fcbf97ee45be4126e827b43c48d0e0b  svg/p10-contract.svg
fab7b8554c6b6066ee49f471fbbd351de9dd2504d5108f79dbdd1d0670875af6  svg/p2-snapshot.svg
5e91b259ca63c533d9724182f69bd31deb5b97cc534990ca58a6aa5da799f8ad  svg/p3-cli.svg
669cd71a0a4f0fe71a347b28a5f40baefea7da7ec82580061f7a52b739b9fbda  svg/p4-protocol.svg
346207f3f8d22bb1aac7965a1e40438cb3007d20fa6c6119e5a5524d416d07dc  svg/p5-policy.svg
269f66c82fed8853a270294bfc732f8ce5d764b7347c54052c60c59382b7946f  svg/p6-errors.svg
917aa0b8b9ce9583994d77b3da6df3634b5e9d1607dd38f2c5af30dbf3646ac8  svg/p7-pipeline.svg
886894cd30bdbcab07de981ead84b11e8d7c32ce450e58a86cc7fc308e6b3df0  svg/p8-mcp.svg
52fd11dd4b235ff9469ab45a69c1424eb7d024e7820bd32c90428f312d849fcf  svg/p9-quality.svg
```

（VERIFICATION.md 与 README.md 本身不入表——前者引用本表，后者是
人读说明；render/crops/ 由 full-2x 派生，由渲染断言覆盖。）

## 四、双跑真空（vacuum.py 实测结果）

判据预先声明（README）：一次性层绝不重建且逐字节不变；确定性层删除后
全链重建逐字节一致；位图三件首选逐字节一致，降级判据为像素零差且剔除非
像素块后字节一致；六禁门禁、svg-linter 门禁、渲染三重断言、指纹 machine
check 全部重过；无残留（零 .pyc、封闭文件普查、/tmp 工作目录清除）。

实测结果：**VACUUM-OK** —— 30 个预声明产物（7 份 JSON 证据 +
display-exemptions + fingerprints + index.html + 11 张 SVG + 位图三件 +
一次性层 2 份）全部复现：文本与位图均逐字节一致（未动用降级判据），
一次性层未被触碰，门禁全过，无残留。

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
