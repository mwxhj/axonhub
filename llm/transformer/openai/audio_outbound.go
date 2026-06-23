package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
)

// SpeechRequestBody represents the JSON request body for the OpenAI /audio/speech API.
type SpeechRequestBody struct {
	Model          string   `json:"model"`
	Input          string   `json:"input"`
	Voice          string   `json:"voice"`
	ResponseFormat string   `json:"response_format,omitempty"`
	Speed          *float64 `json:"speed,omitempty"`
	Instructions   string   `json:"instructions,omitempty"`
	StreamFormat   string   `json:"stream_format,omitempty"`
}

func (t *OutboundTransformer) buildSpeechRequest(ctx context.Context, llmReq *llm.Request) (*httpclient.Request, error) {
	if llmReq.Speech == nil {
		return nil, fmt.Errorf("%w: speech request is nil in llm.Request", transformer.ErrInvalidRequest)
	}
	if llmReq.Speech.Input == "" {
		return nil, fmt.Errorf("%w: input is required for speech", transformer.ErrInvalidRequest)
	}
	if llmReq.Speech.Voice == "" {
		return nil, fmt.Errorf("%w: voice is required for speech", transformer.ErrInvalidRequest)
	}

	body, err := json.Marshal(SpeechRequestBody{
		Model:          llmReq.Model,
		Input:          llmReq.Speech.Input,
		Voice:          llmReq.Speech.Voice,
		ResponseFormat: llmReq.Speech.ResponseFormat,
		Speed:          llmReq.Speech.Speed,
		Instructions:   llmReq.Speech.Instructions,
		StreamFormat:   llmReq.Speech.StreamFormat,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal speech request: %w", err)
	}

	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	if llmReq.Speech.StreamFormat == "sse" {
		headers.Set("Accept", "text/event-stream")
	} else {
		headers.Set("Accept", "*/*")
	}

	return &httpclient.Request{
		Method:      http.MethodPost,
		URL:         t.buildAudioURL("/audio/speech"),
		Headers:     headers,
		Body:        body,
		ContentType: "application/json",
		Auth: &httpclient.AuthConfig{
			Type:   httpclient.AuthTypeBearer,
			APIKey: t.config.APIKeyProvider.Get(ctx),
		},
		RequestType: string(llm.RequestTypeSpeech),
		APIFormat:   string(llm.APIFormatOpenAISpeech),
	}, nil
}

func (t *OutboundTransformer) buildTranscriptionRequest(ctx context.Context, llmReq *llm.Request) (*httpclient.Request, error) {
	if llmReq.Transcription == nil {
		return nil, fmt.Errorf("%w: transcription request is nil in llm.Request", transformer.ErrInvalidRequest)
	}

	tr := llmReq.Transcription
	if len(tr.File) == 0 {
		return nil, fmt.Errorf("%w: file is required for transcription", transformer.ErrInvalidRequest)
	}

	fields := map[string][]string{"model": {llmReq.Model}}
	if tr.Language != "" {
		fields["language"] = []string{tr.Language}
	}
	if tr.Prompt != "" {
		fields["prompt"] = []string{tr.Prompt}
	}
	if tr.ResponseFormat != "" {
		fields["response_format"] = []string{tr.ResponseFormat}
	}
	if tr.Temperature != nil {
		fields["temperature"] = []string{strconv.FormatFloat(*tr.Temperature, 'f', -1, 64)}
	}
	for name, values := range tr.Extra {
		fields[name] = values
	}

	stream := llmReq.Stream != nil && *llmReq.Stream
	if stream {
		fields["stream"] = []string{"true"}
	}

	return t.buildAudioMultipartRequest(
		ctx,
		"/audio/transcriptions",
		llm.RequestTypeTranscription,
		llm.APIFormatOpenAITranscription,
		tr.File,
		tr.FileName,
		fields,
		stream,
	)
}

func (t *OutboundTransformer) buildTranslationRequest(ctx context.Context, llmReq *llm.Request) (*httpclient.Request, error) {
	if llmReq.Translation == nil {
		return nil, fmt.Errorf("%w: translation request is nil in llm.Request", transformer.ErrInvalidRequest)
	}

	tr := llmReq.Translation
	if len(tr.File) == 0 {
		return nil, fmt.Errorf("%w: file is required for translation", transformer.ErrInvalidRequest)
	}

	fields := map[string][]string{"model": {llmReq.Model}}
	if tr.Prompt != "" {
		fields["prompt"] = []string{tr.Prompt}
	}
	if tr.ResponseFormat != "" {
		fields["response_format"] = []string{tr.ResponseFormat}
	}
	if tr.Temperature != nil {
		fields["temperature"] = []string{strconv.FormatFloat(*tr.Temperature, 'f', -1, 64)}
	}
	for name, values := range tr.Extra {
		fields[name] = values
	}

	stream := llmReq.Stream != nil && *llmReq.Stream
	if stream {
		fields["stream"] = []string{"true"}
	}

	return t.buildAudioMultipartRequest(
		ctx,
		"/audio/translations",
		llm.RequestTypeTranslation,
		llm.APIFormatOpenAITranslation,
		tr.File,
		tr.FileName,
		fields,
		stream,
	)
}

func (t *OutboundTransformer) buildAudioMultipartRequest(
	ctx context.Context,
	path string,
	requestType llm.RequestType,
	apiFormat llm.APIFormat,
	file []byte,
	fileName string,
	fields map[string][]string,
	stream bool,
) (*httpclient.Request, error) {
	fileName = sanitizeAudioFileName(fileName)
	if fileName == "" {
		fileName = "audio.mp3"
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(file)); err != nil {
		return nil, fmt.Errorf("failed to write audio data: %w", err)
	}
	for k, values := range fields {
		for _, v := range values {
			if err := writer.WriteField(k, v); err != nil {
				return nil, fmt.Errorf("failed to write %s field: %w", k, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	jsonBody := make(map[string]any, len(fields)+1)
	for k, values := range fields {
		if len(values) == 1 {
			jsonBody[k] = values[0]
		} else {
			jsonBody[k] = values
		}
	}
	jsonBody["file"] = fmt.Sprintf("<audio bytes: %d, filename: %s>", len(file), fileName)

	jsonBodyBytes, err := json.Marshal(jsonBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON body: %w", err)
	}

	headers := make(http.Header)
	headers.Set("Content-Type", writer.FormDataContentType())
	if stream {
		headers.Set("Accept", "text/event-stream")
	} else {
		headers.Set("Accept", "application/json")
	}

	return &httpclient.Request{
		Method:      http.MethodPost,
		URL:         t.buildAudioURL(path),
		Headers:     headers,
		ContentType: writer.FormDataContentType(),
		Body:        body.Bytes(),
		JSONBody:    jsonBodyBytes,
		Auth: &httpclient.AuthConfig{
			Type:   httpclient.AuthTypeBearer,
			APIKey: t.config.APIKeyProvider.Get(ctx),
		},
		RequestType: string(requestType),
		APIFormat:   string(apiFormat),
	}, nil
}

func sanitizeAudioFileName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}

		return r
	}, name)
}

func (t *OutboundTransformer) buildAudioURL(defaultPath string) string {
	if t.config.EndpointPath != "" {
		return t.config.BaseURL + t.config.EndpointPath
	}

	return t.config.BaseURL + defaultPath
}

func transformSpeechStreamChunk(event *httpclient.StreamEvent) (*llm.Response, error) {
	if event == nil || len(event.Data) == 0 {
		return nil, nil
	}
	if bytes.HasPrefix(event.Data, []byte("[DONE]")) {
		return llm.DoneResponse, nil
	}
	if streamErr := parseStreamErrorEvent(event); streamErr != nil {
		return nil, streamErr
	}

	var ev llm.SpeechStreamEvent
	if err := json.Unmarshal(event.Data, &ev); err != nil {
		return nil, fmt.Errorf("failed to decode speech stream event: %w", err)
	}

	return &llm.Response{
		RequestType:       llm.RequestTypeSpeech,
		APIFormat:         llm.APIFormatOpenAISpeech,
		SpeechStreamEvent: &ev,
		Usage:             ev.Usage,
	}, nil
}

func transformSpeechBinaryChunk(event *httpclient.StreamEvent) (*llm.Response, error) {
	if event != nil && event.Type == httpclient.BinaryStreamDoneEventType {
		return &llm.Response{
			Object:      "[DONE]",
			RequestType: llm.RequestTypeSpeech,
			APIFormat:   llm.APIFormatOpenAISpeech,
		}, nil
	}
	if event == nil || len(event.Data) == 0 {
		return nil, nil
	}

	contentType := strings.TrimSpace(event.Type)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	return &llm.Response{
		RequestType: llm.RequestTypeSpeech,
		APIFormat:   llm.APIFormatOpenAISpeech,
		SpeechAudioChunk: &llm.SpeechAudioChunk{
			Audio:       bytes.Clone(event.Data),
			ContentType: contentType,
		},
	}, nil
}

func isSpeechBinaryStreamEvent(event *httpclient.StreamEvent) bool {
	return event != nil &&
		(event.Type == httpclient.BinaryStreamDoneEventType || event.IsBinaryAudioChunk())
}

func speechStreamChunkTransformFor(_ *httpclient.Request) func(*httpclient.StreamEvent) (*llm.Response, error) {
	return func(event *httpclient.StreamEvent) (*llm.Response, error) {
		if isSpeechBinaryStreamEvent(event) {
			return transformSpeechBinaryChunk(event)
		}

		return transformSpeechStreamChunk(event)
	}
}

func transformTranscriptionStreamChunkFor(apiFormat llm.APIFormat) func(*httpclient.StreamEvent) (*llm.Response, error) {
	requestType := requestTypeForAudioFormat(apiFormat)

	return func(event *httpclient.StreamEvent) (*llm.Response, error) {
		if event == nil || len(event.Data) == 0 {
			return nil, nil
		}
		if bytes.HasPrefix(event.Data, []byte("[DONE]")) {
			return llm.DoneResponse, nil
		}
		if streamErr := parseStreamErrorEvent(event); streamErr != nil {
			return nil, streamErr
		}

		var ev llm.TranscriptionStreamEvent
		if err := json.Unmarshal(event.Data, &ev); err != nil {
			return nil, fmt.Errorf("failed to decode transcription stream event: %w", err)
		}

		return &llm.Response{
			RequestType:              requestType,
			APIFormat:                apiFormat,
			TranscriptionStreamEvent: &ev,
			Usage:                    ev.Usage,
		}, nil
	}
}

func transformSpeechResponse(httpResp *httpclient.Response) (*llm.Response, error) {
	contentType := httpResp.Headers.Get("Content-Type")
	if contentType == "" {
		contentType = "audio/mpeg"
	}

	return &llm.Response{
		RequestType: llm.RequestTypeSpeech,
		APIFormat:   llm.APIFormatOpenAISpeech,
		Speech: &llm.SpeechResponse{
			Audio:       httpResp.Body,
			ContentType: contentType,
		},
	}, nil
}

func transformTranscriptionResponse(httpResp *httpclient.Response, apiFormat llm.APIFormat) (*llm.Response, error) {
	contentType := httpResp.Headers.Get("Content-Type")

	resp := &llm.Response{
		RequestType: requestTypeForAudioFormat(apiFormat),
		APIFormat:   apiFormat,
	}

	isJSON := strings.Contains(strings.ToLower(contentType), "application/json")
	if !isJSON && contentType == "" {
		isJSON = looksLikeJSON(httpResp.Body) && json.Valid(httpResp.Body)
	}
	if isJSON {
		var parsed llm.TranscriptionResponse
		if err := json.Unmarshal(httpResp.Body, &parsed); err != nil {
			return nil, fmt.Errorf("failed to unmarshal transcription response: %w", err)
		}

		parsed.Raw = httpResp.Body
		parsed.RawContentType = "application/json"
		resp.Transcription = &parsed

		return resp, nil
	}

	if contentType == "" {
		contentType = "text/plain"
	}

	resp.Transcription = &llm.TranscriptionResponse{
		Text:           string(httpResp.Body),
		Raw:            httpResp.Body,
		RawContentType: contentType,
	}

	return resp, nil
}

func aggregateSpeechStreamChunks(chunks []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	var (
		audioBytes int
		usage      *llm.Usage
	)

	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		if chunk.Type == httpclient.BinaryStreamDoneEventType {
			continue
		}
		if isSpeechBinaryStreamEvent(chunk) {
			if n := len(chunk.Data); n > 0 {
				audioBytes += n
			} else {
				audioBytes += chunk.Size
			}
			continue
		}
		if len(chunk.Data) == 0 {
			continue
		}
		if bytes.HasPrefix(chunk.Data, []byte("[DONE]")) {
			continue
		}

		var ev llm.SpeechStreamEvent
		if err := json.Unmarshal(chunk.Data, &ev); err != nil {
			continue
		}
		if ev.AudioBase64 != "" {
			audioBytes += base64DecodedLen(ev.AudioBase64)
		}
		if ev.Usage != nil {
			usage = ev.Usage
		}
	}

	body, err := json.Marshal(map[string]any{
		"object":      llm.SpeechStreamResponseID,
		"audio_bytes": audioBytes,
		"chunks":      len(chunks),
	})
	if err != nil {
		return nil, llm.ResponseMeta{}, fmt.Errorf("failed to marshal speech stream aggregate: %w", err)
	}

	return body, llm.ResponseMeta{
		ID:    llm.SpeechStreamResponseID,
		Usage: usage,
	}, nil
}

