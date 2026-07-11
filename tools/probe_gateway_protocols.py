#!/usr/bin/env python3
"""Probe gateway protocol conversion and prompt-cache observability.

The API key is read only from SUB2API_PROBE_API_KEY. Reports never include it.
"""

from __future__ import annotations

import argparse
import json
import os
import time
import urllib.error
import urllib.request
from dataclasses import asdict, dataclass
from typing import Any


@dataclass
class ProbeResult:
    model: str
    protocol: str
    attempt: int
    http_status: int
    ok: bool
    latency_ms: int
    cache_read_tokens: int
    input_tokens: int
    output_tokens: int
    reasoning_observed: bool
    response_id: str
    error: str


def request_json(url: str, api_key: str, payload: dict[str, Any] | None = None) -> tuple[int, dict[str, Any], int]:
    headers = {"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"}
    data = None if payload is None else json.dumps(payload, separators=(",", ":")).encode()
    req = urllib.request.Request(url, data=data, headers=headers, method="GET" if payload is None else "POST")
    started = time.monotonic()
    try:
        with urllib.request.urlopen(req, timeout=180) as resp:
            raw = resp.read()
            status = resp.status
    except urllib.error.HTTPError as exc:
        raw = exc.read()
        status = exc.code
    latency_ms = round((time.monotonic() - started) * 1000)
    try:
        body = json.loads(raw)
    except (json.JSONDecodeError, UnicodeDecodeError):
        body = {"_raw": raw[:1000].decode("utf-8", "replace")}
    return status, body, latency_ms


def error_message(body: dict[str, Any]) -> str:
    err = body.get("error")
    if isinstance(err, dict):
        value = err.get("message") or err.get("type") or err.get("code")
        if value:
            return str(value)[:500]
    if isinstance(err, str):
        return err[:500]
    detail = body.get("detail") or body.get("message") or body.get("_raw")
    return str(detail or "")[:500]


def usage_for(protocol: str, body: dict[str, Any]) -> tuple[int, int, int]:
    usage = body.get("usage") if isinstance(body.get("usage"), dict) else {}
    if protocol == "gemini":
        usage = body.get("usageMetadata") if isinstance(body.get("usageMetadata"), dict) else {}
        return (
            int(usage.get("cachedContentTokenCount") or 0),
            int(usage.get("promptTokenCount") or 0),
            int(usage.get("candidatesTokenCount") or 0) + int(usage.get("thoughtsTokenCount") or 0),
        )
    if protocol == "responses":
        details = usage.get("input_tokens_details") if isinstance(usage.get("input_tokens_details"), dict) else {}
        return int(details.get("cached_tokens") or 0), int(usage.get("input_tokens") or 0), int(usage.get("output_tokens") or 0)
    if protocol == "chat_completions":
        details = usage.get("prompt_tokens_details") if isinstance(usage.get("prompt_tokens_details"), dict) else {}
        return int(details.get("cached_tokens") or 0), int(usage.get("prompt_tokens") or 0), int(usage.get("completion_tokens") or 0)
    return int(usage.get("cache_read_input_tokens") or 0), int(usage.get("input_tokens") or 0), int(usage.get("output_tokens") or 0)


def reasoning_for(protocol: str, body: dict[str, Any]) -> bool:
    if protocol == "gemini":
        candidates = body.get("candidates") or []
        for candidate in candidates:
            content = candidate.get("content", {}) if isinstance(candidate, dict) else {}
            for part in content.get("parts", []):
                if isinstance(part, dict) and part.get("thought"):
                    return True
        return False
    if protocol == "responses":
        return any(isinstance(item, dict) and item.get("type") == "reasoning" for item in body.get("output", []))
    if protocol == "chat_completions":
        choices = body.get("choices") or []
        message = choices[0].get("message", {}) if choices and isinstance(choices[0], dict) else {}
        return bool(message.get("reasoning_content"))
    return any(isinstance(item, dict) and item.get("type") in ("thinking", "redacted_thinking") for item in body.get("content", []))


def response_id_for(protocol: str, body: dict[str, Any]) -> str:
    value = body.get("id")
    if value:
        return str(value)[:160]
    return ""


