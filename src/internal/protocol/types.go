package protocol

// 1.1 vad_start
type VadStartResponseData struct {
	SaidWords             string `json:"said_words"`
	RawTheWordsLeftUnsaid string `json:"raw_the_words_left_unsaid"`
	LLMState              string `json:"llm_state"`
}

// 1.2 vad_end
type VadEndResponseData struct {
	PrefixAudio string `json:"prefix_audio"`
	Audio       string `json:"audio"`
}

// 2.1 tts_engine/infer
type TTSInferRequestData struct {
	InputToken string `json:"input_token"`
	State      string `json:"state"`
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
}

type TTSInferResponseAudioHeaderData struct {
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
}

// 3.1 asr_engine/infer
type ASRInferRequestData struct {
	PrefixAudio string `json:"prefix_audio"`
	Audio       string `json:"audio"`
	Format      string `json:"format"`
	SampleRate  int    `json:"sample_rate"`
}

type ASRInferResponseData struct {
	TranscribedWords string `json:"transcribed_words"`
}

// 4.1 llm_engine/infer
type LLMInferRequestData struct {
	Prompt string `json:"prompt"`
}

type LLMInferResponseData struct {
	Token string `json:"token"`
	State string `json:"state"`
}

// 4.2 session/interrupt
type SessionInterruptRequestData struct {
	SaidWords string `json:"said_words"`
}

type SessionInterruptResponseData struct {
	RawTheWordsLeftUnsaid string `json:"raw_the_words_left_unsaid"`
	LLMState              string `json:"llm_state"`
}
