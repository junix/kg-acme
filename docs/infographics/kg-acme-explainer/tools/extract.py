#!/usr/bin/env python3
"""Evidence freezer for the kg-acme explainer infographic.

Reads the engine repo strictly read-only, materializes a fixed /tmp work
copy (IG_WORK) via `git archive`, builds the three real binaries there,
drives them against self-made fake shell providers (zero network, zero
model, zero real API call), and freezes every number the page displays
into data/*.json.

Two evidence layers, pre-declared:

  one-time frozen (never re-measured; extract refuses to overwrite):
    unit-tests.json    wall-clock + per-package timings of one real
                       `go test ./... -v` run
    provenance.json    generated-at, tool versions, command ledger,
                       layer registry

  deterministic rebuild (double-run byte-identical, verified by
  tools/vacuum.py):
    engine-contract.json   live CLI/MCP/pipeline behaviour vs fakes
    catalog-facts.json     the embedded stable command table + checks
    repo-metrics.json      git ls-files census + LOC by role
    source-anchors.json    file:line anchors (audit layer only)

Determinism strategy: IG_WORK is a FIXED /tmp path, every engine child
runs with cwd=$IG_WORK/cwd, HOME=$IG_WORK/home and a minimal PATH, and
the fake providers emit constant strings — so absolute paths baked into
captured output are byte-stable across runs. A cold per-run GOCACHE
under IG_WORK keeps builds hermetic; module deps come from the shared
read-only module cache with GOPROXY=off (offline).

Usage (always from a /tmp flat copy, never inside the delivery tree):

    KG_ACME_ROOT=/path/to/kg-acme IG_OUT=/path/to/tree \
    IG_WORK=/tmp/kgacme-ig-run python3 extract.py

Environment (ALL required; missing = hard fail, no silent defaults):
    KG_ACME_ROOT   engine repo root (frozen HEAD verified)
    IG_OUT         delivery tree root
    IG_WORK        fixed /tmp work dir for this run (must start /tmp/)
"""

import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

FROZEN_HEAD = "9cea0c72ec89e67a8347e11ad60f08fb2b16f602"
EXPECTED_TRACKED = 50

ROOT = os.environ.get("KG_ACME_ROOT") or ""
OUT = Path(os.environ.get("IG_OUT") or "")
WORK = Path(os.environ.get("IG_WORK") or "")
for name, val in [("KG_ACME_ROOT", ROOT), ("IG_OUT", OUT), ("IG_WORK", WORK)]:
    if not val:
        print(f"extract: FATAL: {name} not set (required, no default)",
              file=sys.stderr)
        sys.exit(1)
if not WORK.is_absolute() or not str(WORK).startswith("/tmp/"):
    print("extract: FATAL: IG_WORK must be an absolute path under /tmp/",
          file=sys.stderr)
    sys.exit(1)
DATA = OUT / "data"
DATA.mkdir(parents=True, exist_ok=True)

# engine children: fixed cwd + fake HOME + minimal PATH (no ~/sync, so no
# real installed provider can leak into discovery); the fake providers are
# shell scripts that only use /bin builtins + sed/shasum from /usr/bin.
ENGINE_ENV = {
    "HOME": str(WORK / "home"),
    "PATH": "/usr/bin:/bin:/usr/sbin:/sbin",
    "IGPROBE_LOG": str(WORK / "home" / "provider.log"),
}
ENGINE_CWD = WORK / "cwd"


def die(msg):
    print(f"extract: FATAL: {msg}", file=sys.stderr)
    sys.exit(1)


