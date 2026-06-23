package llm

import "encoding/json"

// SpeechStreamResponseID is the synthetic response ID used for aggregated
// streaming TTS metadata.
const SpeechStreamResponseID = "audio.speech.stream"

// SpeechRequest is the unified text-to-speech (TTS) request structure.
// It maps to OpenAI's POST /v1/audio/speech API.
type SpeechRequest struct {
	// Input is the text to generate audio for.
	Input string `json:"input"`

	// Voice is the voice to use when generating the audio (e.g. alloy, echo, nova).
	Voice string `json:"voice"`

	// ResponseFormat is the audio format (mp3, opus, aac, flac, wav, pcm).
	ResponseFormat string `json:"response_format,omitempty"`

	// Speed is the speed of the generated audio, from 0.25 to 4.0.
	Speed *float64 `json:"speed,omitempty"`

	// Instructions controls the voice tone for supported models.
	Instructions string `json:"instructions,omitempty"`

	// StreamFormat opts into SSE streaming for gpt-4o-mini-tts.
	StreamFormat string `json:"stream_format,omitempty"`
}

// SpeechStreamEvent represents a single SSE event emitted by the OpenAI streaming TTS API.
type SpeechStreamEvent struct {
	Type string `json:"type"`

	AudioBase64 string `json:"audio,omitempty"`

	Usage *Usage `json:"usage,omitempty"`
}

// SpeechAudioChunk represents one binary audio chunk emitted by streaming TTS
// when the provider returns raw chunked audio instead of SSE.
type SpeechAudioChunk struct {
	Audio []byte `json:"-"`

	ContentType string `json:"-"`
}

// SpeechResponse represents the unified TTS response.
type SpeechResponse struct {
	Audio       []byte `json:"-"`
	ContentType string `json:"-"`
}

// TranscriptionRequest is the unified STT transcription request structure.
type TranscriptionRequest struct {
	File []byte `json:"-"`

	FileName string `json:"-"`

	Language string `json:"language,omitempty"`
	Prompt   string `json:"prompt,omitempty"`

	ResponseFormat string `json:"response_format,omitempty"`

	Temperature *float64 `json:"temperature,omitempty"`

	Extra map[string][]string `json:"extra,omitempty"`
}

// TranscriptionStreamEvent represents a single SSE event emitted by the OpenAI
// streaming transcription/translation API.
type TranscriptionStreamEvent struct {
	Type string `json:"type"`

	Delta string `json:"delta,omitempty"`
	Text  string `json:"text,omitempty"`

	Logprobs json.RawMessage `json:"logprobs,omitempty"`

	Usage *Usage `json:"usage,omitempty"`
}

// TranslationRequest is the unified STT translation request structure.
type TranslationRequest struct {
	File []byte `json:"-"`

	FileName string `json:"-"`

	Prompt string `json:"prompt,omitempty"`

	ResponseFormat string `json:"response_format,omitempty"`

	Temperature *float64 `json:"temperature,omitempty"`

	Extra map[string][]string `json:"extra,omitempty"`
}

// TranscriptionResponse represents the unified STT response, shared by transcription and translation.
type TranscriptionResponse struct {
	Text     string   `json:"text,omitempty"`
	Language string   `json:"language,omitempty"`
	Duration *float64 `json:"duration,omitempty"`

	Raw            []byte `json:"-"`
	RawContentType string `json:"-"`
}