def payload_for(protocol: str, model: str, prefix: str, cache_key: str) -> dict[str, Any]:
    user_text = "Return exactly OK."
    if protocol == "gemini":
        return {
            "systemInstruction": {"parts": [{"text": prefix}]},
            "contents": [{"role": "user", "parts": [{"text": user_text}]}],
            "generationConfig": {
                "maxOutputTokens": 128,
                "thinkingConfig": {"includeThoughts": True, "thinkingBudget": 64},
            },
        }
    if protocol == "responses":
        return {
            "model": model,
            "instructions": prefix,
            "input": [{"role": "user", "content": [{"type": "input_text", "text": user_text}]}],
            "max_output_tokens": 128,
            "reasoning": {"effort": "low", "summary": "auto"},
            "prompt_cache_key": cache_key,
            "store": False,
        }
    if protocol == "chat_completions":
        return {
            "model": model,
            "messages": [
                {"role": "system", "content": prefix},
                {"role": "user", "content": user_text},
            ],
            "max_completion_tokens": 128,
            "reasoning_effort": "low",
            "stream": False,
        }
    return {
        "model": model,
        "system": [{"type": "text", "text": prefix, "cache_control": {"type": "ephemeral", "ttl": "1h"}}],
        "messages": [{"role": "user", "content": user_text}],
        "max_tokens": 2048,
        "stream": False,
        "thinking": {"type": "enabled", "budget_tokens": 1024},
        "output_config": {"effort": "low"},
        "metadata": {"user_id": cache_key},
    }


def classify_models(model_ids: list[str]) -> tuple[list[str], dict[str, str]]:
    text: list[str] = []
    skipped: dict[str, str] = {}
    for model in sorted(set(model_ids)):
        lower = model.lower()
        if "image" in lower:
            skipped[model] = "image endpoint model"
        elif "audio" in lower:
            skipped[model] = "audio endpoint model"
        elif "realtime" in lower:
            skipped[model] = "realtime endpoint model"
        elif lower == "codex-auto-review":
            skipped[model] = "internal routing model"
        else:
            text.append(model)
    return text, skipped


