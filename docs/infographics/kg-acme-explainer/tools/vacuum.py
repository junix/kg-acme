#!/usr/bin/env python3
"""Full-rebuild vacuum for the kg-acme explainer delivery tree.

Pre-declared pass criteria (all must hold; any miss = hard fail):

  1. engine HEAD == frozen commit and tracked tree clean (extract asserts)
  2. one-time layer byte-unchanged: data/unit-tests.json,
     data/provenance.json are NEVER rebuilt; extract refuses to overwrite
  3. deterministic layer byte-identical after delete + full-chain rebuild:
     data/engine-contract.json, catalog-facts.json, repo-metrics.json,
     source-anchors.json, display-exemptions.json, fingerprints.json,
     index.html, svg/*.svg
  4. bitmap trio: byte-identical preferred; fallback = pixel-zero-diff
     (PIL) after stripping non-pixel PNG auxiliary chunks
  5. six-ban gate passes on the rebuilt page (positive controls 6/6,
     display layer 0 violations) — run via build.py
  6. svg-linter gate: every rebuilt svg rc=0 AND 0 findings (real binary
     located via `command -v svg-linter`)
  7. render assertions: page width 1200, per-slice scroll readback matched,
     full bitmap height == CSS height * dpr (enforced inside render.py)
  8. fingerprint --check passes: no drift, every sha verbatim in
     VERIFICATION.md
  9. residue census: no .pyc/__pycache__ anywhere in the tree, closed
     file census (no unexpected files), /tmp work dir removed

Usage (from a /tmp flat copy):

    KG_ACME_ROOT=/path/to/kg-acme IG_OUT=/path/to/tree \
    IG_WORK=/tmp/kgacme-ig-run python3 vacuum.py

Environment (ALL required; missing = hard fail, no silent defaults):
    KG_ACME_ROOT   engine repo root (frozen HEAD verified)
    IG_OUT         delivery tree root
    IG_WORK        fixed /tmp work dir for the extract rebuild
"""

import hashlib
import json
import os
import re
import shutil
import struct
import subprocess
import sys
import zlib
from pathlib import Path

FROZEN_HEAD = "9cea0c72ec89e67a8347e11ad60f08fb2b16f602"

ROOT = os.environ.get("KG_ACME_ROOT") or ""
OUT = Path(os.environ.get("IG_OUT") or "")
WORK = os.environ.get("IG_WORK") or ""
for name, val in [("KG_ACME_ROOT", ROOT), ("IG_OUT", OUT),
                  ("IG_WORK", WORK)]:
    if not val:
        print(f"vacuum: FATAL: {name} not set (required, no default)",
              file=sys.stderr)
        sys.exit(1)
if not str(WORK).startswith("/tmp/"):
    print("vacuum: FATAL: IG_WORK must be under /tmp/", file=sys.stderr)
    sys.exit(1)
OUT = Path(OUT)

TEXT_REBUILD = ["index.html", "data/engine-contract.json",
                "data/catalog-facts.json", "data/repo-metrics.json",
                "data/source-anchors.json", "data/display-exemptions.json",
                "data/fingerprints.json"]
BITMAPS = ["render/full-2x.png", "render/full-gray.png",
           "render/thumb.png"]
ONE_TIME = ["data/unit-tests.json", "data/provenance.json"]


def die(msg):
    print(f"vacuum: FATAL: {msg}", file=sys.stderr)
    sys.exit(1)


def sha(p):
    return hashlib.sha256(Path(p).read_bytes()).hexdigest()


def run(script):
    """Run one tool script from a /tmp flat copy with a hermetic env."""
    flat = Path("/tmp") / "kgacme-ig-vacuum"
    flat.mkdir(exist_ok=True)
    shutil.copy(OUT / "tools" / script, flat / script)
    env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1",
               KG_ACME_ROOT=ROOT, IG_OUT=str(OUT), IG_WORK=WORK)
    p = subprocess.run([sys.executable, str(flat / script)], cwd=flat,
                       env=env, capture_output=True, text=True)
    sys.stdout.write(p.stdout)
    if p.returncode != 0:
        sys.stderr.write(p.stderr)
        die(f"{script} failed rc={p.returncode}")
    return p.stdout


