# Protocol Conversion Layer

## Reference Baseline

- Design reference: `Oaklight/llm-rosetta`
- Pinned reference commit: `81ed15999e9a9c40082a7392bd0c0366c18960a0`
- License: MIT
- sub2api implementation language: Go. The reference repository is not copied into this repository and is not a runtime dependency.

The conversion layer uses a hub-and-spoke model: each standard wire protocol is decoded to a common intermediate representation (IR), then encoded to the explicitly selected target protocol. Provider routing, account selection, authentication, endpoint construction, model mapping, retries, billing, and persistence remain outside this layer.

The reference-to-Go responsibility ledger and current production route inventory are maintained in [PROTOCOL_CONVERSION_GAP_AUDIT.md](PROTOCOL_CONVERSION_GAP_AUDIT.md). It distinguishes package-level implementation from production integration and records explicit exclusions.

## Standard Protocols

| ID | Standard wire format | Existing public entry points |
| --- | --- | --- |
| `openai_chat_completions` | OpenAI Chat Completions | `/v1/chat/completions`, `/chat/completions` |
| `openai_responses` | OpenAI Responses | `/v1/responses`, `/responses`, `/backend-api/codex/responses` |
| `anthropic_messages` | Anthropic Messages | `/v1/messages` |
| `google_genai` | Gemini `generateContent` / `streamGenerateContent` | `/v1beta/models/{model}:generateContent`, `/v1beta/models/{model}:streamGenerateContent` |

The source and target protocols are explicit inputs. Endpoint inspection and model-name guessing are not valid routing decisions in the conversion core.

## Current Routing Matrix

The client format alone does not select the upstream format. The effective path also depends on group platform, selected account platform/type, model mapping, and account credentials.

| Group or forced platform | Client formats currently exposed | Effective upstream behavior |
| --- | --- | --- |
| `anthropic` | Messages, Chat Completions, Responses | Native Anthropic Messages; OpenAI formats use compatibility conversion before forwarding |
| `openai` | Messages, Chat Completions, Responses | Responses or Chat Completions according to the OpenAI account path and compatibility fallback |
| `grok` | Messages, Chat Completions, Responses | OpenAI-compatible wire formats with platform-specific policy |
| `gemini` | Messages, Chat Completions, Responses, and native Gemini | Compatibility formats use standard pipelines into Google GenAI; OAuth paths may use the Code Assist envelope, API-key paths use AI Studio semantics |
| `antigravity` | Messages and native Gemini under dedicated routes; may participate in Gemini mixed scheduling | Antigravity `v1internal` envelope over `streamGenerateContent`; Claude-family and Gemini-family behavior is selected from the resolved account/model mapping |
| `opencode_go` | Messages and Chat Completions; Responses is rejected | Protocol comes from account `credentials.model_protocols`, then built-in model protocol metadata/family fallback |

## Antigravity Boundary

Antigravity is not a Google GenAI standard endpoint. Standard Anthropic and Google converters must not inject Antigravity identity prompts, dummy signatures, schema rewrites, project IDs, model substitutions, or internal envelopes.

The Antigravity adapter remains responsible for:

- resolved Claude-family versus Gemini-family selection;
- `project_id`, mapped model, identity prompt, schema cleanup, and internal envelope;
- `thoughtSignature` compatibility and thinking-budget rectification;
- upstream streaming-only behavior and non-stream aggregation;
- signature-related 400 rectification, retry/failover policy, rate limits, sticky sessions, usage, and billing integration.

## Initial Semantic Support

| Semantic area | Chat | Responses | Anthropic | Google GenAI | Loss rule |
| --- | --- | --- | --- | --- | --- |
| Text and ordered parts | Yes | Yes | Yes | Yes | Required |
| System/developer instruction | Yes | Yes | Yes | Yes | Developer role may normalize to system where no distinct role exists |
| Base64/URL image input | Yes | Yes | Base64 standard form | Yes | Unsupported source variants produce a typed warning/error |
| Function tools, calls, results | Yes | Yes | Yes | Yes | IDs are preserved when representable; no fabricated IDs in strict mode |
| Parallel tool calls | Yes | Yes | Via tool choice control | Yes | Target capability warning when control is not expressible |
| Reasoning/thinking text | Compatible extension | Yes | Yes | Yes | Opaque signatures remain metadata and are never invented by standard converters |
| Stop reason | Yes | Yes | Yes | Yes | Normalized IR reason plus original provider value in metadata |
| Usage and cache tokens | Yes | Yes | Yes | Yes | Missing provider data remains absent, never estimated by converters |
| Files, audio, citations/refusal | Partial | Partial | Partial | Partial | Strict error or explicit warning/drop according to conversion policy |

