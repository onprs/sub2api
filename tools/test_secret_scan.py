import unittest
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import secret_scan


class SecretScanTests(unittest.TestCase):
    def test_detects_high_confidence_api_key_assignment(self):
        text = 'OPENAI_API_KEY = "sk-proj-' + ("A" * 48) + '"\n'

        findings = secret_scan.scan_text("config.py", text)

        self.assertEqual(len(findings), 1)
        self.assertEqual(findings[0].kind, "openai_api_key")
        self.assertTrue(findings[0].masked.startswith("sk-proj-"))
        self.assertNotIn("A" * 32, findings[0].masked)

    def test_ignores_obvious_placeholders_and_short_test_keys(self):
        text = "\n".join(
            [
                'OPENAI_API_KEY = "sk-user-test-key"',
                'ANTHROPIC_AUTH_TOKEN = "your_anthropic_token_here"',
                'password = "changeme"',
            ]
        )

        findings = secret_scan.scan_text("example.env", text)

        self.assertEqual(findings, [])

    def test_line_allowlist_suppresses_fixture_secret(self):
        text = 'token = "ghp_' + ("B" * 40) + '"  # pragma: allowlist secret\n'

        findings = secret_scan.scan_text("fixture.py", text)

        self.assertEqual(findings, [])

    def test_ignores_urls_that_contain_key_words(self):
        text = "apiKey: 'https://aistudio.google.com/app/apikey'\n"

        findings = secret_scan.scan_text("component.vue", text)

        self.assertEqual(findings, [])

    def test_ignores_private_key_fixture_in_tests(self):
        text = '"-----BEGIN PRIVATE KEY-----\\nabc\\n-----END PRIVATE KEY-----\\n"\n'

        findings = secret_scan.scan_text("service_account_test.go", text)

        self.assertEqual(findings, [])


if __name__ == "__main__":
    unittest.main()
