# 00 — kg-acme 总览

kg-acme 是 acme 家族（audio-acme / image-acme / video-acme / office-acme）
**capability hub** 模式在知识图谱工具集上的落地：一个 `kg` CLI 作为 KG
工具链的统一入口，把分散的 provider CLI（kg-extract、kg-mm、ygr、以及
任何实现了 `kg.provider/v1` 协议的 `kg-provider-*` 二进制）组织成一张
可发现、可路由、可治理的能力表。

## 两条铁律

1. **hub 只做集成，绝不自实现 KG 算法。** hub 的职责恰好是五件事：
   发现（discovery）、协议（protocol）、能力目录（catalog）、
   策略（policy）、路由（routing）。抽取、去重、社区检测、问答、存储
   等一切算法都属于 provider。
2. **hub 不写死 provider 的选项/枚举。** provider 通过
   `<provider> describe --json` 自描述（`cli_spec` + `input_schema`，
   枚举在 input_schema 里），hub 照此渲染 argv、校验参数、呈现帮助。
   hub 内置的兼容桥 argv 表（`internal/bridge`）只是 **fallback**：
   provider 自描述与其冲突时以 provider 为准，并发射 diagnostic
   `cli_spec differs from hub data table (provider authoritative; hub data table is fallback)`。

## 架构

```
                 ┌──────────────────────────── kg (hub) ───────────────────────────┐
                 │                                                                  │
  kg list ──────►│ catalog (stable commands)                                        │
  kg describe ──►│ discover ── probe: describe --json / available --json            │
  kg extract ───►│ router ──── resolve capability_id ──► provider (probed|bridge)   │
  kg ask ───────►│ policy ──── side-effect gates (default deny, --allow-*)          │
  kg provider ──►│ bridge ──── fallback argv tables (kg-extract / kg-mm / ygr)      │
                 │ schema ──── JSON Schema validation (describe/available/input)    │
                 └───────┬───────────────────────────────────┬─────────────────────┘
                         │ kg.provider/v1                    │ fallback argv
                         ▼                                   ▼
              kg-provider-* (protocol-native)     kg-extract · kg-mm · ygr (legacy)
```

## 数据流（一次能力调用）

1. `kg extract --file doc.md --allow-network --json`
2. catalog：`extract` → `capability_id = extract.entities_relations`
3. discovery：按顺序找到 provider 可执行文件，best-effort probe
4. router：匹配声明了 `extract.entities_relations` 的 provider（probed 优先于 fallback）
5. parse：hub 标志剥离后，按 provider 的 `cli_spec` 把 argv 解析成 input
6. schema：用 capability 的 `input_schema` 校验 input
7. policy：capability 声明的 `side_effects` 全被拒则报 `policy_denied`
8. execute：probed provider 走 `invoke <cap> --request -`（stdin/stdout
   envelope）；legacy CLI 走 `renderArgv` 渲染 argv 执行并把输出包成 envelope
9. `--json` 时 stdout 恰好输出一个 `kg.execution/v1` envelope；日志永远走 stderr

## 目录结构

```
cmd/kg/            CLI 入口（命令面）
internal/
  protocol/        kg.provider/v1 与 kg.execution/v1 类型 + 版本协商
  catalog/         内嵌 catalog.json：稳定命令声明与校验
  discover/        findExecutable 顺序 + describe/available probe
  router/          capability → provider 解析、argv→input、执行
  policy/          side-effect 策略门
  pipeline/        kg.pipeline/v1 定义、DAG 计划/校验、执行与 resume（spec/05）
  bridge/          兼容桥 fallback argv 表 + renderArgv
  schema/          内嵌 JSON Schema + santhosh-tekuri/jsonschema 校验
spec/              设计文档（本目录）
tests/             端到端测试（假 provider shell 脚本）
```