def write_json(name, obj):
    (DATA / name).write_text(
        json.dumps(obj, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8")
    print(f"extract: wrote data/{name}")


def sha256_file(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def engine(binary, *args, check=True):
    """Run one engine binary in the hermetic env; return (rc, out, err)."""
    proc = subprocess.run(
        [str(WORK / "bin" / binary), *args],
        cwd=ENGINE_CWD, env=ENGINE_ENV, capture_output=True, text=True)
    if check and proc.returncode != 0:
        die(f"{binary} {' '.join(args)} failed rc={proc.returncode}\n"
            f"stdout:\n{proc.stdout[-1500:]}\nstderr:\n{proc.stderr[-1500:]}")
    return proc.returncode, proc.stdout, proc.stderr


def log_lines():
    p = WORK / "home" / "provider.log"
    if not p.exists():
        return []
    return [l for l in p.read_text(encoding="utf-8").splitlines() if l]


def engine_json(binary, *args, check=True):
    rc, out, err = engine(binary, *args, check=check)
    doc, source = None, None
    for text, where in ((out, "stdout"), (err, "stderr")):
        try:
            doc = json.loads(text)
            source = where
            break
        except json.JSONDecodeError:
            continue
    if doc is None:
        die(f"{binary} {' '.join(args)}: no single JSON document on stdout "
            f"or stderr:\nout={out[:400]}\nerr={err[:400]}")
    if source == "stderr":
        print(f"extract: note: {binary} {' '.join(args[:2])} emitted its "
              f"envelope on stderr (recorded)", file=sys.stderr)
    return rc, doc, err


# ============================================================ engine integrity
head = subprocess.run(["git", "-C", ROOT, "rev-parse", "HEAD"],
                      capture_output=True, text=True)
if head.returncode != 0:
    die("git rev-parse failed")
head = head.stdout.strip()
if head != FROZEN_HEAD:
    # distinguish "engine evolved" (normal after the delivery commit; the
    # frozen commit is still in history) from "evidence drifted" (frozen
    # commit unresolvable). Both refuse to re-freeze — anchors and timing
    # are line-sensitive — but the evolved case gets the worktree recipe.
    resolvable = subprocess.run(
        ["git", "-C", ROOT, "cat-file", "-e", FROZEN_HEAD + "^{commit}"],
        capture_output=True).returncode == 0
    if resolvable:
        die(f"engine evolved: HEAD is {head}, evidence was frozen at "
            f"{FROZEN_HEAD} — refusing to re-freeze from a moved tree "
            "(anchors are line-sensitive). Post-commit re-runs must use "
            "the frozen worktree recipe (README 提交后复现):\n"
            f"  git worktree add /tmp/kgacme-frozen {FROZEN_HEAD}\n"
            f"  KG_ACME_ROOT=/tmp/kgacme-frozen IG_OUT=… IG_WORK=… "
            "python3 extract.py")
    die(f"evidence drifted: frozen commit {FROZEN_HEAD} is no longer "
        f"resolvable in {ROOT} (history rewritten?) — hard fail")
dirty = subprocess.run(["git", "-C", ROOT, "status", "--porcelain"],
                       capture_output=True, text=True).stdout
tainted = [l for l in dirty.splitlines()
           if l.strip() and not l.startswith("??")]
if tainted:
    die(f"engine tracked content modified: {tainted[:5]}; evidence must come "
        "from a clean tree")
tracked = [l.strip() for l in subprocess.run(
    ["git", "-C", ROOT, "ls-files"], capture_output=True,
    text=True).stdout.splitlines() if l.strip()]
if len(tracked) != EXPECTED_TRACKED:
    die(f"expected {EXPECTED_TRACKED} tracked files, got {len(tracked)}")

# ============================================================ /tmp work copy
if WORK.exists():
    shutil.rmtree(WORK)
(WORK / "src").mkdir(parents=True)
(WORK / "home").mkdir()
(WORK / "fix").mkdir()
(WORK / "bin").mkdir()
ENGINE_CWD.mkdir()
archive = subprocess.run(["git", "-C", ROOT, "archive", FROZEN_HEAD],
                         capture_output=True)
if archive.returncode != 0:
    die(f"git archive failed: {archive.stderr.decode()[:400]}")
tar = subprocess.run(["tar", "-x", "-C", str(WORK / "src")],
                     input=archive.stdout, capture_output=True)
if tar.returncode != 0:
    die("tar extract failed")

GO_ENV = dict(os.environ,
              GOCACHE=str(WORK / "gocache"),
              GOPROXY="off",
              GOFLAGS="-mod=mod")
for name in ("kg", "kgctl", "kg-mcp"):
    p = subprocess.run(["go", "build", "-o", str(WORK / "bin" / name),
                        f"./cmd/{name}"], cwd=WORK / "src", env=GO_ENV,
                       capture_output=True, text=True)
    if p.returncode != 0:
        die(f"go build {name} failed:\n{p.stdout[-800:]}\n{p.stderr[-800:]}")
print("extract: built kg / kgctl / kg-mcp in the /tmp work copy "
      "(GOPROXY=off, offline)")


def fake_provider_script(provider_id, capabilities, protocol_versions=(1,)):
    """Deterministic sh fake speaking kg.provider/v1. Audit layer only."""
    manifest = {
        "protocol": "kg.provider/v1",
        "protocol_versions": list(protocol_versions),
        "provider": {"id": provider_id, "version": "1.0.0",
                     "description": "Fake provider " + provider_id},
        "capabilities": capabilities,
    }
    manifest_json = json.dumps(manifest, separators=(",", ":"),
                               ensure_ascii=False)
    available = '{"available":true,"ready":[],"missing":[]}'
    return (
        "#!/bin/sh\n"
        ': "${IGPROBE_LOG:=/dev/null}"\n'
        'printf \'%s\\n\' "$*" >> "$IGPROBE_LOG"\n'
        'case "$1" in\n'
        f"  describe) printf '%s\\n' '{manifest_json}' ;;\n"
        f"  available) printf '%s\\n' '{available}' ;;\n"
        "  invoke)\n"
        "    req=$(cat)\n"
        "    cap=$(printf '%s' \"$req\" | sed -n "
        "'s/.*\"capability_id\":\"\\([^\"]*\\)\".*/\\1/p')\n"
        "    if [ \"$cap\" = \"test.artifact\" ]; then\n"
        "      out=$(printf '%s' \"$req\" | sed -n "
        "'s/.*\"out\":\"\\([^\"]*\\)\".*/\\1/p')\n"
        "      printf 'probe-artifact\\n' > \"$out\"\n"
        "      sum=$(shasum -a 256 \"$out\" | cut -d' ' -f1)\n"
        "      printf '{\"protocol\":\"kg.execution/v1\","
        "\"capability_id\":\"test.artifact\","
        "\"provider\":\"fake\",\"status\":\"ok\","
        "\"artifacts\":[{\"path\":\"%s\",\"kind\":\"kg-document\","
        "\"checksum\":\"sha256:%s\"}]}\\n' \"$out\" \"$sum\"\n"
        "    elif [ \"$cap\" = \"test.net\" ]; then\n"
        "      printf '{\"protocol\":\"kg.execution/v1\","
        "\"capability_id\":\"test.net\",\"provider\":\"fake\","
        "\"status\":\"ok\",\"result\":{\"contacted\":true}}\\n'\n"
        "    else\n"
        "      printf '{\"protocol\":\"kg.execution/v1\","
        "\"capability_id\":\"test.echo\",\"provider\":\"fake\","
        "\"status\":\"ok\",\"result\":{\"value\":\"hello\"}}\\n'\n"
        "    fi ;;\n"
        "esac\n")


def cap(cid, title, desc, effects, props=None, required=None,
        mode="result-json", kind="json"):
    schema = {"type": "object", "properties": props or {},
              "additionalProperties": False}
    if required:
        schema["required"] = required
    return {"capability_id": cid, "title": title, "description": desc,
            "side_effects": effects, "input_schema": schema,
            "output": {"mode": mode, "kind": kind},
            "cli_spec": {"subcommand": [], "always": [], "positionals": [],
                         "flags": []}}


fake_caps = [
    cap("test.echo", "Echo a value",
        "Return the supplied value without changing it.", [],
        {"value": {"type": "string"}}, ["value"]),
    cap("test.net", "Use the network",
        "Pretend to call a remote service.", ["network", "data_egress"]),
    cap("test.artifact", "Write an artifact",
        "Write the requested artifact file.", [],
        {"out": {"type": "string"}}, ["out"], "artifact", "kg-document"),
]
(WORK / "fix" / "fake-provider").write_text(
    fake_provider_script("fake", fake_caps), encoding="utf-8")
(WORK / "fix" / "ver-provider").write_text(
    fake_provider_script("ver", fake_caps[:1], protocol_versions=(99,)),
    encoding="utf-8")
(WORK / "fix" / "ugly-provider").write_text(
    fake_provider_script("ugly", [cap(
        "test.magic", "Unknown effect",
        "Declare a side effect the hub does not know.", ["time_travel"])]),
    encoding="utf-8")
bad = ("#!/bin/sh\ncase \"$1\" in\n"
       "  describe) printf 'this is not json at all\\n' ;;\n"
       "  available) printf '%s\\n' "
       "'{\"available\":true,\"ready\":[],\"missing\":[]}' ;;\nesac\n")
(WORK / "fix" / "bad-provider").write_text(bad, encoding="utf-8")
for f in (WORK / "fix").iterdir():
    f.chmod(0o755)

FIX = WORK / "fix"
PROVIDER_BINS = []
for pid in ("fake", "ver", "ugly", "bad"):
    PROVIDER_BINS += ["--provider-bin", f"{pid}={FIX / (pid + '-provider')}"]

# ============================================================ live contract run
contract = {"note": "every field below is a live observation of the real "
                    "binaries built from the frozen HEAD, driven against "
                    "shell fake providers (zero network / zero model)"}
commands_ledger = []

rc, refresh_out, refresh_err = engine("kgctl", "refresh", *PROVIDER_BINS)
snapshot_rel = "Library/Caches/kg-acme/capability-snapshot.json"
snapshot_path = WORK / "home" / snapshot_rel
refresh_log = log_lines()
contract["refresh"] = {
    "rc": rc,
    "stdout_lines": [l for l in refresh_out.splitlines() if l.strip()],
    "snapshot_path_suffix": snapshot_rel,
    "provider_calls_during_refresh": len(refresh_log),
    "provider_call_sequence": refresh_log,
}
commands_ledger.append("kgctl refresh --provider-bin fake=… ver=… ugly=… "
                       "bad=… (4 fake providers)")

snap = json.loads(snapshot_path.read_text(encoding="utf-8"))
contract["snapshot"] = {
    "schema_version": snap["schema_version"],
    "fingerprint": snap["fingerprint"],
    "fingerprint_note": "content sha256 over providers+groups+capabilities; "
                        "recomputed identically on every rebuild",
    "semantic_ids": [c["semantic_id"] for c in snap["capabilities"]],
    "group_ids": [g["id"] for g in snap["groups"]],
    "providers": [{"id": p["Status"]["id"], "probed": p["Status"]["probed"],
                   "probe_error_code": p["Status"].get("probe_error_code")}
                  for p in snap["providers"]],
}

# ---- read-only surface: zero provider starts (measured) -------------------
readonly_cmds = [
    ("kg", ["--help"]),
    ("kg", ["list"]),
    ("kg", ["list", "--prefix", "test", "--level", "0", "--tree"]),
    ("kg", ["list", "--json"]),
    ("kg", ["test.echo", "--describe"]),
    ("kg", ["test", "--describe"]),
    ("kg", ["test.echo", "--params", '{"value":"hello"}', "--dry-run",
            "--json"]),
    ("kgctl", ["capabilities", "list"]),
    ("kgctl", ["route", "explain", "test.echo", "--json"]),
    ("kgctl", ["completion", "zsh"]),
]
before = len(log_lines())
readonly_runs = []
for binary, args in readonly_cmds:
    rc, out, err = engine(binary, *args)
    readonly_runs.append({"binary": binary, "argv": args[0], "rc": rc})
delta = len(log_lines()) - before
if delta != 0:
    die(f"read-only surface started providers {delta} times — expected 0")
contract["readonly_surface"] = {
    "runs": readonly_runs,
    "provider_starts": delta,
    "measured_by": "fake provider appends one log line per invocation",
}

# ---- help / tree / describe shapes ----------------------------------------
rc, out, _ = engine("kg", "--help")
contract["help_contract"] = {
    "has_capability_table": "CAPABILITY ID" in out,
    "lists_test_echo": "test.echo" in out,
    "no_kg_dotted_prefix": "kg.test.echo" not in out,
    "points_to_control_plane": "kgctl --help" in out,
    "global_flag_lines": [l.strip() for l in out.splitlines()
                          if l.startswith("  --")][:10],
}
rc, out, _ = engine("kg", "list", "--prefix", "test", "--level", "0",
                    "--tree")
contract["tree_text"] = out.splitlines()
rc, doc, _ = engine_json("kg", "list", "--json")
contract["list_json"] = {"items": [
    {"capability_id": i["capability_id"], "available": i["available"]}
    for i in doc["items"]]}
rc, doc, _ = engine_json("kg", "test.echo", "--describe")
contract["describe_atomic"] = doc
contract["describe_atomic_keys"] = sorted(doc.keys())
rc, doc, _ = engine_json("kg", "test", "--describe")
contract["describe_group"] = {"length": len(doc),
                              "capability_ids": [c["capability_id"]
                                                 for c in doc]}

# ---- dry-run plans ---------------------------------------------------------
rc, doc, _ = engine_json("kg", "test.echo", "--params",
                         '{"value":"hello"}', "--dry-run", "--json")
contract["dry_run_echo"] = doc
rc, doc, _ = engine_json("kg", "test.net", "--params", '{}',
                         "--dry-run", "--json")
contract["dry_run_net_no_flags"] = doc

# ---- execution: policy deny (provider never starts) and allow -------------
rc, doc, err = engine_json("kg", "test.net", "--params", '{}', "--json",
                           check=False)
contract["policy_denied"] = {
    "rc": rc,
    "envelope": doc,
    "first_stderr_line": err.splitlines()[0] if err.splitlines() else "",
}
before = len(log_lines())
rc, doc, _ = engine_json("kg", "test.net", "--params", '{}',
                         "--allow-network", "--allow-data-egress", "--json")
exec_delta = len(log_lines()) - before
tail = log_lines()[-3:]
if exec_delta != 3 or tail != ["describe --json", "available --json",
                               "invoke test.net --request -"]:
    die(f"execution path logged {exec_delta} provider calls "
        f"(expected exactly describe/available/invoke): {log_lines()[-6:]}")
contract["policy_allowed"] = doc
contract["execution_revalidation"] = {
    "provider_calls": exec_delta,
    "sequence": tail,
}
commands_ledger.append("kg test.net --params {} [--allow-*] --json "
                       "(policy deny + allow)")

# ---- error envelope + CLI failure contract --------------------------------
rc, doc, err = engine_json("kg", "no.such-capability", "--json", check=False)
contract["error_envelope"] = {"rc": rc, "envelope": doc}
failure_cases = [
    ["test.echo", "--describe", "--params", '{"value":"x"}'],
    ["test.echo", "--params", "nope"],
    ["test.echo", "--params", "null"],
    ["list", "--all"],
    ["list", "--provider-bin", "nope"],
]
failures = []
for args in failure_cases:
    rc, out, err = engine("kg", *args, check=False)
    failures.append({
        "argv": args[:2],
        "rc": rc,
        "first_stderr_line": err.splitlines()[0] if err.splitlines() else "",
    })
contract["cli_failure_contract"] = failures
rc, out, _ = engine("kg", "version")
contract["kg_version"] = out.strip()

# ---- provider probe failure taxonomy (live) --------------------------------
rc, doc, _ = engine_json("kgctl", "providers", "list", "--all", "--json")
taxonomy = {}
for p in doc["providers"]:
    taxonomy[p["id"]] = {
        "probed": p["probed"],
        "probe_error_code": p.get("probe_error_code"),
        "diagnostic_severity": [d["severity"]
                                for d in p.get("diagnostics", [])],
        "diagnostic_snippet": (p.get("diagnostics") or [{}])[0]
                                      .get("message", "")[:160],
    }
contract["probe_taxonomy"] = taxonomy
rc, doc, _ = engine_json("kgctl", "route", "explain", "test.echo", "--json")
contract["route_explain_keys"] = sorted(doc.keys())
contract["route_explain_capability_keys"] = sorted(
    doc["capability"].keys())
commands_ledger.append("kgctl providers list --all --json + route explain")

# ---- MCP: same snapshot, same gates ---------------------------------------
mcp_requests = [
    '{"jsonrpc":"2.0","id":1,"method":"initialize"}',
    '{"jsonrpc":"2.0","id":2,"method":"ping"}',
    '{"jsonrpc":"2.0","id":3,"method":"notifications/initialized"}',
    'not json',
    '{"jsonrpc":"2.0","id":4,"method":"resources/list"}',
    '{"jsonrpc":"2.0","id":5,"method":"tools/list"}',
    '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":'
    '"kg_test_echo","arguments":{"value":"hello","dry_run":true}}}',
    '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":'
    '"kg_test_net","arguments":{}}}',
]
before = len(log_lines())
proc = subprocess.run([str(WORK / "bin" / "kg-mcp")],
                      input="\n".join(mcp_requests) + "\n",
                      cwd=ENGINE_CWD, env=ENGINE_ENV, capture_output=True,
                      text=True)
if proc.returncode != 0:
    die(f"kg-mcp exited {proc.returncode}: {proc.stderr[:500]}")
responses = [json.loads(l) for l in proc.stdout.splitlines() if l.strip()]
mcp_delta = len(log_lines()) - before
if mcp_delta != 0:
    die(f"MCP session started providers {mcp_delta} times — expected 0")
by_id = {r["id"]: r for r in responses}
tools = by_id[5]["result"]["tools"]
deny = by_id[7]["result"]
contract["mcp"] = {
    "requests_sent": len(mcp_requests),
    "responses_returned": len(responses),
    "silent_frames": ["notifications/initialized", "not json"],
    "initialize_result": by_id[1]["result"],
    "ping_result": by_id[2]["result"],
    "unknown_method_error": by_id[4]["error"],
    "tool_names": sorted(t["name"] for t in tools),
    "tool_count": len(tools),
    "capability_tool_schema_props": {
        t["name"].removeprefix("kg_"): sorted(
            t["inputSchema"]["properties"].keys())
        for t in tools if t["name"].startswith("kg_test")},
    "dry_run_call": {
        "is_error": by_id[6]["result"]["isError"],
        "structured_status": by_id[6]["result"]["structuredContent"]
                                     .get("status"),
        "structured_capability": by_id[6]["result"]["structuredContent"]
                                         .get("capability_id"),
        "would_execute": by_id[6]["result"]["structuredContent"]
                                 ["result"].get("would_execute"),
    },
    "policy_denied_call": {
        "is_error": deny["isError"],
        "structured_content": deny["structuredContent"],
    },
    "provider_starts": mcp_delta,
}
commands_ledger.append("kg-mcp stdio session: initialize/ping/tools list/"
                       "tools call (snapshot only)")

# ---- pipeline: plan, typed edge, preflight, run, resume --------------------
defs = {
    "pipe-ok.json": {
        "pipeline": "kg.pipeline/v1", "name": "artifact-chain",
        "stages": [
            {"id": "write", "capability": "test.artifact",
             "input": {"out": str(ENGINE_CWD / "provider-out.json")}},
            {"id": "shout", "capability": "test.echo",
             "input_from": {"stage": "write",
                            "artifact_kind": "kg-document", "as": "value"}},
        ]},
    "pipe-bad-edge.json": {
        "pipeline": "kg.pipeline/v1", "name": "bad-edge",
        "stages": [
            {"id": "a", "capability": "test.echo",
             "input": {"value": "x"}},
            {"id": "b", "capability": "test.artifact",
             "input_from": {"stage": "a", "artifact_kind": "json",
                            "as": "out"}},
        ]},
    "pipe-net.json": {
        "pipeline": "kg.pipeline/v1", "name": "needs-net",
        "stages": [{"id": "n", "capability": "test.net", "input": {}}]},
}
for name, d in defs.items():
    (ENGINE_CWD / name).write_text(
        json.dumps(d, separators=(",", ":")) + "\n", encoding="utf-8")

rc, doc, _ = engine_json("kg", "pipeline.validate", "pipe-ok.json", "--json")
contract["pipeline_validate_plan"] = doc
rc, doc, err = engine_json("kg", "pipeline.validate", "pipe-bad-edge.json",
                           "--json", check=False)
contract["pipeline_bad_edge"] = {"rc": rc, "envelope": doc}
before = len(log_lines())
rc, doc, _ = engine_json("kg", "pipeline.run", "pipe-net.json",
                         "--dry-run", "--json", check=False)
net_delta = len(log_lines()) - before
if net_delta != 0:
    die(f"pipeline dry-run started providers {net_delta} times")
contract["pipeline_dry_run_gates"] = {
    "rc": rc, "envelope": doc, "provider_starts": net_delta}

wd = ENGINE_CWD / "wd"
if wd.exists():
    shutil.rmtree(wd)
wd.mkdir()
before = len(log_lines())
rc, doc, _ = engine_json("kg", "pipeline.run", "pipe-ok.json",
                         "--work-dir", "wd", "--json", check=False)
run_seq = log_lines()[before:]
contract["pipeline_run"] = {
    "rc": rc, "envelope": doc,
    "provider_call_sequence": run_seq,
    "work_dir_files": sorted(p.name for p in wd.iterdir()),
    "injected_downstream_input": doc["stages"][1]["input"]["value"],
}
rc, doc, _ = engine_json("kg", "pipeline.run", "pipe-ok.json",
                         "--work-dir", "wd", "--resume", "wd", "--json",
                         check=False)
resume_seq = log_lines()[before + len(run_seq):]
contract["pipeline_resume"] = {
    "rc": rc,
    "stage_statuses": [(s["id"], s["status"]) for s in doc["stages"]],
    "provider_call_sequence": resume_seq,
    "invoke_calls": sum(1 for l in resume_seq if l.startswith("invoke ")),
}
commands_ledger.append("kg pipeline validate/run/resume vs fakes "
                       "(typed-edge + gate preflight + artifact chain)")
write_json("engine-contract.json", contract)

# ============================================================ one-time test run
ut_path = DATA / "unit-tests.json"
if ut_path.exists():
    print("extract: data/unit-tests.json already frozen — NOT re-measured "
          "(one-time layer; delete it manually if a re-freeze is truly "
          "intended)")
else:
    t0 = time.time()
    proc = subprocess.run(["go", "test", "./...", "-v", "-count=1"],
                          cwd=WORK / "src", env=GO_ENV, capture_output=True,
                          text=True)
    elapsed = round(time.time() - t0, 1)
    if proc.returncode != 0:
        die(f"go test failed rc={proc.returncode}\n{proc.stdout[-2500:]}"
            f"\n{proc.stderr[-800:]}")
    pkgs = []
    for m in re.finditer(r"^(ok|FAIL)\s+([\w./-]+)(?:\s+([\d.]+)s)?",
                         proc.stdout, re.M):
        pkgs.append({"package": m.group(2), "status": m.group(1),
                     "seconds": m.group(3)})
    if not pkgs:
        die("could not parse any per-package result line from go test")
    total_passed = proc.stdout.count("--- PASS")
    total_failed = proc.stdout.count("--- FAIL")
    write_json("unit-tests.json", {
        "command": "go test ./... -v -count=1",
        "where": "IG_WORK /tmp copy of frozen HEAD (engine repo read-only)",
        "offline": "GOPROXY=off GOFLAGS=-mod=mod",
        "layer": "one-time frozen (timing evidence; never re-measured)",
        "packages": [{"package": p["package"], "status": p["status"],
                      "seconds": p["seconds"]} for p in pkgs],
        "total_passed": total_passed,
        "total_failed": total_failed,
        "wall_seconds": elapsed,
        "rc": 0,
    })

# ============================================================ repo metrics
def loc(path):
    return len((Path(ROOT) / path).read_text(
        encoding="utf-8", errors="replace").splitlines())


by_ext = {}
for t in tracked:
    ext = t.rsplit(".", 1)[-1] if "." in t else "(no-ext)"
    by_ext.setdefault(ext, []).append(t)
go_impl = sorted(t for t in by_ext.get("go", []) if not t.endswith("_test.go"))
go_test = sorted(t for t in by_ext.get("go", []) if t.endswith("_test.go"))

ROLE_MAP = [
    ("命令界面（三入口共享）", ["internal/cli/cli.go", "internal/cli/control.go",
                                "internal/cli/navigation.go"]),
    ("路由与执行", ["internal/router/router.go"]),
    ("流水线编排", ["internal/pipeline/pipeline.go",
                    "internal/pipeline/run.go"]),
    ("提供者发现", ["internal/discover/discover.go",
                    "internal/discover/exec.go"]),
    ("能力快照构建", ["internal/surface/surface.go"]),
    ("兼容桥（fallback 表）", ["internal/bridge/bridge.go"]),
    ("协议契约类型", ["internal/protocol/types.go",
                      "internal/protocol/negotiate.go"]),
    ("能力目录", ["internal/catalog/catalog.go"]),
    ("策略门", ["internal/policy/policy.go"]),
    ("模式校验", ["internal/schema/schema.go"]),
    ("快照存取", ["internal/state/store.go"]),
    ("二进制入口", ["cmd/kg/main.go", "cmd/kgctl/main.go",
                    "cmd/kg-mcp/main.go"]),
]
role_loc = []
for role, files in ROLE_MAP:
    role_loc.append({"role": role, "files": len(files),
                     "loc": sum(loc(f) for f in files)})
mapped = {f for _, fs in ROLE_MAP for f in fs}
unmapped = [t for t in go_impl if t not in mapped]
if unmapped:
    die(f"role map missed implementation files: {unmapped}")
if sum(r["loc"] for r in role_loc) != sum(loc(t) for t in go_impl):
    die("role LOC sum mismatch")

SOURCE_CONSTS = [
    ("snapshot_schema", "internal/surface/surface.go",
     r'SnapshotSchema = "([^"]+)"'),
    ("curated_group_namespaces", "internal/surface/surface.go",
     r'curated := map\[string\]string\{(.*?)\}'),
    ("protocol_native_bins", "internal/router/router.go",
     r'ProtocolNativeBins = \[\]string\{(.*?)\}'),
    ("fallback_bridge_ids", "internal/bridge/bridge.go",
     r'"(kg-extract|kg-mm|ygr)"'),
    ("supported_versions", "internal/protocol/types.go",
     r'SupportedVersions = \[\]int\{(.*?)\}'),
]
src_consts, src_prov = {}, []
for name, fname, pat in SOURCE_CONSTS:
    text = (Path(ROOT) / fname).read_text(encoding="utf-8",
                                          errors="replace")
    if name == "curated_group_namespaces":
        m = re.search(pat, text, re.S)
        keys = re.findall(r'"([a-z]+)":', m.group(1)) if m else []
        src_consts[name] = sorted(set(keys))
    elif name == "protocol_native_bins":
        m = re.search(pat, text, re.S)
        src_consts[name] = re.findall(r'"([\w-]+)"', m.group(1)) if m else []
    elif name == "fallback_bridge_ids":
        src_consts[name] = sorted(set(re.findall(pat, text)))
    elif name == "supported_versions":
        m = re.search(pat, text, re.S)
        src_consts[name] = [int(x) for x in
                            re.findall(r"\d+", m.group(1))] if m else []
    else:
        m = re.search(pat, text)
        src_consts[name] = m.group(1) if m else None
    if not src_consts[name]:
        die(f"source constant pattern not found: {name} in {fname}")
    src_prov.append({"name": name, "file": fname, "pattern": pat,
                     "value": src_consts[name]})

write_json("repo-metrics.json", {
    "head": head,
    "tracked_total": len(tracked),
    "by_ext": {ext: len(v) for ext, v in sorted(by_ext.items())},
    "ext_sum_check": sum(len(v) for v in by_ext.values()),
    "go_impl_files": len(go_impl),
    "go_test_files": len(go_test),
    "spec_files": len(by_ext.get("md", [])),
    "spec_loc": sum(loc(t) for t in by_ext.get("md", [])),
    "impl_loc": sum(loc(t) for t in go_impl),
    "test_loc": sum(loc(t) for t in go_test),
    "go_total_loc": sum(loc(t) for t in by_ext.get("go", [])),
    "test_fn_count": sum(len(re.findall(r"^func Test\w+\(", (
        Path(ROOT) / t).read_text(encoding="utf-8", errors="replace"),
        re.M)) for t in go_test),
    "role_loc": sorted(role_loc, key=lambda r: -r["loc"]),
    "largest_impl_files": sorted(({"file": t, "loc": loc(t)}
                                  for t in go_impl),
                                 key=lambda x: -x["loc"])[:5],
    "source_constants": src_consts,
    "source_constant_provenance": src_prov,
    "file_snapshot": tracked,
})

# ============================================================ catalog facts
cat = json.loads((Path(ROOT) / "internal" / "catalog" / "catalog.json")
                 .read_text(encoding="utf-8"))
commands = cat["commands"]
namespaces = sorted({c["capability_id"].split(".")[0]
                     for c in commands if c.get("capability_id")})


def path_text(c):
    p = c["command_path"]
    return " ".join(p) if isinstance(p, list) else str(p)


# mirror rule (engine CLAUDE.md): semantic_id mirrors command_path segments
# joined by dots — extract.entities-relations <-> [extract, entities-relations]
def path_dotted(c):
    p = c["command_path"]
    return ".".join(p) if isinstance(p, list) else str(p)


mirror_ok = all(path_dotted(c) == c["semantic_id"] for c in commands)
if not mirror_ok:
    die("catalog semantic_id no longer mirrors command_path — C08 anchor "
        "has drifted, re-audit before freezing")
write_json("catalog-facts.json", {
    "version": cat["version"],
    "command_count": len(commands),
    "builtin_count": sum(1 for c in commands if not c.get("capability_id")),
    "capability_command_count": sum(1 for c in commands
                                    if c.get("capability_id")),
    "published_namespaces": namespaces,
    "namespace_count": len(namespaces),
    "semantic_id_mirrors_command_path": mirror_ok,
    "commands": [{"command": path_text(c),
                  "semantic_id": c["semantic_id"],
                  "capability_id": c.get("capability_id"),
                  "builtin": not c.get("capability_id")}
                 for c in commands],
})

# ============================================================ source anchors
ANCHORS = [
    ("C01", "README.md", "kg`：执行面"),
    ("C01", "README.md", "kgctl`：管理面"),
    ("C01", "README.md", "kg-mcp`：从同一份不可变快照发布 MCP tools"),
    ("C01", "CLAUDE.md", "入口 `cmd/kg`"),
    ("C02", "spec/00-overview.md", "hub 只做集成，绝不自实现 KG 算法"),
    ("C02", "internal/discover/discover.go", "func FindExecutable"),
    ("C03", "spec/00-overview.md", "hub 不写死 provider 的选项/枚举"),
    ("C03", "internal/bridge/bridge.go", "cli_spec differs from hub data"),
    ("C04", "README.md", "capability-snapshot.json"),
    ("C04", "internal/surface/surface.go", "func Build"),
    ("C05", "tests/e2e_test.go", "TestSnapshotMetadataAndDryRunNeverStart"
     "Provider"),
    ("C05", "tests/e2e_test.go", "metadata/dry-run started provider"),
    ("C06", "tests/e2e_test.go", "TestActualInvocationRevalidatesOnlySelected"
     "Provider"),
    ("C06", "tests/e2e_test.go", "describe, available, then invoke exactly "
     "once"),
    ("C07", "spec/02-catalog-and-routing.md", "稳定命令面"),
    ("C07", "internal/catalog/catalog.go", "go:embed"),
    ("C08", "CLAUDE.md", "semantic_id 镜像 command_path"),
    ("C08", "internal/catalog/catalog.go", "func Load"),
    ("C09", "internal/surface/surface.go", "func PublicID"),
    ("C09", "internal/surface/surface.go", "func normalizeID"),
    ("C09", "internal/surface/surface.go", "curated := map[string]string"),
    ("C10", "spec/02-catalog-and-routing.md", "Discovery：provider 发现顺序"),
    ("C10", "internal/discover/discover.go", "kg-provider-"),
    ("C11", "CLAUDE.md", "malformed_manifest"),
    ("C11", "internal/protocol/negotiate.go", "VersionError"),
    ("C11", "spec/01-provider-protocol.md", "不降级"),
    ("C12", "internal/surface/surface.go", "left.Probed != right.Probed"),
    ("C12", "spec/02-catalog-and-routing.md", "probed 优先于 fallback"),
    ("C13", "spec/01-provider-protocol.md", "argv 发射序"),
    ("C13", "internal/bridge/bridge.go",
     "Always ++ Subcommand ++ Positionals ++ Flags"),
    ("C13", "internal/bridge/bridge.go", "func RenderArgv"),
    ("C14", "spec/03-policy-gates.md", "默认全拒"),
    ("C14", "internal/policy/policy.go", "func AllowFlag"),
    ("C15", "internal/schema/provider-v1.schema.json",
     '"enum": ["network", "data_egress", "downloads_models", '
     '"writes_db"]'),
    ("C15", "internal/policy/policy.go", "fail-closed"),
    ("C16", "spec/03-policy-gates.md", "would_execute"),
    ("C16", "internal/router/router.go", "dryRun is true it renders the"),
    ("C17", "internal/protocol/types.go", "ErrUnsupportedSchemaVersion"),
    ("C17", "internal/protocol/types.go", "ErrIncompatibleStageEdge"),
    ("C18", "CLAUDE.md", "恰好一个"),
    ("C18", "internal/cli/cli.go", "cannot be combined"),
    ("C19", "spec/05-pipeline.md", "hub 仍然只做编排"),
    ("C19", "internal/pipeline/run.go",
     "stage by stage in topological order"),
    ("C20", "spec/05-pipeline.md", "可接线"),
    ("C20", "internal/pipeline/pipeline.go", "no artifact to wire"),
    ("C21", "spec/05-pipeline.md", "Kahn 拓扑排序"),
    ("C21", "internal/pipeline/pipeline.go", "cycle"),
    ("C22", "spec/05-pipeline.md", "策略门预检"),
    ("C22", "internal/pipeline/pipeline.go", "Resolved.SideEffects"),
    ("C23", "spec/05-pipeline.md", "artifact 落 work-dir"),
    ("C23", "internal/pipeline/run.go", "sha256"),
    ("C24", "spec/05-pipeline.md", "断点重跑"),
    ("C24", "internal/pipeline/run.go", "reused"),
    ("C25", "spec/06-mcp.md", "运行时派生"),
    ("C25", "cmd/kg-mcp/main.go", "tools/list"),
    ("C26", "spec/06-mcp.md", "OR 合并"),
    ("C26", "internal/policy/policy.go", "func ParseGates"),
    ("C26", "internal/policy/policy.go", "unknown side effect"),
    ("C27", "spec/06-mcp.md", "结构化错误"),
    ("C27", "cmd/kg-mcp/main.go", "-32601"),
    ("C28", "tests/e2e_test.go", "would_execute"),
    ("C28", "tests/e2e_test.go", "kg.error/v1"),
    ("C29", "(computed)", "git ls-files + line counts (repo-metrics.json)"),
    ("C30", "CLAUDE.md", "测试分两层"),
    ("C30", "tests/e2e_test.go", "fake-provider"),
]
anchors = []
for claim, fname, pattern in ANCHORS:
    if fname.startswith("("):
        anchors.append({"claim": claim, "file": fname, "pattern": pattern,
                        "kind": "computed-or-live"})
        continue
    lines = (Path(ROOT) / fname).read_text(
        encoding="utf-8", errors="replace").splitlines()
    hits = [i + 1 for i, l in enumerate(lines) if pattern in l]
    if not hits:
        die(f"anchor pattern not found: {claim} {fname} {pattern!r}")
    anchors.append({"claim": claim, "file": fname, "line": hits[0],
                    "pattern": pattern,
                    "excerpt": lines[hits[0] - 1].strip()[:120],
                    "extra_hits": hits[1:4]})
write_json("source-anchors.json", {
    "note": "file:line anchors frozen against the engine HEAD; audit layer "
            "only — never rendered on the page",
    "head": head,
    "anchors": anchors,
})

# ============================================================ provenance
prov_path = DATA / "provenance.json"
if prov_path.exists():
    print("extract: data/provenance.json already frozen — kept (one-time "
          "layer)")
else:
    provenance = {
        "engine_root_env": "KG_ACME_ROOT",
        "engine_head": head,
        "frozen_head": FROZEN_HEAD,
        "engine_clean_tree": True,
        "tracked_file_count": len(tracked),
        "work_copy": "git archive of frozen HEAD into IG_WORK (/tmp); "
                     "engine repo never written",
        "offline_build": "GOPROXY=off GOFLAGS=-mod=mod (module cache)",
        "fake_providers": "4 shell fakes (fake/ver/ugly/bad): zero network, "
                          "zero model, zero real API call",
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "tool_versions": {
            "go": subprocess.run(["go", "version"], capture_output=True,
                                 text=True).stdout.strip(),
            "python": sys.version.split()[0],
        },
        "layer_registry": {
            "one_time_frozen": [
                {"file": "unit-tests.json",
                 "why": "wall-clock + per-package timings of one real "
                        "go-test run; re-measuring would produce different "
                        "seconds",
                 "policy": "extract refuses to overwrite; vacuum asserts "
                           "byte-unchanged"},
                {"file": "provenance.json",
                 "why": "generated-at timestamp + tool versions",
                 "policy": "extract refuses to overwrite; vacuum asserts "
                           "byte-unchanged"},
            ],
            "deterministic_rebuild": [
                {"file": "engine-contract.json",
                 "why": "fixed IG_WORK paths + hermetic env + constant fake "
                        "outputs make every captured byte reproducible"},
                {"file": "catalog-facts.json",
                 "why": "pure function of the embedded catalog"},
                {"file": "repo-metrics.json",
                 "why": "pure function of git ls-files + line counts"},
                {"file": "source-anchors.json",
                 "why": "pure function of the frozen HEAD text"},
                {"file": "display-exemptions.json",
                 "why": "written by build.py from the same inputs"},
                {"file": "fingerprints.json",
                 "why": "written by fingerprint.py over the rebuilt tree"},
            ],
        },
        "commands": commands_ledger + [
            "go build ./cmd/{kg,kgctl,kg-mcp} (work copy, offline)",
            "go test ./... -v -count=1 (work copy; one-time layer)",
            "git ls-files + line counts + anchored regex reads (read-only)",
        ],
    }
    provenance["data_sha256"] = {}
    for f in sorted(DATA.glob("*.json")):
        if f.name != "provenance.json":
            provenance["data_sha256"][f.name] = sha256_file(f)
    write_json("provenance.json", provenance)

shutil.rmtree(WORK, ignore_errors=True)
print("extract: DONE (work copy removed; deterministic layer rebuildable, "
      "timing layer frozen)")
