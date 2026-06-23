package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	transformer "github.com/looplj/axonhub/llm/transformer"
)

const (
	maxAudioBodySize = 50 * 1024 * 1024
	maxAudioFileSize = 26 * 1024 * 1024
)

// AudioInboundTransformer implements OpenAI-compatible audio APIs.
type AudioInboundTransformer struct {
	apiFormat llm.APIFormat
}

func NewSpeechInboundTransformer() *AudioInboundTransformer {
	return &AudioInboundTransformer{apiFormat: llm.APIFormatOpenAISpeech}
}

func NewTranscriptionInboundTransformer() *AudioInboundTransformer {
	return &AudioInboundTransformer{apiFormat: llm.APIFormatOpenAITranscription}
}

func NewTranslationInboundTransformer() *AudioInboundTransformer {
	return &AudioInboundTransformer{apiFormat: llm.APIFormatOpenAITranslation}
}

func (t *AudioInboundTransformer) APIFormat() llm.APIFormat {
	return t.apiFormat
}

func (t *AudioInboundTransformer) TransformRequest(ctx context.Context, httpReq *httpclient.Request) (*llm.Request, error) {
	if httpReq == nil {
		return nil, fmt.Errorf("%w: http request is nil", transformer.ErrInvalidRequest)
	}
	if len(httpReq.Body) == 0 {
		return nil, fmt.Errorf("%w: request body is empty", transformer.ErrInvalidRequest)
	}

	switch t.apiFormat {
	case llm.APIFormatOpenAISpeech:
		return t.transformSpeechRequest(httpReq)
	case llm.APIFormatOpenAITranscription:
		return t.transformTranscriptionRequest(httpReq)
	case llm.APIFormatOpenAITranslation:
		return t.transformTranslationRequest(httpReq)
	default:
		return nil, fmt.Errorf("%w: unknown audio api format: %s", transformer.ErrInvalidRequest, t.apiFormat)
	}
}

func (t *AudioInboundTransformer) transformSpeechRequest(httpReq *httpclient.Request) (*llm.Request, error) {
	contentType := strings.ToLower(httpReq.Headers.Get("Content-Type"))
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		return nil, fmt.Errorf("%w: speech requires application/json", transformer.ErrInvalidRequest)
	}

	var body SpeechRequestBody
	if err := json.Unmarshal(httpReq.Body, &body); err != nil {
		return nil, fmt.Errorf("%w: failed to decode speech request: %w", transformer.ErrInvalidRequest, err)
	}
	if body.Model == "" {
		return nil, fmt.Errorf("%w: model is required", transformer.ErrInvalidRequest)
	}
	if body.Input == "" {
		return nil, fmt.Errorf("%w: input is required", transformer.ErrInvalidRequest)
	}
	if body.Voice == "" {
		return nil, fmt.Errorf("%w: voice is required", transformer.ErrInvalidRequest)
	}

	streamFormat := strings.ToLower(strings.TrimSpace(body.StreamFormat))
	if streamFormat != "" && streamFormat != "sse" && streamFormat != "audio" {
		return nil, fmt.Errorf("%w: unsupported stream_format: %q", transformer.ErrInvalidRequest, body.StreamFormat)
	}

	return &llm.Request{
		Model:       body.Model,
		Stream:      lo.ToPtr(streamFormat != ""),
		RawRequest:  httpReq,
		RequestType: llm.RequestTypeSpeech,
		APIFormat:   t.apiFormat,
		Speech: &llm.SpeechRequest{
			Input:          body.Input,
			Voice:          body.Voice,
			ResponseFormat: body.ResponseFormat,
			Speed:          body.Speed,
			Instructions:   body.Instructions,
			StreamFormat:   streamFormat,
		},
	}, nil
}

func (t *AudioInboundTransformer) transformTranscriptionRequest(httpReq *httpclient.Request) (*llm.Request, error) {
	form, err := parseAudioMultipartRequest(httpReq)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(form.First("model"))
	if model == "" {
		return nil, fmt.Errorf("%w: model is required", transformer.ErrInvalidRequest)
	}
	if len(form.File) == 0 {
		return nil, fmt.Errorf("%w: file is required for transcription", transformer.ErrInvalidRequest)
	}

	temperature, err := parseOptionalFloat64("temperature", form.First("temperature"))
	if err != nil {
		return nil, err
	}
	isStream, err := parseStreamField(form.First("stream"))
	if err != nil {
		return nil, err
	}

	extra := form.extraFields()
	delete(extra, "stream")
	httpReq.JSONBody = buildAudioJSONBody(form)

	return &llm.Request{
		Model:       model,
		Stream:      lo.ToPtr(isStream),
		RawRequest:  httpReq,
		RequestType: llm.RequestTypeTranscription,
		APIFormat:   t.apiFormat,
		Transcription: &llm.TranscriptionRequest{
			File:           form.File,
			FileName:       form.FileName,
			Language:       strings.TrimSpace(form.First("language")),
			Prompt:         strings.TrimSpace(form.First("prompt")),
			ResponseFormat: strings.TrimSpace(form.First("response_format")),
			Temperature:    temperature,
			Extra:          extra,
		},
	}, nil
}

