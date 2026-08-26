#!/usr/bin/env python3
import argparse
import hashlib
import sys
from dataclasses import dataclass
from pathlib import Path


PROFILE_MARKERS = {
    "subscription-rolling-quotas": [
        "five_hour_limit_usd",
        "seven_day_limit_usd",
        "thirty_day_limit_usd",
        "subscription_quota_snapshot_version",
        "user_group_plan_unique_active",
    ],
    "onprs-subquota": [
        "five_hour_limit_usd",
        "seven_day_limit_usd",
        "thirty_day_limit_usd",
        "subscription_quota_snapshot_version",
        "user_group_plan_unique_active",
        "renewal_discount_percent",
        "subscription_renewal_discount_percent",
        "lmspeed.net/provider/api-onprs-top",
        "api/provider/claim-badge/1420",
        "opencode_go",
        "https://opencode.ai/zen/go/v1",
        "https://opencode.ai/docs/go/",
        "channel_monitor_provider_opencode_go",
        "clinepass",
        "https://api.cline.bot/api/v1",
        "channel_monitor_provider_clinepass",
        "openrouter",
        "https://openrouter.ai/api/v1",
        "channel_monitor_provider_openrouter",
        "commandcode",
        "https://api.commandcode.ai",
        "channel_monitor_provider_commandcode",
    ],
}


@dataclass
class VerificationResult:
    path: Path
    sha256: str
    marker_counts: dict[str, int]
    errors: list[str]


def count_marker(payload: bytes, marker: str) -> int:
    return payload.count(marker.encode("utf-8"))


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def normalize_sha(value: str | None) -> str | None:
    if value is None:
        return None
    return value.strip().lower()


def required_markers(profile: str | None, extra_markers: list[str]) -> list[str]:
    markers: list[str] = []
    if profile:
        markers.extend(PROFILE_MARKERS[profile])
    markers.extend(extra_markers)

    unique = []
    seen = set()
    for marker in markers:
        marker = marker.strip()
        if not marker or marker in seen:
            continue
        unique.append(marker)
        seen.add(marker)
    return unique


def verify_binary(
    path: Path,
    profile: str | None,
    extra_markers: list[str],
    expected_sha256: str | None,
    expected_strings: list[str],
) -> VerificationResult:
    errors: list[str] = []
    if not path.is_file():
        return VerificationResult(path, "", {}, [f"Binary not found: {path}"])

    payload = path.read_bytes()
    actual_sha = sha256_file(path)
    expected_sha = normalize_sha(expected_sha256)
    if expected_sha and actual_sha != expected_sha:
        errors.append(f"SHA256 mismatch: expected {expected_sha}, got {actual_sha}")

    markers = required_markers(profile, extra_markers + expected_strings)
    marker_counts = {marker: count_marker(payload, marker) for marker in markers}
    for marker, count in marker_counts.items():
        if count <= 0:
            errors.append(f"Required marker missing: {marker}")

    return VerificationResult(path, actual_sha, marker_counts, errors)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Verify a Sub2API release binary before replacing a live service."
    )
    parser.add_argument("binary", type=Path)
    parser.add_argument(
        "--profile",
        choices=sorted(PROFILE_MARKERS),
        help="Predefined marker set to require.",
    )
    parser.add_argument(
        "--marker",
        action="append",
        default=[],
        help="Additional marker string that must appear in the binary.",
    )
    parser.add_argument("--expected-sha256", help="Expected SHA256 for the binary.")
    parser.add_argument(
        "--expect-string",
        action="append",
        default=[],
        help="Expected version or release identifier string that must appear.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    result = verify_binary(
        args.binary,
        profile=args.profile,
        extra_markers=args.marker,
        expected_sha256=args.expected_sha256,
        expected_strings=args.expect_string,
    )

    print(f"path={result.path}")
    print(f"sha256={result.sha256}")
    for marker, count in result.marker_counts.items():
        print(f"marker[{marker}]={count}")

    if result.errors:
        for error in result.errors:
            print(f"ERROR: {error}", file=sys.stderr)
        return 1

    print("release_binary_ok=true")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