func aggregateTranscriptionStreamChunks(chunks []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	var (
		deltaBuilder strings.Builder
		finalText    string
		usage        *llm.Usage
	)

	for _, chunk := range chunks {
		if chunk == nil || len(chunk.Data) == 0 {
			continue
		}
		if bytes.HasPrefix(chunk.Data, []byte("[DONE]")) {
			continue
		}

		var ev llm.TranscriptionStreamEvent
		if err := json.Unmarshal(chunk.Data, &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "transcript.text.delta":
			deltaBuilder.WriteString(ev.Delta)
		case "transcript.text.done":
			if ev.Text != "" {
				finalText = ev.Text
			}
			if ev.Usage != nil {
				usage = ev.Usage
			}
		}
	}

	text := finalText
	if text == "" {
		text = deltaBuilder.String()
	}

	body, err := json.Marshal(map[string]any{"text": text})
	if err != nil {
		return nil, llm.ResponseMeta{}, fmt.Errorf("failed to marshal transcription stream aggregate: %w", err)
	}

	return body, llm.ResponseMeta{Usage: usage}, nil
}

func requestTypeForAudioFormat(apiFormat llm.APIFormat) llm.RequestType {
	if apiFormat == llm.APIFormatOpenAITranslation {
		return llm.RequestTypeTranslation
	}

	return llm.RequestTypeTranscription
}

func looksLikeJSON(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
}

func base64DecodedLen(s string) int {
	n := len(strings.TrimRight(s, "="))
	return n * 3 / 4
}