func (t *AudioInboundTransformer) transformTranslationRequest(httpReq *httpclient.Request) (*llm.Request, error) {
	form, err := parseAudioMultipartRequest(httpReq)
	if err != nil {
		return nil, err
	}

	model := strings.TrimSpace(form.First("model"))
	if model == "" {
		return nil, fmt.Errorf("%w: model is required", transformer.ErrInvalidRequest)
	}
	if len(form.File) == 0 {
		return nil, fmt.Errorf("%w: file is required for translation", transformer.ErrInvalidRequest)
	}

	temperature, err := parseOptionalFloat64("temperature", form.First("temperature"))
	if err != nil {
		return nil, err
	}
	isStream, err := parseStreamField(form.First("stream"))
	if err != nil {
		return nil, err
	}

	extra := form.extraFields()
	delete(extra, "stream")
	httpReq.JSONBody = buildAudioJSONBody(form)

	return &llm.Request{
		Model:       model,
		Stream:      lo.ToPtr(isStream),
		RawRequest:  httpReq,
		RequestType: llm.RequestTypeTranslation,
		APIFormat:   t.apiFormat,
		Translation: &llm.TranslationRequest{
			File:           form.File,
			FileName:       form.FileName,
			Prompt:         strings.TrimSpace(form.First("prompt")),
			ResponseFormat: strings.TrimSpace(form.First("response_format")),
			Temperature:    temperature,
			Extra:          extra,
		},
	}, nil
}

func (t *AudioInboundTransformer) TransformResponse(ctx context.Context, llmResp *llm.Response) (*httpclient.Response, error) {
	if llmResp == nil {
		return nil, fmt.Errorf("%w: audio response is nil", transformer.ErrInvalidResponse)
	}

	if t.apiFormat == llm.APIFormatOpenAISpeech {
		if llmResp.Speech == nil {
			return nil, fmt.Errorf("%w: speech response is nil", transformer.ErrInvalidResponse)
		}

		contentType := llmResp.Speech.ContentType
		if contentType == "" {
			contentType = "audio/mpeg"
		}

		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body:       llmResp.Speech.Audio,
			Headers:    http.Header{"Content-Type": []string{contentType}},
		}, nil
	}

	if llmResp.Transcription == nil {
		return nil, fmt.Errorf("%w: transcription response is nil", transformer.ErrInvalidResponse)
	}

	tr := llmResp.Transcription
	if len(tr.Raw) > 0 {
		contentType := tr.RawContentType
		if contentType == "" {
			contentType = "application/json"
		}

		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body:       tr.Raw,
			Headers:    http.Header{"Content-Type": []string{contentType}},
		}, nil
	}

	body, err := json.Marshal(struct {
		Text     string   `json:"text"`
		Language string   `json:"language,omitempty"`
		Duration *float64 `json:"duration,omitempty"`
	}{
		Text:     tr.Text,
		Language: tr.Language,
		Duration: tr.Duration,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal transcription response: %w", err)
	}

	return &httpclient.Response{
		StatusCode: http.StatusOK,
		Body:       body,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func (t *AudioInboundTransformer) TransformStream(ctx context.Context, stream streams.Stream[*llm.Response]) (streams.Stream[*httpclient.StreamEvent], error) {
	return streams.NoNil(streams.MapErr(stream, func(resp *llm.Response) (*httpclient.StreamEvent, error) {
		if resp == nil {
			return nil, nil
		}
		if resp == llm.DoneResponse || resp.Object == "[DONE]" {
			if t.apiFormat == llm.APIFormatOpenAISpeech && resp.RequestType == llm.RequestTypeSpeech {
				return &httpclient.StreamEvent{Type: httpclient.BinaryStreamDoneEventType}, nil
			}

			return nil, nil
		}

		if resp.SpeechStreamEvent != nil {
			data, err := json.Marshal(resp.SpeechStreamEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal speech stream event: %w", err)
			}

			return &httpclient.StreamEvent{Type: resp.SpeechStreamEvent.Type, Data: data}, nil
		}

		if resp.SpeechAudioChunk != nil {
			return &httpclient.StreamEvent{
				Type: resp.SpeechAudioChunk.ContentType,
				Data: resp.SpeechAudioChunk.Audio,
			}, nil
		}

		if resp.TranscriptionStreamEvent != nil {
			data, err := json.Marshal(resp.TranscriptionStreamEvent)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal transcription stream event: %w", err)
			}

			return &httpclient.StreamEvent{Type: resp.TranscriptionStreamEvent.Type, Data: data}, nil
		}

		return nil, nil
	})), nil
}

func (t *AudioInboundTransformer) TransformError(ctx context.Context, rawErr error) *httpclient.Error {
	return NewInboundTransformer().TransformError(ctx, rawErr)
}

func (t *AudioInboundTransformer) AggregateStreamChunks(ctx context.Context, chunks []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	if t.apiFormat == llm.APIFormatOpenAISpeech {
		return aggregateSpeechStreamChunks(chunks)
	}

	return aggregateTranscriptionStreamChunks(chunks)
}

func parseStreamField(s string) (bool, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return false, nil
	}

	switch s {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%w: invalid stream value: %q", transformer.ErrInvalidRequest, s)
	}
}

