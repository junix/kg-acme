# 01 — kg.provider/v1 协议契约

hub 与 provider 之间的全部契约。provider 侧并行实现中，hub 按本文消费。
所有协议输出写 **stdout**，日志写 **stderr**。

## 1. 自描述：`<provider> describe --json`

输出一个 manifest（JSON Schema：`internal/schema/provider-v1.schema.json`）：

```json
{
  "protocol": "kg.provider/v1",
  "protocol_versions": [1],
  "provider": {"id": "...", "version": "...", "description": "..."},
  "capabilities": [{
    "capability_id": "extract.entities_relations",
    "title": "...",
    "description": "...",
    "side_effects": ["network", "data_egress", "downloads_models", "writes_db"],
    "input_schema": {"type": "object", "...": "JSON Schema，枚举写在这里"},
    "output": {
      "mode": "result-json | artifact",
      "kind": "kg-document | chunks | communities | json"
    },
    "cli_spec": {
      "subcommand": ["..."],
      "always": ["..."],
      "positionals": [{"name": "file", "required": true}],
      "flags": [{
        "name": "backend",
        "flag": "-b",
        "kind": "string | number | boolean | array",
        "optional": true,
        "default": null,
        "repeatable": false,
        "join": ",",
        "stdout": false,
        "order": 10,
        "negated": false
      }]
    }
  }]
}
```

要点：

- hub 按 `capability_id` 匹配能力，用 provider 的 `cli_spec` **覆盖**
  hub 的 fallback 表；两者不同时发射 diagnostic
  `cli_spec differs from hub data table (provider authoritative; hub data table is fallback)`。
- `cli_spec` 的 argv 发射序：**Always ++ Subcommand ++ Positionals ++
  Flags**（flags 按 `order` 升序，平手按 `flag` 字典序）。
- boolean flag 仅在启用（true）时发射；`negated: true` 相反（false 时发射）。
- array flag：`repeatable: true` 每元素发射一次 `--flag v`；否则用 `join`
  分隔符拼成单个值。
- `side_effects` 是策略门的输入，见 03-policy-gates.md。

## 2. 依赖探测：`<provider> available --json`

```json
{
  "available": true,
  "ready":   [{"name": "ollama", "kind": "service"}],
  "missing": [{"name": "model-x", "kind": "weights"}],
  "cache_dir": "..."
}
```

- 退 0。
- hub 做 **best-effort** 探测：不支持或失败时 `Probed=false`，
  **不降级**（fail-safe）——available 未知不等于不可用。

## 3. 调用：`<provider> invoke <capability_id> --request -`

- stdin：一个 JSON 请求对象（hub 发送 `{"capability_id","input":{...}}`）。
- stdout：**恰好一个** `kg.execution/v1` envelope：

```json
{
  "protocol": "kg.execution/v1",
  "capability_id": "extract.entities_relations",
  "provider": "kg-extract",
  "status": "ok | error",
  "result": {},
  "artifacts": [{"path": "...", "kind": "...", "checksum": "sha256:..."}],
  "diagnostics": [{"severity": "info | warning | error", "message": "..."}],
  "error": {"code": "...", "message": "..."}
}
```

- `output.mode == "result-json"` → 结果内联在 `result`；
  `"artifact"` → 文件列在 `artifacts`（带 checksum）。

## 4. 版本协商

- hub 支持集（当前 `[1]`）与 provider `protocol_versions` 取交集，
  选最高的共同版本。
- **无交集** → 错误码 `unsupported_schema_version`。
- **manifest 不是合法 JSON 或违反 provider-v1 schema** → 错误码
  `malformed_manifest`。
- 两者必须可区分：前者"说得好但听不懂"，后者"根本没说清楚"。

## 5. hub 错误码

| code | 含义 |
|---|---|
| `unsupported_schema_version` | 版本协商无交集 |
| `malformed_manifest` | describe 输出非法 JSON / 违反 schema |
| `capability_not_found` | 没有 provider 声明该 capability_id |
| `provider_not_found` | 指定的 provider id 未发现 |
| `policy_denied` | side-effect 门未开 |
| `invalid_input` | 参数违反 input_schema 或 cli_spec |
| `invocation_failed` | provider 执行失败 / envelope 非法 |
