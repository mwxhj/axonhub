# LLM Transformer Guidelines

> Protocol conversion contracts for the `llm/` module and backend API adapters.

---

## Media Data URL Construction

### 1. Scope / Trigger

Read this section before changing image, audio, video, document, or multipart
transformers that convert binary payloads into `data:` URLs.

### 2. Signatures

Shared helper:

```go
func xurl.BuildDataURL(mediaType string, data string, isBase64 bool) string
func xurl.BuildDataURLFromBytes(mediaType string, data []byte) string
```

### 3. Contracts

- Transformers must use the shared `xurl` helpers instead of ad hoc string
  concatenation for base64 `data:` URLs.
- `BuildDataURLFromBytes` is for raw binary bytes.
- `BuildDataURL(..., true)` is for strings that are already base64 encoded by
  the upstream provider.
- The helper owns the default media type behavior. Callers should pass the best
  known content type and avoid duplicating fallback formatting logic.

### 4. Validation & Error Matrix

| Condition | Required Behavior |
|-----------|-------------------|
| Raw bytes need a `data:` URL | Use `BuildDataURLFromBytes(mediaType, data)`. |
| Provider returns base64 image/audio/video data | Use `BuildDataURL(mediaType, base64Data, true)`. |
| Media type is missing | Let the shared helper apply its default, unless the protocol requires a more specific fallback. |
| Code manually builds `data:<type>;base64,<data>` | Replace with the shared helper. |

### 5. Good / Base / Bad Cases

- Good: Responses image output calls
  `xurl.BuildDataURL("image/png", partialImageB64, true)`.
- Good: multipart image bytes call
  `xurl.BuildDataURLFromBytes(file.ContentType, file.Data)`.
- Base: an incoming URL is already a valid `data:` URL and is passed through.
- Bad: a transformer hand-builds `fmt.Sprintf("data:%s;base64,%s", ...)`.

### 6. Tests Required

When changing media URL conversion:

- add or update helper-level tests for the constructed `data:` URL;
- add or update transformer tests when protocol-specific media type or payload
  shape changes;
- search for remaining ad hoc base64 `data:` URL construction in `llm/`.

### 7. Wrong vs Correct

#### Wrong

```go
url := fmt.Sprintf("data:%s;base64,%s", contentType, encoded)
```

#### Correct

```go
url := xurl.BuildDataURL(contentType, encoded, true)
```

## Scenario: Streaming terminal event handling

### 1. Scope / Trigger
- Trigger: changing inbound/outbound streaming persistence for OpenAI Responses, Chat Completions, Anthropic Messages, Gemini, or audio streams.
- This includes stream close handling, request execution status updates, and persisted request chunks.

### 2. Signatures
- `internal/server/orchestrator/isTerminalStreamEvent(event *httpclient.StreamEvent) bool`
- `internal/server/orchestrator/InboundPersistentStream.Close()`
- `internal/server/orchestrator/OutboundPersistentStream.Close()`

### 3. Contracts
- `response.completed` is a terminal Responses API event.
- `response.failed`, `response.cancelled`, and `response.incomplete` are also terminal Responses API events.
- Terminal does not mean "successful completion"; it means the stream is finished and must not be treated as an incomplete transport loss.

### 4. Validation & Error Matrix
| Condition | Required Behavior |
|-----------|-------------------|
| Responses stream ends with `response.completed` | Mark stream completed and persist normally. |
| Responses stream ends with `response.failed` | Mark stream completed and persist terminal response state. |
| Responses stream ends with `response.cancelled` | Mark stream completed and persist terminal response state. |
| Responses stream ends with `response.incomplete` | Mark stream completed and persist terminal response state. |
| Stream ends without any recognized terminal event and cannot be aggregated to a complete response | Report `stream ended without terminal event or completed response`. |

### 5. Good/Base/Bad Cases
- Good: a Responses stream with `response.failed` is stored as a finished request execution with failed response body.
- Base: a Responses stream with `response.completed` stores as completed.
- Bad: a valid Responses terminal state is misclassified as transport EOF and surfaces the generic incomplete-stream error.

