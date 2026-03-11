// ---------------------------------------------------------------------------
// DOM elements
// ---------------------------------------------------------------------------
const statusRing = document.getElementById("status-ring");
const statusLabel = document.getElementById("status-label");
const startBtn = document.getElementById("start-btn");
const transcriptEl = document.getElementById("transcript-content");
const responseEl = document.getElementById("response-content");

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------
let ws = null;
let vad = null;
let running = false;
let isSpeaking = false;

// Pre-speech buffer: keep last ~1s of audio frames before speech starts.
// Each VAD frame ≈ 96ms at 16kHz (1536 samples), so ~11 frames ≈ 1s.
const PRE_SPEECH_FRAMES = 11;
let preSpeechBuffer = [];

// Audio playback queue
let audioQueue = [];
let isPlaying = false;
let currentSource = null;
let audioContext = null;

// ---------------------------------------------------------------------------
// WebSocket
// ---------------------------------------------------------------------------
function connectWS() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  ws = new WebSocket(`${proto}//${location.host}/ws`);
  ws.binaryType = "arraybuffer";

  ws.onopen = () => console.log("WS connected");
  ws.onclose = () => {
    console.log("WS disconnected");
    if (running) setTimeout(connectWS, 2000);
  };
  ws.onerror = (e) => console.error("WS error:", e);

  ws.onmessage = (event) => {
    if (event.data instanceof ArrayBuffer) {
      queueAudio(event.data);
    } else {
      handleServerMessage(JSON.parse(event.data));
    }
  };
}

function sendJSON(obj) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(obj));
  }
}

function sendAudio(int16Array) {
  if (ws && ws.readyState === WebSocket.OPEN) {
    ws.send(int16Array.buffer);
  }
}

// ---------------------------------------------------------------------------
// Server message handling
// ---------------------------------------------------------------------------
function handleServerMessage(msg) {
  switch (msg.type) {
    case "status":
      setStatus(msg.state);
      break;
    case "transcript":
      transcriptEl.textContent = msg.text;
      break;
    case "response":
      responseEl.textContent += msg.text;
      break;
  }
}

function setStatus(state) {
  statusRing.className = state;
  statusLabel.textContent = state.toUpperCase();

  // Clear response display when a new listening cycle starts
  if (state === "listening") {
    transcriptEl.textContent = "";
    responseEl.textContent = "";
    stopAudio();
  }
}

// ---------------------------------------------------------------------------
// VAD + Audio capture
// ---------------------------------------------------------------------------
async function startVAD() {
  audioContext = new AudioContext({ sampleRate: 16000 });

  vad = await vad_web.MicVAD.new({
    positiveSpeechThreshold: 0.8,
    negativeSpeechThreshold: 0.3,
    minSpeechFrames: 3,
    preSpeechPadFrames: 0, // we handle pre-speech ourselves

    onSpeechStart: () => {
      isSpeaking = true;
      sendJSON({ type: "vad_start" });

      // Flush pre-speech buffer (≈1s before speech)
      for (const frame of preSpeechBuffer) {
        sendAudio(float32ToInt16(frame));
      }
      preSpeechBuffer = [];
    },

    onSpeechEnd: (_audio) => {
      isSpeaking = false;
      sendJSON({ type: "vad_end" });
    },

    onFrameProcessed: (_probs, frame) => {
      if (isSpeaking) {
        sendAudio(float32ToInt16(frame));
      } else {
        // Maintain rolling 1s buffer
        preSpeechBuffer.push(new Float32Array(frame));
        if (preSpeechBuffer.length > PRE_SPEECH_FRAMES) {
          preSpeechBuffer.shift();
        }
      }
    },
  });

  vad.start();
}

function stopVAD() {
  if (vad) {
    vad.destroy();
    vad = null;
  }
  if (audioContext) {
    audioContext.close();
    audioContext = null;
  }
  isSpeaking = false;
  preSpeechBuffer = [];
}

// ---------------------------------------------------------------------------
// Audio playback queue
// ---------------------------------------------------------------------------
function queueAudio(arrayBuffer) {
  audioQueue.push(arrayBuffer);
  if (!isPlaying) playNext();
}

async function playNext() {
  if (audioQueue.length === 0) {
    isPlaying = false;
    currentSource = null;
    return;
  }

  isPlaying = true;
  const data = audioQueue.shift();

  try {
    if (!audioContext || audioContext.state === "closed") {
      audioContext = new AudioContext({ sampleRate: 24000 });
    }
    // Resume if suspended (autoplay policy)
    if (audioContext.state === "suspended") {
      await audioContext.resume();
    }

    const audioBuffer = await audioContext.decodeAudioData(data.slice(0));
    const source = audioContext.createBufferSource();
    source.buffer = audioBuffer;
    source.connect(audioContext.destination);
    source.onended = () => playNext();
    currentSource = source;
    source.start();
  } catch (e) {
    console.error("Audio decode error:", e);
    playNext();
  }
}

function stopAudio() {
  audioQueue = [];
  if (currentSource) {
    try { currentSource.stop(); } catch (_) {}
    currentSource = null;
  }
  isPlaying = false;
}

// ---------------------------------------------------------------------------
// Float32 → Int16 conversion
// ---------------------------------------------------------------------------
function float32ToInt16(float32Array) {
  const int16 = new Int16Array(float32Array.length);
  for (let i = 0; i < float32Array.length; i++) {
    const s = Math.max(-1, Math.min(1, float32Array[i]));
    int16[i] = s < 0 ? s * 0x8000 : s * 0x7fff;
  }
  return int16;
}

// ---------------------------------------------------------------------------
// Start / Stop toggle
// ---------------------------------------------------------------------------
async function toggleVoice() {
  if (running) {
    running = false;
    startBtn.textContent = "Start";
    startBtn.classList.remove("active");
    stopVAD();
    stopAudio();
    if (ws) ws.close();
    setStatus("idle");
  } else {
    running = true;
    startBtn.textContent = "Stop";
    startBtn.classList.add("active");
    connectWS();
    // Small delay to let WS connect before starting VAD
    setTimeout(() => startVAD(), 500);
    setStatus("idle");
  }
}