type audioFormData struct {
	File     []byte
	FileName string
	Fields   map[string][]string
}

func (f *audioFormData) First(name string) string {
	values := f.Fields[name]
	if len(values) == 0 {
		return ""
	}

	return values[0]
}

var audioKnownFields = map[string]bool{
	"model":           true,
	"language":        true,
	"prompt":          true,
	"response_format": true,
	"temperature":     true,
}

func (f *audioFormData) extraFields() map[string][]string {
	var extra map[string][]string
	for name, values := range f.Fields {
		if audioKnownFields[name] {
			continue
		}
		if extra == nil {
			extra = make(map[string][]string)
		}
		extra[name] = values
	}

	return extra
}

func parseAudioMultipartRequest(httpReq *httpclient.Request) (*audioFormData, error) {
	if len(httpReq.Body) > maxAudioBodySize {
		return nil, fmt.Errorf("%w: request body too large", transformer.ErrInvalidRequest)
	}

	mediaType, params, err := mime.ParseMediaType(httpReq.Headers.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid content-type", transformer.ErrInvalidRequest)
	}
	if !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		return nil, fmt.Errorf("%w: expected multipart/form-data", transformer.ErrInvalidRequest)
	}

	boundary := params["boundary"]
	if boundary == "" {
		return nil, fmt.Errorf("%w: missing boundary in content-type", transformer.ErrInvalidRequest)
	}

	reader := multipart.NewReader(bytes.NewReader(httpReq.Body), boundary)
	form := &audioFormData{Fields: map[string][]string{}}

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read multipart", transformer.ErrInvalidRequest)
		}

		fieldName := part.FormName()
		filename := part.FileName()
		if filename == "" {
			value, err := io.ReadAll(io.LimitReader(part, maxAudioFileSize+1))
			if err != nil {
				return nil, fmt.Errorf("%w: failed to read multipart field", transformer.ErrInvalidRequest)
			}
			if len(value) > maxAudioFileSize {
				return nil, fmt.Errorf("%w: multipart field too large", transformer.ErrInvalidRequest)
			}
			form.Fields[fieldName] = append(form.Fields[fieldName], string(value))
			continue
		}

		if fieldName != "file" {
			continue
		}
		if form.File != nil {
			return nil, fmt.Errorf("%w: multiple file parts are not allowed", transformer.ErrInvalidRequest)
		}

		data, err := io.ReadAll(io.LimitReader(part, maxAudioFileSize+1))
		if err != nil {
			return nil, fmt.Errorf("%w: failed to read multipart file", transformer.ErrInvalidRequest)
		}
		if len(data) > maxAudioFileSize {
			return nil, fmt.Errorf("%w: file too large", transformer.ErrInvalidRequest)
		}

		form.File = data
		form.FileName = filename
	}

	return form, nil
}

func buildAudioJSONBody(form *audioFormData) []byte {
	body := make(map[string]any, len(form.Fields)+1)
	for k, values := range form.Fields {
		switch len(values) {
		case 0:
		case 1:
			if values[0] != "" {
				body[k] = values[0]
			}
		default:
			body[k] = values
		}
	}
	body["file"] = fmt.Sprintf("<audio bytes: %d, filename: %s>", len(form.File), form.FileName)

	b, err := json.Marshal(body)
	if err != nil {
		return nil
	}

	return b
}

func parseOptionalFloat64(name, s string) (*float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s: %q", transformer.ErrInvalidRequest, name, s)
	}

	return &v, nil
}
