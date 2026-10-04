#!/usr/bin/env python3
"""检查 ZCode 上游协议变化；不覆盖本地实现或自动修改固定版本。"""
from __future__ import annotations
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
MANIFEST = ROOT / "backend/internal/pkg/zcode/upstream.json"
REMOTE = "https://github.com/TriDefender/zcode-api.git"


def command(args: list[str], cwd: Path | None = None, optional: bool = False) -> str:
    kwargs = {}
    if os.name == "nt":
        kwargs["creationflags"] = subprocess.CREATE_NO_WINDOW
        info = subprocess.STARTUPINFO()
        info.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        info.wShowWindow = subprocess.SW_HIDE
        kwargs["startupinfo"] = info
    proc = subprocess.run(args, cwd=cwd, text=True, encoding="utf-8", errors="replace", capture_output=True, timeout=90, **kwargs)
    if proc.returncode and not optional:
        # 不转印 stderr，避免用户本地 remote/proxy 错误泄露凭据。
        raise RuntimeError(f"命令失败 exit={proc.returncode}: {args[0]} {args[1] if len(args)>1 else ''}")
    return proc.stdout.strip() if proc.returncode == 0 else ""


def file_hash(source: Path, target: str, path: str) -> str:
    # git blob 的字节哈希，不通过文本解码或换行转换。
    opts = {}
    if os.name == "nt":
        opts["creationflags"] = subprocess.CREATE_NO_WINDOW
        info = subprocess.STARTUPINFO()
        info.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        info.wShowWindow = subprocess.SW_HIDE
        opts["startupinfo"] = info
    proc = subprocess.run(["git", "show", f"{target}:{path}"], cwd=source, capture_output=True, timeout=30, **opts)
    if proc.returncode:
        raise ValueError(f"映射文件在目标版本中不存在，需要先更新 source mapping: {path}")
    return hashlib.sha256(proc.stdout).hexdigest()


def validate_ref(ref: str) -> str:
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._/-]{0,199}", ref) or ".." in ref:
        raise ValueError("ref 必须为 commit、tag 或安全的分支名")
    return ref


def parse_changes(raw: str) -> list[dict[str, str]]:
    fields = raw.split("\0")
    result = []
    index = 0
    while index < len(fields) and fields[index]:
        status = fields[index]
        index += 1
        path = fields[index]
        index += 1
        entry = {"status": status, "path": path}
        if status.startswith(("R", "C")):
            entry["old_path"] = path
            entry["path"] = fields[index]
            index += 1
        result.append(entry)
    return result


def make_report(manifest: dict, target: str, changes: list[dict], tag: str = "") -> dict:
    categories = {}
    impacted = set()
    relevant = []
    mapped = {source for item in manifest["source_mapping"] for source in item["upstream_files"]}
    for item in manifest["source_mapping"]:
        paths = set(item["upstream_files"])
        selected = [entry for entry in changes if entry["path"] in paths or entry.get("old_path") in paths]
        categories[item["category"]] = {"status": "changed" if selected else "unchanged", "changes": selected, "local_modules": item["local_files"]}
        if selected:
            impacted.update(item["local_files"])
            relevant.extend(selected)
    unmapped = [entry for entry in changes if (entry["path"].startswith(("src/auth/", "src/proxy/", "src/claim/", "src/config/", "src/server/routes-quota")) or entry["path"] in ("package.json", "LICENSE", "LICENSE.md", "README.md")) and entry["path"] not in mapped and entry.get("old_path") not in mapped]
    return {"upstream_repo": manifest["upstream_repo"], "pinned_commit": manifest["pinned_commit"], "target_commit": target, "target_tag": tag or None, "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(), "review_required": bool(relevant or unmapped), "categories": categories, "unmapped_changes": unmapped, "affected_local_files": sorted(impacted), "all_changes": changes}


def text_report(report: dict) -> str:
    lines = [f"上游: {report['upstream_repo']}", f"Pinned: {report['pinned_commit']}", f"Target: {report['target_commit']} ({report['target_tag'] or '无 tag'})"]
    for name, info in report["categories"].items():
        lines.append(f"{name}: {info['status']}")
        for entry in info["changes"]:
            lines.append(f"  {entry['status']} {entry.get('old_path', '')} {'→ ' if 'old_path' in entry else ''}{entry['path']}")
        if info["changes"]:
            lines.extend(f"  review → {path}" for path in info["local_modules"])
    if report["unmapped_changes"]:
        lines.append("未映射的相关变化（必须补充 review）:")
        lines.extend(f"  {entry['status']} {entry['path']}" for entry in report["unmapped_changes"])
    lines.append("需要 review" if report["review_required"] else "已映射协议文件没有变化")
    return "\n".join(lines) + "\n"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ref", default="HEAD")
    parser.add_argument("--source", type=Path, help="使用已有上游源码；不联网 fetch")
    parser.add_argument("--cache", type=Path, default=ROOT / "artifacts/zcode-upstream-cache")
    parser.add_argument("--output", type=Path, default=ROOT / "artifacts/zcode-upstream-report.json")
    parser.add_argument("--update-pin", action="store_true", help="显式记录已 review 的版本；不覆盖任何 Go/前端代码")
    parser.add_argument("--reviewed", action="store_true", help="确认已完成源码 review 和必要测试")
    args = parser.parse_args(argv)
    target_ref = validate_ref(args.ref)
    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    validate_ref(manifest["pinned_commit"])
    if args.update_pin and not args.reviewed:
        parser.error("--update-pin 必须同时指定 --reviewed；先完成文档规定的 review 和测试")
    source = args.source or args.cache
    if not args.source:
        if not (source / ".git").exists():
            source.parent.mkdir(parents=True, exist_ok=True)
            command(["git", "clone", "--filter=blob:none", REMOTE, str(source)])
        if command(["git", "remote", "get-url", "origin"], source) != REMOTE:
            raise ValueError("cache 的 origin 与固定上游仓库不一致")
        command(["git", "fetch", "--tags", "origin", target_ref], source)
        target_ref = command(["git", "rev-parse", "--verify", "FETCH_HEAD^{commit}"], source)
        if not command(["git", "rev-parse", "--verify", manifest["pinned_commit"] + "^{commit}"], source, optional=True):
            command(["git", "fetch", "origin", manifest["pinned_commit"]], source)
    target = command(["git", "rev-parse", "--verify", target_ref + "^{commit}"], source)
    if not re.fullmatch("[0-9a-f]{40}", target):
        raise ValueError("目标 commit 无效")
    # -z 保留带空格的路径，重命名同时检查旧路径和新路径。
    raw = command(["git", "diff", "--name-status", "-z", "--find-renames", manifest["pinned_commit"], target, "--"], source)
    report = make_report(manifest, target, parse_changes(raw), command(["git", "describe", "--tags", "--exact-match", target], source, optional=True))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(text_report(report), end="")
    print(f"报告: {args.output.resolve()}")
    if args.update_pin:
        manifest["pinned_commit"] = target
        manifest["pinned_tag"] = report["target_tag"]
        manifest["synced_at"] = dt.datetime.now(dt.timezone.utc).date().isoformat()
        manifest["upstream_file_sha256"] = {path: file_hash(source, target, path) for item in manifest["source_mapping"] for path in item["upstream_files"]}
        MANIFEST.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return 1 if report["review_required"] else 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (RuntimeError, ValueError, OSError, subprocess.TimeoutExpired) as exc:
        print(f"检查失败: {exc}", file=sys.stderr)
        raise SystemExit(2)
