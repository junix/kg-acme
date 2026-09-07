#!/usr/bin/env python3
"""Fingerprint table for the kg-acme explainer delivery tree.

Modes:
  (default)  write data/fingerprints.json — sha256 + size for every
             reproducible/ frozen artifact in the tree, EXCLUDING
             VERIFICATION.md (the audit doc that quotes this table) and
             fingerprints.json itself (self-hash is impossible). No
             timestamps: the table is deterministic and double-run stable.
  --check    recompute every listed sha from the live tree (drift gate),
             then assert every sha — including the sha of
             data/fingerprints.json itself — appears VERBATIM in
             VERIFICATION.md. Any missing/mismatched sha = hard fail.

Usage (from a /tmp flat copy):

    IG_OUT=/path/to/tree python3 fingerprint.py [--check]

Environment (required; missing = hard fail, no silent defaults):
    IG_OUT   delivery tree root
"""

import hashlib
import json
import os
import sys
from pathlib import Path

OUT = os.environ.get("IG_OUT") or ""
if not OUT:
    print("fingerprint: FATAL: IG_OUT not set (required, no default)",
          file=sys.stderr)
    sys.exit(1)
OUT = Path(OUT)

EXCLUDED = {"VERIFICATION.md", "README.md", "data/fingerprints.json"}
REL_GLOBS = ["index.html", "svg/*.svg", "data/*.json", "render/full-2x.png",
             "render/full-gray.png", "render/thumb.png"]
# post-commit run records are fingerprint-exempt BY DESIGN (delta fixpoint
# rule: recording a run inside a fingerprinted file would force another
# commit, which would force another run — no fixpoint)
EXEMPT_PREFIXES = ("tools/", "render/crops/", "data/audit/")


def collect():
    files = set()
    for g in REL_GLOBS:
        files.update(p.relative_to(OUT).as_posix()
                     for p in OUT.glob(g) if p.is_file())
    # never hash our own previous output: a table row carrying the OLD
    # fingerprints.json sha would drift on the next write (self-hash is
    # impossible, and re-running on an existing tree must be byte-stable)
    files.discard("data/fingerprints.json")
    unexpected = []
    for p in OUT.rglob("*"):
        if not p.is_file():
            continue
        rel = p.relative_to(OUT).as_posix()
        if rel in EXCLUDED or rel.startswith(EXEMPT_PREFIXES) or \
                rel in {"fingerprint.py"}:
            continue
        if rel not in files:
            unexpected.append(rel)
    return sorted(files), sorted(unexpected)


def sha256_file(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def build_table():
    files, unexpected = collect()
    if unexpected:
        print(f"fingerprint: FATAL: unexpected files in tree (census is "
              f"closed): {unexpected}", file=sys.stderr)
        sys.exit(1)
    table = [{"path": rel, "sha256": sha256_file(OUT / rel),
              "bytes": (OUT / rel).stat().st_size} for rel in files]
    return {"note": "deterministic fingerprint table; excluded: "
                    "VERIFICATION.md (quotes this table), README.md, this "
                    "file itself, data/audit/ (post-commit run records, "
                    "exempt by design); render/crops are derived from "
                    "full-2x and byte-covered by the render assertions",
            "count": len(table),
            "files": table}


def main():
    dest = OUT / "data" / "fingerprints.json"
    if "--check" in sys.argv:
        if not dest.exists():
            print("fingerprint: FATAL: data/fingerprints.json missing",
                  file=sys.stderr)
            sys.exit(1)
        fp = json.loads(dest.read_text(encoding="utf-8"))
        verif = OUT / "VERIFICATION.md"
        if not verif.exists():
            print("fingerprint: FATAL: VERIFICATION.md missing",
                  file=sys.stderr)
            sys.exit(1)
        vtext = verif.read_text(encoding="utf-8")
        missing_verbatim, drifted = [], []
        for entry in fp["files"]:
            live = sha256_file(OUT / entry["path"])
            if live != entry["sha256"]:
                drifted.append(entry["path"])
            if entry["sha256"] not in vtext:
                missing_verbatim.append(entry["path"])
        self_sha = sha256_file(dest)
        if self_sha not in vtext:
            missing_verbatim.append("data/fingerprints.json (self)")
        if drifted or missing_verbatim:
            print(f"fingerprint: CHECK FAILED: drifted={drifted} "
                  f"not-verbatim-in-VERIFICATION={missing_verbatim}",
                  file=sys.stderr)
            sys.exit(1)
        print(f"fingerprint: CHECK PASS — {fp['count']} files drift-free, "
              f"every sha verbatim in VERIFICATION.md (incl. table self-sha "
              f"{self_sha[:12]}…)")
        return
    table = build_table()
    dest.write_text(json.dumps(table, ensure_ascii=False, indent=2) + "\n",
                    encoding="utf-8")
    print(f"fingerprint: wrote data/fingerprints.json "
          f"({table['count']} files)")
    for e in table["files"]:
        print(f"  {e['sha256'][:16]}…  {e['path']}  ({e['bytes']:,} B)")


if __name__ == "__main__":
    main()
