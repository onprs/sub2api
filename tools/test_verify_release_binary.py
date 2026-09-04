import hashlib
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import verify_release_binary


class VerifyReleaseBinaryTest(unittest.TestCase):
    def write_binary(self, payload: bytes) -> Path:
        tmp = tempfile.NamedTemporaryFile(delete=False)
        tmp.write(payload)
        tmp.close()
        self.addCleanup(lambda: Path(tmp.name).unlink(missing_ok=True))
        return Path(tmp.name)

    def test_onprs_profile_accepts_expected_release_markers(self):
        payload = b"\0".join(
            marker.encode("utf-8")
            for marker in [
                "0.1.131-subquota-lmspeed-08e09b7",
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
            ]
        )
        binary = self.write_binary(payload)
        expected_sha = hashlib.sha256(payload).hexdigest()

        result = verify_release_binary.verify_binary(
            binary,
            profile="onprs-subquota",
            extra_markers=[],
            expected_sha256=expected_sha,
            expected_strings=["0.1.131-subquota-lmspeed-08e09b7"],
        )

        self.assertEqual([], result.errors)
        self.assertEqual(expected_sha, result.sha256)

    def test_subscription_profile_rejects_missing_quota_marker(self):
        binary = self.write_binary(
            b"five_hour_limit_usd seven_day_limit_usd user_group_plan_unique_active"
        )

        result = verify_release_binary.verify_binary(
            binary,
            profile="subscription-rolling-quotas",
            extra_markers=[],
            expected_sha256=None,
            expected_strings=[],
        )

        self.assertTrue(
            any("thirty_day_limit_usd" in error for error in result.errors),
            result.errors,
        )

    def test_onprs_profile_rejects_missing_lmspeed_marker(self):
        binary = self.write_binary(
            b"five_hour_limit_usd seven_day_limit_usd thirty_day_limit_usd "
            b"subscription_quota_snapshot_version user_group_plan_unique_active "
            b"renewal_discount_percent subscription_renewal_discount_percent"
        )

        result = verify_release_binary.verify_binary(
            binary,
            profile="onprs-subquota",
            extra_markers=[],
            expected_sha256=None,
            expected_strings=[],
        )

        self.assertTrue(
            any("lmspeed.net/provider/api-onprs-top" in error for error in result.errors),
            result.errors,
        )

    def test_onprs_profile_rejects_missing_opencode_go_markers(self):
        binary = self.write_binary(
            b"five_hour_limit_usd seven_day_limit_usd thirty_day_limit_usd "
            b"subscription_quota_snapshot_version user_group_plan_unique_active "
            b"renewal_discount_percent subscription_renewal_discount_percent "
            b"lmspeed.net/provider/api-onprs-top api/provider/claim-badge/1420"
        )

        result = verify_release_binary.verify_binary(
            binary,
            profile="onprs-subquota",
            extra_markers=[],
            expected_sha256=None,
            expected_strings=[],
        )

        self.assertTrue(any("opencode_go" in error for error in result.errors), result.errors)
        self.assertTrue(
            any("https://opencode.ai/zen/go/v1" in error for error in result.errors),
            result.errors,
        )
        self.assertTrue(
            any("https://opencode.ai/docs/go/" in error for error in result.errors),
            result.errors,
        )
        self.assertTrue(
            any("channel_monitor_provider_opencode_go" in error for error in result.errors),
            result.errors,
        )

    def test_rejects_sha_mismatch(self):
        binary = self.write_binary(b"five_hour_limit_usd")

        result = verify_release_binary.verify_binary(
            binary,
            profile=None,
            extra_markers=[],
            expected_sha256="0" * 64,
            expected_strings=[],
        )

        self.assertTrue(any("SHA256 mismatch" in error for error in result.errors), result.errors)


if __name__ == "__main__":
    unittest.main()