def continuation_payload(protocol: str, first_payload: dict[str, Any], first_body: dict[str, Any]) -> dict[str, Any]:
    payload = json.loads(json.dumps(first_payload))
    next_text = "In one word, confirm that you remember the previous request."
    if protocol == "responses":
        output = first_body.get("output")
        if not isinstance(output, list) or not output:
            raise ValueError("first Responses result has no output history")
        payload["input"] = [
            *payload["input"],
            *output,
            {"role": "user", "content": [{"type": "input_text", "text": next_text}]},
        ]
        return payload
    if protocol == "chat_completions":
        choices = first_body.get("choices") or []
        message = choices[0].get("message") if choices and isinstance(choices[0], dict) else None
        if not isinstance(message, dict):
            raise ValueError("first Chat Completions result has no assistant message")
        payload["messages"] = [*payload["messages"], message, {"role": "user", "content": next_text}]
        return payload
    if protocol == "anthropic_messages":
        content = first_body.get("content")
        if not isinstance(content, list) or not content:
            raise ValueError("first Messages result has no assistant content")
        payload["messages"] = [
            *payload["messages"],
            {"role": "assistant", "content": content},
            {"role": "user", "content": next_text},
        ]
        return payload
    candidates = first_body.get("candidates") or []
    content = candidates[0].get("content") if candidates and isinstance(candidates[0], dict) else None
    if not isinstance(content, dict):
        raise ValueError("first Gemini result has no model content")
    payload["contents"] = [
        *payload["contents"],
        content,
        {"role": "user", "parts": [{"text": next_text}]},
    ]
    return payload


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://127.0.0.1:8080")
    parser.add_argument("--models", default="", help="comma-separated override")
    parser.add_argument(
        "--protocols",
        default="responses,chat_completions,anthropic_messages",
        help="comma-separated subset: responses,chat_completions,anthropic_messages,gemini",
    )
    parser.add_argument("--output", required=True)
    parser.add_argument("--pause", type=float, default=0.25)
    args = parser.parse_args()

    api_key = os.environ.get("SUB2API_PROBE_API_KEY", "").strip()
    if not api_key:
        raise SystemExit("SUB2API_PROBE_API_KEY is required")
    base_url = args.base_url.rstrip("/")

    listed_status, listed_body, _ = request_json(f"{base_url}/v1/models", api_key)
    if listed_status != 200:
        raise SystemExit(f"models request failed: HTTP {listed_status}: {error_message(listed_body)}")
    listed = [str(item.get("id")) for item in listed_body.get("data", []) if isinstance(item, dict) and item.get("id")]
    if args.models.strip():
        models = [item.strip() for item in args.models.split(",") if item.strip()]
        skipped: dict[str, str] = {}
    else:
        models, skipped = classify_models(listed)

    phrase = "Protocol cache stability marker. Keep this prefix byte-for-byte unchanged. "
    prefix = (phrase * 180).strip()
    results: list[ProbeResult] = []
    available_protocols = {
        "responses": "/v1/responses",
        "chat_completions": "/v1/chat/completions",
        "anthropic_messages": "/v1/messages",
        "gemini": "/v1beta/models/{model}:generateContent",
    }
    requested_protocols = [item.strip() for item in args.protocols.split(",") if item.strip()]
    unknown_protocols = sorted(set(requested_protocols) - set(available_protocols))
    if unknown_protocols:
        raise SystemExit(f"unknown protocols: {','.join(unknown_protocols)}")
    protocols = {name: available_protocols[name] for name in requested_protocols}

    for model in models:
        for protocol, path in protocols.items():
            cache_key = f"sub2api-probe-{model}-{protocol}-v1"
            payload = payload_for(protocol, model, prefix, cache_key)
            endpoint = path.format(model=model)
            first_body: dict[str, Any] | None = None
            for attempt in (1, 2):
                if attempt == 2:
                    if first_body is None:
                        break
                    try:
                        payload = continuation_payload(protocol, payload, first_body)
                    except ValueError as exc:
                        results.append(ProbeResult(
                            model=model, protocol=protocol, attempt=attempt, http_status=0, ok=False,
                            latency_ms=0, cache_read_tokens=0, input_tokens=0, output_tokens=0,
                            reasoning_observed=False, response_id="", error=str(exc),
                        ))
                        break
                status, body, latency_ms = request_json(f"{base_url}{endpoint}", api_key, payload)
                cache_read, input_tokens, output_tokens = usage_for(protocol, body)
                ok = 200 <= status < 300 and not body.get("error")
                results.append(ProbeResult(
                    model=model,
                    protocol=protocol,
                    attempt=attempt,
                    http_status=status,
                    ok=ok,
                    latency_ms=latency_ms,
                    cache_read_tokens=cache_read,
                    input_tokens=input_tokens,
                    output_tokens=output_tokens,
                    reasoning_observed=reasoning_for(protocol, body),
                    response_id=response_id_for(protocol, body),
                    error="" if ok else error_message(body),
                ))
                if attempt == 1:
                    first_body = body if ok else None
                time.sleep(args.pause)

    report = {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "base_url": base_url,
        "listed_models": listed,
        "tested_models": models,
        "tested_protocols": list(protocols),
        "skipped_models": skipped,
        "stable_prefix_bytes": len(prefix.encode()),
        "second_attempt_mode": "explicit_assistant_history_plus_new_user_turn",
        "results": [asdict(item) for item in results],
    }
    with open(args.output, "w", encoding="utf-8") as handle:
        json.dump(report, handle, ensure_ascii=True, indent=2)
        handle.write("\n")

    failures = sum(1 for item in results if not item.ok)
    second_attempts = [item for item in results if item.attempt == 2]
    cache_hits = sum(1 for item in second_attempts if item.cache_read_tokens > 0)
    print(json.dumps({
        "tested_models": len(models),
        "requests": len(results),
        "failures": failures,
        "second_attempts": len(second_attempts),
        "cache_hits": cache_hits,
        "report": args.output,
    }, separators=(",", ":")))
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
