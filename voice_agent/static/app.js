// ---------------------------------------------------------------------------
// DOM elements
// ---------------------------------------------------------------------------
const statusDot = document.getElementById("status-dot");
const statusText = document.getElementById("status-text");
const startBtn = document.getElementById("start-btn");
const chatContainer = document.getElementById("chat-container");
const chatEmpty = document.getElementById("chat-empty");
const eventLogEl = document.getElementById("event-log");
const logArrow = document.getElementById("log-arrow");

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------
let ws = null;
let vad = null;
let running = false;
let isSpeaking = false;

const PRE_SPEECH_FRAMES = 11;
let preSpeechBuffer = [];

let audioQueue = [];
let isPlaying = false;
let currentSource = null;
let audioContext = null;

let currentUserBubble = null;
let currentAIBubble = null;

// ---------------------------------------------------------------------------
// WebSocket
// ---------------------------------------------------------------------------
function connectWS() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  ws = new WebSocket(`${proto}//${location.host}/ws`);
  ws.binaryType = "arraybuffer";

  ws.onopen = () => logEvent("WS", "connected");
  ws.onclose = () => {
    logEvent("WS", "disconnected");
    if (running) setTimeout(connectWS, 2000);
  };
  ws.onerror = (e) => logEvent("WS_ERROR", e.type);

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
      logEvent("STATUS", msg.state);
      if (msg.state === "listening") {
        startNewUserBubble();
      }
      if (msg.state === "processing") {
        startNewAIBubble();
      }
      break;

    case "transcript":
      updateCurrentUserBubble(msg.text);
      logEvent("ASR_PARTIAL", truncateLog(msg.text, 60));
      break;

    case "transcript_final":
      replaceCurrentUserBubble(msg.text);
      logEvent("ASR_FINAL", truncateLog(msg.text, 60));
      break;

    case "response":
      appendCurrentAIBubble(msg.text);
      break;
  }
}

function setStatus(state) {
  statusDot.className = "status-dot " + state;
  statusText.textContent = state.toUpperCase();

  if (state === "listening") {
    stopAudio();
  }
}

// ---------------------------------------------------------------------------
// Chat bubble management
// ---------------------------------------------------------------------------
function startNewUserBubble() {
  chatEmpty.style.display = "none";
  const block = createMsgBlock("user", "You");
  chatContainer.appendChild(block.el);
  currentUserBubble = block;
  scrollChat();
}

function startNewAIBubble() {
  const block = createMsgBlock("ai", "AI");
  chatContainer.appendChild(block.el);
  currentAIBubble = block;
  scrollChat();
}

function updateCurrentUserBubble(text) {
  if (!currentUserBubble) startNewUserBubble();
  currentUserBubble.textEl.textContent = text;
  scrollChat();
}

function replaceCurrentUserBubble(text) {
  if (!currentUserBubble) startNewUserBubble();
  currentUserBubble.textEl.textContent = text;
  currentUserBubble.textEl.classList.add("finalizing");
  setTimeout(() => currentUserBubble.textEl.classList.remove("finalizing"), 500);
  scrollChat();
}

function appendCurrentAIBubble(text) {
  if (!currentAIBubble) startNewAIBubble();
  currentAIBubble.textEl.textContent += text;
  scrollChat();
}

function createMsgBlock(role, label) {
  const el = document.createElement("div");
  el.className = "msg-block";

  const bar = document.createElement("div");
  bar.className = "msg-bar " + role;

  const body = document.createElement("div");
  body.className = "msg-body";

  const labelEl = document.createElement("div");
  labelEl.className = "msg-label";
  labelEl.textContent = label;

  const textEl = document.createElement("div");
  textEl.className = "msg-text";

  body.appendChild(labelEl);
  body.appendChild(textEl);
  el.appendChild(bar);
  el.appendChild(body);

  return { el, textEl };
}

function scrollChat() {
  chatContainer.scrollTop = chatContainer.scrollHeight;
}

// ---------------------------------------------------------------------------
// Event log
// ---------------------------------------------------------------------------
function logEvent(type, detail) {
  const now = new Date();
  const ts = now.toTimeString().slice(0, 8) + "." + String(now.getMilliseconds()).padStart(3, "0");

  const line = document.createElement("div");
  line.className = "log-line";
  line.innerHTML = `<span class="ts">[${ts}]</span> <span class="evt">${type}</span> ${escapeHtml(detail || "")}`;
  eventLogEl.appendChild(line);
  eventLogEl.scrollTop = eventLogEl.scrollHeight;
}

function toggleLog() {
  eventLogEl.classList.toggle("open");
  logArrow.classList.toggle("open");
}

function escapeHtml(s) {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function truncateLog(s, max) {
  if (s.length <= max) return s;
  return s.slice(0, max) + "...";
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
    preSpeechPadFrames: 0,

    onSpeechStart: () => {
      isSpeaking = true;
      sendJSON({ type: "vad_start" });
      logEvent("VAD", "speech_start");
      for (const frame of preSpeechBuffer) {
        sendAudio(float32ToInt16(frame));
      }
      preSpeechBuffer = [];
    },

    onSpeechEnd: (_audio) => {
      isSpeaking = false;
      sendJSON({ type: "vad_end" });
      logEvent("VAD", "speech_end");
    },

    onFrameProcessed: (_probs, frame) => {
      if (isSpeaking) {
        sendAudio(float32ToInt16(frame));
      } else {
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
// Float32 -> Int16 conversion
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
    logEvent("SYSTEM", "stopped");
  } else {
    running = true;
    startBtn.textContent = "Stop";
    startBtn.classList.add("active");
    connectWS();
    setTimeout(() => startVAD(), 500);
    setStatus("idle");
    logEvent("SYSTEM", "started");
  }
}
