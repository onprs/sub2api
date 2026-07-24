#!/usr/bin/env python3
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check_pnpm_audit_exceptions.py")


class CheckPnpmAuditExceptionsTests(unittest.TestCase):
    def run_checker(
        self,
        audit,
        *,
        audit_exit_code=0,
        exceptions=None,
        validate_only=False,
    ):
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_path = Path(temp_dir)
            audit_path = temp_path / "audit.json"
            audit_path.write_text(json.dumps(audit), encoding="utf-8")

            command = [
                sys.executable,
                str(SCRIPT),
                "--audit",
                str(audit_path),
                "--audit-exit-code",
                str(audit_exit_code),
            ]
            if validate_only:
                command.append("--validate-only")
            else:
                exceptions_path = temp_path / "exceptions.yml"
                exceptions_path.write_text(exceptions or "version: 1\nexceptions:\n", encoding="utf-8")
                command.extend(["--exceptions", str(exceptions_path)])

            return subprocess.run(command, text=True, capture_output=True, check=False)

    def test_registry_error_json_fails_closed(self):
        result = self.run_checker(
            {"error": {"code": "EAI_AGAIN", "summary": "registry unavailable"}},
            audit_exit_code=1,
            validate_only=True,
        )

        self.assertEqual(result.returncode, 1)
        self.assertIn("pnpm audit reported an error", result.stderr)

    def test_empty_json_fails_closed(self):
        result = self.run_checker({}, validate_only=True)

        self.assertEqual(result.returncode, 1)
        self.assertIn("missing advisories/vulnerabilities", result.stderr)

    def test_high_vulnerability_without_stable_advisory_id_fails(self):
        result = self.run_checker(
            {
                "advisories": {
                    "legacy-key": {
                        "module_name": "unsafe-package",
                        "severity": "high",
                        "title": "Title is not a stable advisory identifier",
                    }
                }
            },
            audit_exit_code=1,
            validate_only=True,
        )

        self.assertEqual(result.returncode, 1)
        self.assertIn("missing advisory id", result.stderr)

    def test_vulnerabilities_shape_without_stable_advisory_id_fails(self):
        result = self.run_checker(
            {
                "vulnerabilities": {
                    "unsafe-package": {
                        "severity": "critical",
                        "via": [{"title": "No stable identifier"}],
                    }
                }
            },
            audit_exit_code=1,
            validate_only=True,
        )

        self.assertEqual(result.returncode, 1)
        self.assertIn("missing advisory id", result.stderr)

    def test_nonzero_exit_without_high_vulnerability_fails(self):
        result = self.run_checker(
            {"advisories": {}},
            audit_exit_code=1,
            validate_only=True,
        )

        self.assertEqual(result.returncode, 1)
        self.assertIn("without reporting a high/critical vulnerability", result.stderr)

    def test_valid_exception_allows_audit_exit_one(self):
        result = self.run_checker(
            {
                "advisories": {
                    "1001": {
                        "module_name": "known-package",
                        "severity": "high",
                        "github_advisory_id": "GHSA-aaaa-bbbb-cccc",
                        "title": "Known issue",
                    }
                }
            },
            audit_exit_code=1,
            exceptions="""version: 1
exceptions:
  - package: known-package
    advisory: GHSA-aaaa-bbbb-cccc
    severity: high
    mitigation: not reachable from untrusted input
    expires_on: 2099-12-31
""",
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Audit exceptions validated", result.stdout)

    def test_vulnerabilities_url_matches_ghsa_exception(self):
        result = self.run_checker(
            {
                "vulnerabilities": {
                    "known-package": {
                        "severity": "high",
                        "via": [
                            {
                                "url": "https://github.com/advisories/GHSA-aaaa-bbbb-cccc",
                                "title": "Known issue",
                            }
                        ],
                    }
                }
            },
            audit_exit_code=1,
            exceptions="""version: 1
exceptions:
  - package: known-package
    advisory: GHSA-aaaa-bbbb-cccc
    severity: high
    mitigation: not reachable from untrusted input
    expires_on: 2099-12-31
""",
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Audit exceptions validated", result.stdout)

    def test_empty_valid_report_with_zero_exit_passes_validation(self):
        result = self.run_checker(
            {"vulnerabilities": {}},
            audit_exit_code=0,
            validate_only=True,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Audit response validated", result.stdout)


if __name__ == "__main__":
    unittest.main()
