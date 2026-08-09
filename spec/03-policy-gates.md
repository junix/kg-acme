# 03 — 策略门（Policy Gates）

## 原则

capability 在 describe（或 fallback 表）里声明 `side_effects`。
**默认全拒**：每一个声明的副作用都必须由操作者显式开启对应的
`--allow-*` 门，否则调用在执行前以 `policy_denied` 终止，provider
进程根本不会启动。

## 副作用 ↔ 门

| side_effect | 放行 flag | 含义 |
|---|---|---|
| `network` | `--allow-network` | 访问网络（调 LLM、拉远程服务） |
| `data_egress` | `--allow-data-egress` | 本地数据离开本机（发给云端模型） |
| `downloads_models` | `--allow-model-download` | 下载模型权重 |
| `writes_db` | `--allow-db-write` | 写数据库 |

未识别的副作用**同样拒绝**（fail-closed）：provider 声明了 hub 还不
认识的副作用时，没有任何 flag 能放行它，hub 会在错误里点名该副作用。

## `--dry-run`：零副作用的执行计划

`--dry-run` 不做任何执行（provider 不启动、不写文件、不联网），
只渲染执行计划：

```json
{
  "dry_run": true,
  "provider": "kg-extract",
  "provider_path": "/Users/.../kg-extract",
  "probed": false,
  "capability_id": "extract.entities_relations",
  "argv": ["/Users/.../kg-extract", "-o", "kg-protocol", "--file", "doc.md"],
  "side_effects": ["network", "data_egress"],
  "denied": ["network", "data_egress"],
  "would_execute": false
}
```

- `denied` 列出当前门状态下会被拒的副作用；
- `would_execute` = `len(denied) == 0`，即"如果现在真跑会不会过门"。

dry-run 本身永远成功（status ok）——它是只读操作。

## 实现位置

- `internal/policy`：门状态与判定（`Gates.Denied` / `Gates.Check`）；
- `internal/router.Execute`：校验 input → 渲染 argv → dry-run 短路 →
  策略检查 → 执行。策略检查在执行前，denied 时返回
  `policy_denied` envelope，exit 1。
