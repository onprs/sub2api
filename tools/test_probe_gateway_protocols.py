import importlib.util
import pathlib
import sys
import unittest


MODULE_PATH = pathlib.Path(__file__).with_name("probe_gateway_protocols.py")
SPEC = importlib.util.spec_from_file_location("probe_gateway_protocols", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
probe = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = probe
SPEC.loader.exec_module(probe)


class ContinuationPayloadTests(unittest.TestCase):
    def test_responses_adds_output_and_new_user_turn(self):
        first = probe.payload_for("responses", "gpt-test", "prefix", "cache-key")
        body = {"output": [{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "OK"}]}]}
        second = probe.continuation_payload("responses", first, body)
        self.assertEqual("user", second["input"][0]["role"])
        self.assertEqual("assistant", second["input"][1]["role"])
        self.assertEqual("user", second["input"][2]["role"])
        self.assertEqual("prefix", second["instructions"])

    def test_chat_adds_assistant_and_new_user_turn(self):
        first = probe.payload_for("chat_completions", "gpt-test", "prefix", "cache-key")
        second = probe.continuation_payload(
            "chat_completions", first, {"choices": [{"message": {"role": "assistant", "content": "OK"}}]}
        )
        self.assertEqual(["system", "user", "assistant", "user"], [item["role"] for item in second["messages"]])

    def test_messages_adds_assistant_and_new_user_turn(self):
        first = probe.payload_for("anthropic_messages", "claude-test", "prefix", "cache-key")
        second = probe.continuation_payload(
            "anthropic_messages", first, {"content": [{"type": "text", "text": "OK"}]}
        )
        self.assertEqual(["user", "assistant", "user"], [item["role"] for item in second["messages"]])
        self.assertEqual(first["system"], second["system"])

    def test_gemini_adds_model_and_new_user_turn(self):
        first = probe.payload_for("gemini", "gemini-test", "prefix", "cache-key")
        second = probe.continuation_payload(
            "gemini", first, {"candidates": [{"content": {"role": "model", "parts": [{"text": "OK"}]}}]}
        )
        self.assertEqual(["user", "model", "user"], [item["role"] for item in second["contents"]])
        self.assertEqual(first["systemInstruction"], second["systemInstruction"])


if __name__ == "__main__":
    unittest.main()