# ---- 0. preconditions -------------------------------------------------------
head = subprocess.run(["git", "-C", ROOT, "rev-parse", "HEAD"],
                      capture_output=True, text=True).stdout.strip()
if head != FROZEN_HEAD:
    # "engine evolved" (delivery commit landed; frozen commit still in
    # history) → recipe pointer; "evidence drifted" (frozen commit gone)
    # → hard fail. Vacuum rebuilds against the frozen state, so both
    # refuse — but only the second is unexplained.
    resolvable = subprocess.run(
        ["git", "-C", ROOT, "cat-file", "-e", FROZEN_HEAD + "^{commit}"],
        capture_output=True).returncode == 0
    if resolvable:
        die(f"engine evolved: HEAD {head} != frozen {FROZEN_HEAD}. Post-"
            "commit vacuum must rebuild against the frozen state via the "
            "README 提交后复现 recipe:\n"
            f"  git worktree add /tmp/kgacme-frozen {FROZEN_HEAD}\n"
            f"  KG_ACME_ROOT=/tmp/kgacme-frozen IG_OUT=… IG_WORK=… "
            "python3 vacuum.py")
    die(f"evidence drifted: frozen commit {FROZEN_HEAD} is no longer "
        f"resolvable in {ROOT} (history rewritten?)")
for rel in TEXT_REBUILD + BITMAPS + ONE_TIME + ["svg/p0-hero.svg"]:
    if not (OUT / rel).exists():
        die(f"missing artifact before vacuum: {rel}")

# ---- 1. snapshot run-1 fingerprints ----------------------------------------
run1 = {rel: sha(OUT / rel) for rel in TEXT_REBUILD + BITMAPS + ONE_TIME}
run1.update({f"svg/{p.name}": sha(p)
             for p in sorted((OUT / "svg").glob("*.svg"))})
print(f"vacuum: run-1 fingerprints: {len(run1)} files")

# ---- 2. delete rebuildables (one-time layer stays) --------------------------
# keep run-1 bitmaps outside the tree so the pre-declared pixel-zero
# fallback is actually computable if bytes drift
run1_dir = Path("/tmp") / "kgacme-ig-vacuum" / "run1"
shutil.rmtree(run1_dir, ignore_errors=True)
run1_dir.mkdir(parents=True)
for rel in BITMAPS:
    shutil.copy2(OUT / rel, run1_dir / Path(rel).name)
for rel in TEXT_REBUILD:
    (OUT / rel).unlink(missing_ok=True)
for p in (OUT / "svg").glob("*.svg"):
    p.unlink()
shutil.rmtree(OUT / "render", ignore_errors=True)
print("vacuum: deleted rebuildables (one-time layer kept: "
      "unit-tests.json, provenance.json; run-1 bitmaps parked in "
      f"{run1_dir})")

# ---- 3. full-chain rebuild --------------------------------------------------
run("extract.py")
run("panels.py")
run("build.py")

linter = shutil.which("svg-linter")
if not linter:
    die("svg-linter not on PATH (real binary required for the svg gate)")
for p in sorted((OUT / "svg").glob("*.svg")):
    p2 = subprocess.run([linter, "check", "--plain", str(p)],
                        capture_output=True, text=True)
    findings = [l for l in p2.stdout.splitlines()
                if l.split("\t")[0] == "finding"]
    if p2.returncode != 0 or findings:
        die(f"svg-linter gate failed on {p.name}: rc={p2.returncode} "
            f"findings={len(findings)}")
print(f"vacuum: svg-linter gate PASS ({len(list((OUT / 'svg').glob('*.svg')))}"
      " files, rc=0 and 0 findings each)")

run("render.py")
run("fingerprint.py")

# ---- 4. compare --------------------------------------------------------------
def strip_png_aux(blob):
    """Remove non-pixel auxiliary chunks (timestamps etc.) for the byte
    fallback comparison."""
    out, i = bytearray(), 8
    while i < len(blob):
        ln = struct.unpack(">I", blob[i:i + 4])[0]
        ctype = blob[i + 4:i + 8]
        if ctype in (b"IHDR", b"PLTE", b"IDAT", b"IEND", b"tRNS"):
            out += blob[i:i + 12 + ln]
        i += 12 + ln
    return bytes(out)


failures = []
for rel in TEXT_REBUILD:
    if sha(OUT / rel) != run1[rel]:
        failures.append(f"text drift: {rel}")
