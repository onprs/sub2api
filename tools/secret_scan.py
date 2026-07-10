#!/usr/bin/env python3
"""Lightweight high-confidence secret scan for release checks.

The scanner intentionally favors low false positives over broad heuristics. It
checks source-controlled and untracked working-tree files, skips generated or
dependency directories, and prints only masked secret values.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import argparse
import os
import re
import subprocess
import sys
from typing import Iterable


ALLOWLIST_MARKERS = (
    "allowlist secret",
    "allow-list secret",
    "pragma: allowlist",
    "nosec",
    "gitleaks:allow",
)

PLACEHOLDER_WORDS = (
    "changeme",
    "change-this",
    "change_this",
    "dummy",
    "example",
    "fake",
    "placeholder",
    "sample",
    "test",
    "your_",
    "your-",
    "<",
    ">",
)

SKIP_DIRS = {
    ".git",
    ".pytest_cache",
    ".vite",
    "__pycache__",
    "artifacts",
    "bin",
    "build",
    "coverage",
    "dist",
    "node_modules",
    "release",
}

SKIP_PATH_PARTS = {
    ("backend", "internal", "web", "dist"),
    ("frontend", "node_modules"),
    ("tools", "__pycache__"),
}

TEXT_EXTENSIONS = {
    ".bat",
    ".cjs",
    ".conf",
    ".css",
    ".csv",
    ".env",
    ".go",
    ".html",
    ".js",
    ".json",
    ".jsonc",
    ".md",
    ".mjs",
    ".ps1",
    ".py",
    ".sh",
    ".sql",
    ".toml",
    ".ts",
    ".tsx",
    ".vue",
    ".yaml",
    ".yml",
}

PATTERNS: tuple[tuple[str, re.Pattern[str]], ...] = (
    ("private_key", re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----")),
    ("aws_access_key", re.compile(r"\bA(?:KIA|SIA)[0-9A-Z]{16}\b")),
    ("openai_api_key", re.compile(r"\bsk-(?:proj-|live-)?[A-Za-z0-9_-]{32,}\b")),
    ("anthropic_api_key", re.compile(r"\bsk-ant-[A-Za-z0-9_-]{32,}\b")),
    ("google_api_key", re.compile(r"\bAIza[0-9A-Za-z_-]{35}\b")),
    ("github_token", re.compile(r"\bgh[pousr]_[A-Za-z0-9_]{36,}\b")),
    ("slack_token", re.compile(r"\bxox[baprs]-[A-Za-z0-9-]{20,}\b")),
    (
        "generic_secret_assignment",
        re.compile(
            r"(?i)\b(?:api[_-]?key|auth[_-]?token|secret|token|password)\b"
            r"\s*[:=]\s*['\"]([A-Za-z0-9_./+=:-]{32,})['\"]"
        ),
    ),
)


@dataclass(frozen=True)
class Finding:
    path: str
    line: int
    kind: str
    masked: str


def mask_secret(value: str) -> str:
    if len(value) <= 12:
        return "*" * len(value)
    return f"{value[:8]}...{value[-4:]}"


def is_allowlisted(line: str) -> bool:
    lower = line.lower()
    return any(marker in lower for marker in ALLOWLIST_MARKERS)


def is_placeholder(value: str) -> bool:
    lower = value.lower()
    if any(word in lower for word in PLACEHOLDER_WORDS):
        return True
    unique = set(value)
    return len(value) >= 24 and len(unique) <= 3


def extract_secret(kind: str, match: re.Match[str]) -> str:
    if kind == "generic_secret_assignment":
        return match.group(1)
    return match.group(0)


def is_test_or_fixture_path(path: str) -> bool:
    normalized = path.replace("\\", "/").lower()
    name = normalized.rsplit("/", 1)[-1]
    return (
        "/__tests__/" in normalized
        or "/testdata/" in normalized
        or "fixture" in normalized
        or name.startswith("test_")
        or name.endswith("_test.go")
        or name.endswith("_test.py")
        or ".spec." in name
    )


def scan_text(path: str, text: str) -> list[Finding]:
    findings: list[Finding] = []
    for lineno, line in enumerate(text.splitlines(), start=1):
        if is_allowlisted(line):
            continue
        for kind, pattern in PATTERNS:
            for match in pattern.finditer(line):
                secret = extract_secret(kind, match)
                if kind == "private_key" and is_test_or_fixture_path(path):
                    continue
                if kind == "generic_secret_assignment" and re.match(r"(?i)^https?://", secret):
                    continue
                if is_placeholder(secret):
                    continue
                findings.append(Finding(path=path, line=lineno, kind=kind, masked=mask_secret(secret)))
    return findings


def git_files(root: Path) -> list[Path]:
    try:
        result = subprocess.run(
            ["git", "ls-files", "-co", "--exclude-standard"],
            cwd=root,
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
    except (OSError, subprocess.CalledProcessError):
        return [path for path in root.rglob("*") if path.is_file()]

    return [root / line for line in result.stdout.splitlines() if line.strip()]


def should_skip(path: Path, root: Path) -> bool:
    try:
        rel = path.relative_to(root)
    except ValueError:
        return True
    parts = rel.parts
    if any(part in SKIP_DIRS for part in parts[:-1]):
        return True
    lower_parts = tuple(part.lower() for part in parts)
    for skip in SKIP_PATH_PARTS:
        if lower_parts[: len(skip)] == skip or skip == lower_parts[-len(skip) :]:
            return True
    if path.suffix.lower() not in TEXT_EXTENSIONS and path.name not in {"Dockerfile", "Makefile"}:
        return True
    try:
        if path.stat().st_size > 2_000_000:
            return True
    except OSError:
        return True
    return False


def scan_files(root: Path, files: Iterable[Path]) -> list[Finding]:
    findings: list[Finding] = []
    for path in files:
        if should_skip(path, root):
            continue
        try:
            data = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            try:
                data = path.read_text(encoding="utf-8-sig")
            except UnicodeDecodeError:
                continue
        except OSError:
            continue
        rel = os.fspath(path.relative_to(root)).replace("\\", "/")
        findings.extend(scan_text(rel, data))
    return findings


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Scan working tree for high-confidence secrets")
    parser.add_argument("--root", default=".", help="repository root to scan")
    args = parser.parse_args(argv)

    root = Path(args.root).resolve()
    findings = scan_files(root, git_files(root))
    if findings:
        print("secret_scan_failed=true")
        for finding in findings:
            print(f"{finding.path}:{finding.line}: {finding.kind}: {finding.masked}")
        return 1

    print("secret_scan_ok=true")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