Identity conversion validates JSON and returns the original byte slice unchanged. This preserves unknown compatible fields, prompt prefixes, and cache-key-sensitive serialization.

## Known Risks From Reverted Implementation

Commits `15c253d8` and `e35ec276` were reverted by `4df353d9` and `2a7dc9d8`. They are retained only as investigation material.

- The implementation used the existing Responses structs as a de facto IR, so fields outside that schema were silently constrained by OpenAI Responses semantics.
- Production integration, compatibility fixes, model inventory tests, and Python probes were mixed into the same changeset, preventing an isolated core review and rollback.
- Streaming did not have one four-protocol IR lifecycle. It delegated to existing pairwise state machines and had no Google standard stream converter.
- Endpoint substring detection was available beside explicit protocol inputs, making accidental routing inference possible.
- Antigravity Claude conversion reused its vendor transformer, but Gemini-family handling only validated/re-marshaled the standard body and did not establish a complete adapter contract.
- The probes were Python scripts and did not exercise the exact Go conversion implementation required for production.
- Coverage emphasized current model names and selected text/tool fixtures rather than malformed streams, capability loss, per-request state isolation, fuzzing, and round-trip inflation.

The replacement is therefore implemented as a production-disconnected Go core first. Gateway integration is a separate branch and merge boundary after protocol and live probe gates pass.

## Live Probe Findings

A Stage 2 probe was run against the existing production endpoints on 2026-07-12 with separate test-only keys for the Codex, Antigravity, and OpenCode groups. The keys are not recorded in this repository.

Confirmed behavior:

- Codex `gpt-5.4` Chat and Responses support non-streaming text, streaming text, and a two-request tool call/result round trip. Responses reasoning items, encrypted reasoning content, reasoning-token usage, and cache-read usage were observed.
- Antigravity Claude-family Messages supports text, streaming, images, signed thinking blocks, and a two-request tool call/result round trip.
- Antigravity Gemini-family supports text, streaming, images, thought-token usage, and Google-shaped invalid-argument errors on its dedicated `/antigravity/v1beta/...` routes.
- OpenCode Chat and Messages paths support text, streaming, and tool round trips when a model implementing that protocol capability is selected. Model behavior is not interchangeable: `glm-5` passed Chat tools and `minimax-m2.7` passed Messages tools, while a `deepseek-v4-flash` tool request returned an upstream 502.
- A valid OpenAI Responses stream may emit `response.in_progress` after `response.created`. It updates lifecycle state and must not be treated as a duplicate stream start.

Unresolved production behavior:

- Antigravity Gemini-family non-streaming tool forcing returned HTTP 200 with no `functionCall` and no non-empty text for `gemini-2.5-flash`, `gemini-3-flash`, and `gemini-3.1-pro-high`. A direct streaming diagnostic showed the upstream first SSE chunk does contain `functionCall` plus `thoughtSignature`; the terminal chunk contains only an empty text part. The native Gemini stream-to-non-stream collector currently accumulates only text and images, so it drops the earlier function call. This is a production aggregation defect to fix in the integration branch by preserving all ordered parts. Standard Google semantics must not be weakened to hide it.
- Antigravity Gemini function calls observed on the wire did not include an ID. Standard IR strict mode does not invent one. The retained Gemini vendor response bridge generates the client-facing tool ID; on the next request the shared decoder links that ID back to the function name, and the vendor transport strips non-portable Google IDs after encoding. This keeps correlation policy outside the standard converter.
- A structurally invalid Codex Responses input reached the upstream path and returned 502 instead of a client-facing 400. OpenCode malformed Chat/Messages requests returned 400 with an empty body. Production integration should preserve intentional compatibility but classify local conversion/validation failures before upstream dispatch.

