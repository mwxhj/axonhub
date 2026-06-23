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

