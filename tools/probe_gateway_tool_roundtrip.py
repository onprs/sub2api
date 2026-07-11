#!/usr/bin/env python3
"""Probe tool-call round trips through Chat Completions and Anthropic Messages."""

from __future__ import annotations

import argparse
import json
import os
import time
import urllib.error
import urllib.request
from typing import Any


def post(url: str, key: str, payload: dict[str, Any]) -> tuple[int, dict[str, Any]]:
    req = urllib.request.Request(
        url,
        data=json.dumps(payload, separators=(",", ":")).encode(),
        headers={"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            status, raw = resp.status, resp.read()
    except urllib.error.HTTPError as exc:
        status, raw = exc.code, exc.read()
    try:
        return status, json.loads(raw)
    except Exception:
        return status, {"_raw": raw[:1000].decode("utf-8", "replace")}


def chat_roundtrip(base: str, key: str, model: str, prefix: str) -> dict[str, Any]:
    first = {
        "model": model,
        "messages": [{"role": "system", "content": prefix}, {"role": "user", "content": "Read demo.txt."}],
        "tools": [{"type": "function", "function": {"name": "read_file", "description": "Read a file", "parameters": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]}}}],
        "tool_choice": {"type": "function", "function": {"name": "read_file"}},
        "max_tokens": 256,
    }
    s1, b1 = post(base + "/v1/chat/completions", key, first)
    choices = b1.get("choices") or []
    message = choices[0].get("message", {}) if choices and isinstance(choices[0], dict) else {}
    calls = message.get("tool_calls") or []
    if s1 != 200 or not calls:
        return {"protocol": "chat_completions", "first_status": s1, "tool_call": False, "error": str(b1.get("error") or b1.get("_raw") or "missing tool call")[:500]}
    call = calls[0]
    second = dict(first)
    second["tool_choice"] = "auto"
    second["messages"] = first["messages"] + [message, {"role": "tool", "tool_call_id": call.get("id"), "content": "demo content"}]
    s2, b2 = post(base + "/v1/chat/completions", key, second)
    usage = b2.get("usage") if isinstance(b2.get("usage"), dict) else {}
    details = usage.get("prompt_tokens_details") if isinstance(usage.get("prompt_tokens_details"), dict) else {}
    return {
        "protocol": "chat_completions",
        "first_status": s1,
        "second_status": s2,
        "tool_call": True,
        "reasoning_first": bool(message.get("reasoning_content")),
        "cache_read_tokens": int(details.get("cached_tokens") or 0),
        "ok": s2 == 200 and bool((b2.get("choices") or [])),
        "error": "" if s2 == 200 else str(b2.get("error") or b2.get("_raw") or "")[:500],
    }


def anthropic_roundtrip(base: str, key: str, model: str, prefix: str) -> dict[str, Any]:
    first = {
        "model": model,
        "system": [{"type": "text", "text": prefix, "cache_control": {"type": "ephemeral", "ttl": "1h"}}],
        "messages": [{"role": "user", "content": "Read demo.txt."}],
        "tools": [{"name": "read_file", "description": "Read a file", "input_schema": {"type": "object", "properties": {"path": {"type": "string"}}, "required": ["path"]}}],
        "tool_choice": {"type": "tool", "name": "read_file"},
        "max_tokens": 2048,
        "thinking": {"type": "enabled", "budget_tokens": 1024},
        "output_config": {"effort": "low"},
        "metadata": {"user_id": f"tool-probe-{model}"},
    }
    s1, b1 = post(base + "/v1/messages", key, first)
    content = b1.get("content") or []
    calls = [item for item in content if isinstance(item, dict) and item.get("type") == "tool_use"]
    if s1 != 200 or not calls:
        return {"protocol": "anthropic_messages", "first_status": s1, "tool_call": False, "error": str(b1.get("error") or b1.get("_raw") or "missing tool call")[:500]}
    call = calls[0]
    second = dict(first)
    second["tool_choice"] = {"type": "auto"}
    second["messages"] = first["messages"] + [
        {"role": "assistant", "content": content},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": call.get("id"), "content": "demo content"}]},
    ]
    s2, b2 = post(base + "/v1/messages", key, second)
    usage = b2.get("usage") if isinstance(b2.get("usage"), dict) else {}
    return {
        "protocol": "anthropic_messages",
        "first_status": s1,
        "second_status": s2,
        "tool_call": True,
        "thinking_first": any(isinstance(item, dict) and item.get("type") in ("thinking", "redacted_thinking") for item in content),
        "cache_read_tokens": int(usage.get("cache_read_input_tokens") or 0),
        "ok": s2 == 200 and isinstance(b2.get("content"), list),
        "error": "" if s2 == 200 else str(b2.get("error") or b2.get("_raw") or "")[:500],
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://127.0.0.1:8080")
    parser.add_argument("--model", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    key = os.environ.get("SUB2API_PROBE_API_KEY", "").strip()
    if not key:
        raise SystemExit("SUB2API_PROBE_API_KEY is required")
    prefix = ("Tool round-trip cache prefix. Keep this stable. " * 180).strip()
    report = {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "model": args.model,
        "prefix_bytes": len(prefix.encode()),
        "results": [chat_roundtrip(args.base_url.rstrip("/"), key, args.model, prefix), anthropic_roundtrip(args.base_url.rstrip("/"), key, args.model, prefix)],
    }
    with open(args.output, "w", encoding="utf-8") as handle:
        json.dump(report, handle, ensure_ascii=True, indent=2)
        handle.write("\n")
    print(json.dumps(report["results"], separators=(",", ":")))
    return 0 if all(item.get("ok") for item in report["results"]) else 1


if __name__ == "__main__":
    raise SystemExit(main())