## Production Integration

The production integration keeps transport and account policy outside the converter. Authentication, model mapping, OAuth transforms, retries, sticky sessions, failover, rate limits, response headers, usage accounting, billing, and error passthrough remain owned by their existing services. Local admission, validation, conversion, and synthesized fallback failures keep caller-owned status/message policy but use the source `protocolconv.Renderer` for non-stream JSON envelopes across Chat, Responses, Anthropic, Google, and OpenCode Go entrypoints. Synthesized errors after SSE commitment also use the source renderer for JSON validation and framing: Chat and Google use data-only records, Anthropic uses `event: error`, and Responses retains its service-owned terminal `response.failed` object before renderer framing. Raw upstream HTTP error bodies remain service-owned passthrough; status selection, ops marking, response-commit policy, and protocol terminal semantics remain outside the renderer.

Integrated through the shared IR registry:

- Anthropic-platform Chat Completions, Responses, and Google GenAI request conversion, complete responses, and streaming responses; Google ingress shares the OpenAI/Anthropic account loop while retaining each provider's auth, health, usage, and billing ownership;
- OpenAI Chat Completions to Responses request conversion on accounts that use the Responses upstream path, plus raw Chat same-protocol request/response/stream validation through one identity Pipeline, structured actual-Chat results, bounded SSE parsing, and the Chat renderer. Both buffered and streaming Chat-over-Responses clients consume the internally streaming Responses upstream through `transport.Stream` and the bounded parser; buffered raw terminal responses cross `transport.Response`, while stream events use the request Pipeline and Chat renderer. Timeout, keepalive, partial-record protection, silent-refusal detection, `response.failed`, usage, and disconnect draining remain service-owned. Cursor's Responses-shaped `/chat/completions` request remains the explicit response-only converter exception, but now shares the same structured transport. Raw Chat provider transforms such as model mapping, fast policy, Grok image bridging, GLM effort normalization, silent-refusal failover, and stream-usage injection remain service-owned; an empty successful buffered body remains an explicit compatibility exception outside the non-empty structured response contract;
- Native OpenAI Responses ordinary HTTP and passthrough requests establish a same-protocol Pipeline after provider request transforms for every actual HTTP attempt; any retry that rebuilds and resends the body creates fresh conversion state, and only the successful attempt owns response conversion. Buffered JSON responses cross `transport.Response`; ordinary and passthrough streams plus explicit passthrough SSE-as-unary successes cross `transport.Stream` with the bounded SSE parser, preserve raw terminal fields and reconstructed sparse output, and are framed by the Responses renderer with client-facing model restoration. The same structured Responses policy feeds Google GenAI source output through its request Pipeline and Google renderer, while filtering route-specific `x-codex-*` headers from foreign-source output. Streaming retains timeout/keepalive, `response.failed`/cyber policy, pre-output failover, usage collection, and disconnect draining; conversion and lifecycle processing continue after downstream disconnect. `/responses/compact`, WebSocket transport, Grok's vendor adapter, upstream errors, and content-type/body-heuristic legacy aggregation remain specialized boundaries;
- Native generic Anthropic forwarding (OAuth, setup token, compatible API-key, and Vertex transport) creates a fresh same-protocol Pipeline after the final standard Anthropic body is built for every main, signature-repair, tool-repair, and budget-repair upstream attempt. Buffered success crosses `transport.Response`; streaming success crosses `transport.Stream` and the bounded SSE parser, then both return through the successful attempt's Pipeline and the Anthropic renderer. The parser's bounded raw-record mode preserves transport-level `event:error` compatibility, including empty or non-JSON error data, while normal provider payloads still require Pipeline identity JSON validation. Kimi `cached_tokens` reconciliation, cache-TTL usage reclassification, response model restoration, tool-name restoration, Claude Code noop-delta keepalive, rate/session updates, timeout/failover, TTFT, and disconnect draining remain service-owned; Vertex keeps its outer vendor request envelope outside the standard Pipeline. Anthropic output never emits an OpenAI `[DONE]` sentinel;
- Anthropic API-key native passthrough creates a fresh same-protocol Pipeline for each actual upstream attempt after beta/body sanitization. Buffered JSON success crosses `transport.Response`; streaming success crosses `transport.Stream` and the bounded parser through that attempt's Pipeline and the Anthropic renderer. Exact JSON/event bytes and unknown fields are preserved unless client tool-name restoration is required. Timeout, keepalive, partial-record protection, usage, and disconnect draining remain service-owned transport policy, and the Anthropic source never emits an OpenAI `[DONE]` sentinel;
- Native Gemini generation creates a fresh Google-to-Google identity Pipeline for every actual HTTP attempt after empty-part filtering and thought-signature provider policy, but before Code Assist wrapping. AI Studio and Vertex unary successes, plus Code Assist SSE-as-unary aggregation, cross `transport.Response`; native `streamGenerateContent` crosses a bounded `transport.Stream`, the successful attempt's identity `StreamSession`, and the Google renderer. SSE-as-unary collection uses the same bounded record parser plus the configured non-stream response byte limit, preserves raw transport bytes as metadata, and retains text-chunk aggregation. Code Assist request/response envelopes stay outside standard conversion, with transformed stream records retaining bounded raw transport metadata. Native streams preserve EOF and upstream `[DONE]` as compatible terminals, delay downstream commitment until the first valid Google event, and continue draining usage after client disconnect. Retries, auth, sticky signatures, usage, billing, and `countTokens` fallback remain service-owned;
- Gemini compatibility Chat Completions, OpenAI Responses, and Anthropic Messages request/response/stream conversion to Google GenAI through one shared provider executor;
- OpenCode Chat Completions and Messages requests, complete responses, and streams for both cross-protocol and same-protocol routes. Same-protocol streams use a Pipeline-owned identity session that validates JSON while preserving exact event bytes; the service transport boundary still requires the protocol terminal before the source renderer writes its sentinel. Buffered generation HTTP errors are collected as bounded `transport.Response` values, while errors returned to streaming requests transfer body ownership through `transport.Stream.ErrorBody`; both carry explicit actual Chat or Anthropic protocol before existing raw passthrough, account-health, and failover policy runs, so no downstream SSE status is committed first;
- Antigravity Claude-family and Gemini-family request conversion through the vendor adapter, including identity, schema cleanup, signature rectification, and v1internal envelopes.