### 6. Tests Required
- Unit test the terminal-event helper for all Responses terminal states.
- Add a persistence regression that proves `response.failed` does not trip the incomplete-stream path.
- Keep existing aggregation-complete tests for terminal-less but fully aggregatable streams.

### 7. Wrong vs Correct
#### Wrong
```go
return event.Type == "response.completed"
```

#### Correct
```go
return event.Type == "response.completed" ||
	event.Type == "response.failed" ||
	event.Type == "response.cancelled" ||
	event.Type == "response.incomplete"
```

## Scenario: Responses function-call argument backfill in streaming output

### 1. Scope / Trigger
- Trigger: changing OpenAI Responses outbound stream transformation for
  `function_call` items, tool-call delta emission, or final-item reconciliation.

### 2. Signatures
- `llm/transformer/openai/responses.(*responsesOutboundStream).transformStreamChunk(...)`
- `llm/transformer/openai/responses.(*responsesOutboundStream).enqueueFunctionCallArgumentsDelta(...)`
- `llm/transformer/openai/responses.(*responsesOutboundStream).backfillFunctionCallArguments(...)`

### 3. Contracts
- Do not assume tool-call arguments always arrive as incremental delta events
  before `response.output_item.done`.
- For Responses API streams, the only available arguments may appear in the
  final `function_call` item or in `response.function_call_arguments.done`.
- The outbound transformer must preserve a valid Chat Completions style delta
  stream even when the provider only supplies final arguments at the end.
- If prior streamed arguments are a prefix of the final arguments, emit only the
  missing suffix as a synthesized delta before updating final tool-call state.
- If no prior arguments were emitted, synthesize one arguments delta with the
  full final payload.
- Tool-call identity resolution must prefer explicit `call_id`, then the
  `item.id -> call_id` mapping recorded from earlier stream items, and only then
  fall back to `item.id`.

### 4. Validation & Error Matrix
| Condition | Required Behavior |
|-----------|-------------------|
| Arguments stream in normally before final item | Keep existing deltas; do not duplicate them. |
| Final item contains a longer arguments string than accumulated state | Emit only the missing suffix delta, then update stored final arguments. |
| No arguments delta ever arrived, but final item includes arguments | Emit one synthesized arguments delta with the full final JSON string. |
| Final item has no `call_id` but earlier item established `item.id -> call_id` | Resolve via the mapping and backfill the correct tool call. |
| Final arguments do not extend accumulated arguments | Do not invent a diff; update state only if needed. |

### 5. Good / Base / Bad Cases
- Good: `response.output_item.done` for a `function_call` carries the only
  arguments payload, and the client still receives a tool-call arguments delta.
- Good: partial arguments streamed first, final item adds the missing tail, and
  only the tail is synthesized.
- Base: normal `response.function_call_arguments.delta` streams continue to work
  without extra emitted chunks.
- Bad: the client sees `function_call.arguments == ""` even though the final
  Responses item included valid JSON arguments.

### 6. Tests Required
- Add a stream regression where `response.output_item.done` is the only source
  of tool-call arguments and assert a synthesized arguments delta is emitted.
- Keep a coverage point for terminal stream completion so the synthesized delta
  path still ends in a finished stream.
- When changing call-id resolution, assert the emitted delta uses the existing
  tool-call index for the original tool call rather than creating a phantom one.

### 7. Wrong vs Correct
#### Wrong
```go
case StreamEventTypeOutputItemDone:
	if streamEvent.Item.Type == "function_call" {
		return nil
	}
```

#### Correct
```go
case StreamEventTypeOutputItemDone:
	if streamEvent.Item.Type == "function_call" {
		callID := s.resolveToolCallID(streamEvent.Item.CallID, lo.ToPtr(streamEvent.Item.ID))
		s.backfillFunctionCallArguments(callID, streamEvent.Item.Name, streamEvent.Item.Arguments)
		return nil
	}
```