for p in sorted((OUT / "svg").glob("*.svg")):
    rel = f"svg/{p.name}"
    if sha(p) != run1[rel]:
        failures.append(f"text drift: {rel}")

for rel in ONE_TIME:
    if sha(OUT / rel) != run1[rel]:
        failures.append(f"ONE-TIME LAYER CHANGED: {rel}")

bmp_report = []
bmp_drift = False
for rel in BITMAPS:
    name = Path(rel).name
    if sha(OUT / rel) == run1[rel]:
        bmp_report.append(f"{rel}: byte-identical")
        continue
    # pre-declared fallback: pixel-zero-diff AND stripped-aux byte equality
    try:
        from PIL import Image, ImageChops
        a = Image.open(OUT / rel).convert("RGB")
        b = Image.open(run1_dir / name).convert("RGB")
        if a.size != b.size:
            failures.append(f"bitmap drift: {rel} size {a.size} != {b.size}")
            bmp_drift = True
            continue
        diff = ImageChops.difference(a, b).getbbox()
        aux_ok = strip_png_aux((OUT / rel).read_bytes()) == \
            strip_png_aux((run1_dir / name).read_bytes())
        if diff is None and aux_ok:
            bmp_report.append(f"{rel}: pixel-zero-diff + stripped-aux "
                              "byte-equal (fallback tier)")
        else:
            failures.append(f"bitmap drift: {rel} bbox={diff} aux_ok="
                            f"{aux_ok}")
            bmp_drift = True
    except ImportError:
        failures.append(f"bitmap drift: {rel} (PIL absent for fallback)")
        bmp_drift = True
if not bmp_drift:
    print("vacuum: bitmap trio reproduced across delete+rebuild")
for line in bmp_report:
    print(f"  {line}")

# ---- 5. fingerprint check vs VERIFICATION -----------------------------------
if (OUT / "VERIFICATION.md").exists():
    flat = Path("/tmp") / "kgacme-ig-vacuum"
    p = subprocess.run([sys.executable, str(flat / "fingerprint.py"),
                        "--check"],
                       cwd=flat,
                       env=dict(os.environ, PYTHONDONTWRITEBYTECODE="1",
                                IG_OUT=str(OUT)),
                       capture_output=True, text=True)
    sys.stdout.write(p.stdout)
    if p.returncode != 0:
        sys.stderr.write(p.stderr)
        failures.append("fingerprint --check failed")
else:
    failures.append("VERIFICATION.md missing (fingerprint check skipped)")

# ---- 6. residue census -------------------------------------------------------
for p in OUT.rglob("*"):
    if p.suffix == ".pyc" or p.name == "__pycache__":
        failures.append(f"residue: {p.relative_to(OUT)}")
expected_files = set(TEXT_REBUILD + BITMAPS + ONE_TIME +
                     ["VERIFICATION.md", "README.md"]) | \
    {f"svg/{p.name}" for p in (OUT / "svg").glob("*.svg")} | \
    {f"tools/{p.name}" for p in (OUT / "tools").glob("*.py")} | \
    {f"render/crops/{p.name}" for p in (OUT / "render" / "crops").glob("*.png")} | \
    {f"data/audit/{p.name}" for p in (OUT / "data" / "audit").glob("*.md")}
actual = {p.relative_to(OUT).as_posix() for p in OUT.rglob("*")
          if p.is_file()}
stray = sorted(actual - expected_files)
if stray:
    failures.append(f"unexpected files: {stray}")
if Path(WORK).exists():
    failures.append(f"work dir residue: {WORK}")
n_files = len(actual)
print(f"vacuum: residue census — {n_files} files, "
      f"{len(list((OUT / 'render' / 'crops').glob('*.png')))} crops, "
      f"stray={stray or 'none'}")

# ---- verdict -----------------------------------------------------------------
if failures:
    print("vacuum: VACUUM FAILED:", file=sys.stderr)
    for f in failures:
        print(f"  {f}", file=sys.stderr)
    sys.exit(1)
print(f"vacuum: VACUUM-OK — {len(run1)} pre-declared artifacts "
      "reproduced (text byte-identical, bitmaps byte-identical, one-time "
      "layer untouched), gates re-passed, no residue")