The Antigravity native Gemini stream-to-non-stream collector now preserves every ordered part. This keeps early `functionCall`, thinking, `thoughtSignature`, image, and text parts when the terminal upstream chunk contains only finish and usage metadata.

Intentional retained compatibility bridges:

- OpenAI Messages to Codex Responses runs the standard Anthropic-to-Responses request lifecycle first, then applies the retained Codex request transform. That transform is not plain schema conversion: it controls developer-input ordering, Claude Code todo guards, continuation replay trimming, default reasoning/text policy, and Codex call-ID normalization. Both buffered and streaming Messages clients consume the internally streaming Responses upstream through `transport.Stream` and the bounded parser, then return through the request Pipeline and Anthropic renderer. Timeout, keepalive, partial-record protection, `response.failed` policy, usage, and disconnect draining remain service-owned; existing service tests lock the provider policy.
- Gemini native Google ingress and Antigravity response handling retain provider adapters for grounding, image, signature, and vendor-envelope behavior. Standard Google ingress to OpenAI and Anthropic targets, plus Chat, Responses, and Messages compatibility paths to Google targets, use request-scoped pipelines and the Google renderer. The Google stream decoder synthesizes deterministic request-scoped IDs only when an upstream standard function call omits one, so downstream tool-result correlation remains stable without global state.

The old service-local Anthropic-to-Gemini request converter and its duplicate schema/tool mapping were removed. Google server-search tools are represented explicitly by the standard Google converter rather than being flattened into function declarations. Google requires `functionResponse.response` to be a protobuf Struct: object-valued IR tool results are preserved, while scalar or array results are wrapped as `{ "content": <value> }` without discarding the original value.
