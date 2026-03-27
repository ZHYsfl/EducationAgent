// ---------------------------------------------------------------------------
// DOM elements
// ---------------------------------------------------------------------------
const statusDot = document.getElementById("status-dot");
const statusText = document.getElementById("status-text");
const startBtn = document.getElementById("start-btn");
const testBtn = document.getElementById("test-btn");
const recordBtn = document.getElementById("record-btn");
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
let micAudioContext = null;
let playbackAudioContext = null;

let currentUserBubble = null;
let currentAIBubble = null;

// ---------------------------------------------------------------------------
// WebSocket
// ---------------------------------------------------------------------------
function getOrCreateUserId() {
  const params = new URLSearchParams(location.search);
  const fromQuery = params.get("user_id");
  if (fromQuery && fromQuery.trim()) {
    return fromQuery.trim();
  }
  let id = localStorage.getItem("voice_user_id");
  if (!id) {
    id = crypto.randomUUID();
    localStorage.setItem("voice_user_id", id);
  }
  return id;
}

function connectWS() {
  return new Promise((resolve, reject) => {
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const uid = encodeURIComponent(getOrCreateUserId());
    const socket = new WebSocket(`${proto}//${location.host}/ws?user_id=${uid}`);
    socket.binaryType = "arraybuffer";

    const timer = setTimeout(() => {
      socket.close();
      reject(new Error("WebSocket 连接超时(8s)"));
    }, 8000);

    socket.onopen = () => {
      clearTimeout(timer);
      ws = socket;
      logEvent("WS", "connected");
      resolve();
    };
    socket.onerror = () => {
      clearTimeout(timer);
      reject(new Error("WebSocket 连接失败"));
    };
    socket.onclose = () => {
      clearTimeout(timer);
      logEvent("WS", "disconnected");
      ws = null;
      if (running) {
        running = false;
        startBtn.textContent = "Start";
        startBtn.classList.remove("active");
        startBtn.disabled = false;
        stopVAD();
        stopAudio();
        setStatus("idle");
        logEvent("SYSTEM", "连接断开，请重新点 Start");
      }
    };
    socket.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        queueAudio(event.data);
      } else {
        handleServerMessage(JSON.parse(event.data));
      }
    };
  });
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

    case "requirements_summary":
      showRequirementsCard(msg);
      break;

    case "requirements_progress":
      updateRequirementsProgress(msg);
      break;

    case "task_created":
      hideRequirementsCard();
      addMessage("assistant", `课件创建成功：${msg.topic}`);
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
// Event log (auto-open so diagnostics are always visible)
// ---------------------------------------------------------------------------
function logEvent(type, detail) {
  const now = new Date();
  const ts = now.toTimeString().slice(0, 8) + "." + String(now.getMilliseconds()).padStart(3, "0");

  const line = document.createElement("div");
  line.className = "log-line";
  line.innerHTML = `<span class="ts">[${ts}]</span> <span class="evt">${type}</span> ${escapeHtml(detail || "")}`;
  eventLogEl.appendChild(line);
  eventLogEl.scrollTop = eventLogEl.scrollHeight;

  console.log(`[${type}] ${detail || ""}`);
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
  logEvent("VAD", "step1: checking vad_web library");
  if (typeof vad_web === "undefined") {
    throw new Error("vad_web 未加载 — CDN 脚本可能被墙，请检查网络");
  }
  if (typeof ort === "undefined") {
    throw new Error("onnxruntime 未加载 — CDN 脚本可能被墙，请检查网络");
  }

  logEvent("VAD", "step2: requesting mic permission");
  const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
  stream.getTracks().forEach((t) => t.stop());
  logEvent("VAD", "step3: mic permission granted");

  micAudioContext = new AudioContext({ sampleRate: 16000 });
  logEvent("VAD", "step4: AudioContext created (state=" + micAudioContext.state + ")");

  if (micAudioContext.state === "suspended") {
    await micAudioContext.resume();
    logEvent("VAD", "step4b: AudioContext resumed");
  }

  logEvent("VAD", "step5: initializing MicVAD (local model)");

  const vadConfig = {
    positiveSpeechThreshold: 0.22,
    negativeSpeechThreshold: 0.18,
    minSpeechFrames: 1,
    preSpeechPadFrames: 3,

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

    onFrameProcessed: (probs, frame) => {
      if (window._vadDebugCount == null) window._vadDebugCount = 0;
      window._vadDebugCount++;
      if (window._vadDebugCount % 100 === 0 && probs && typeof probs.isSpeech === "number") {
        logEvent("VAD_DEBUG", "prob=" + probs.isSpeech.toFixed(2) + " (speak to test)");
      }
      if (isSpeaking) {
        sendAudio(float32ToInt16(frame));
      } else {
        preSpeechBuffer.push(new Float32Array(frame));
        if (preSpeechBuffer.length > PRE_SPEECH_FRAMES) {
          preSpeechBuffer.shift();
        }
      }
    },
  };

  try {
    vad = await vad_web.MicVAD.new({
      ...vadConfig,
      modelURL: "/models/silero_vad.onnx",
    });
    logEvent("VAD", "step6: MicVAD created (local model)");
  } catch (e1) {
    logEvent("VAD_WARN", "local model failed: " + (e1.message || e1));
    logEvent("VAD", "step6b: retrying with CDN model");
    vad = await vad_web.MicVAD.new(vadConfig);
    logEvent("VAD", "step6c: MicVAD created (CDN model)");
  }

  vad.start();
  logEvent("VAD", "step7: MicVAD started — speak now!");
}

function stopVAD() {
  if (vad) {
    try { vad.destroy(); } catch (_) {}
    vad = null;
  }
  if (micAudioContext) {
    try { micAudioContext.close(); } catch (_) {}
    micAudioContext = null;
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
    if (!playbackAudioContext || playbackAudioContext.state === "closed") {
      playbackAudioContext = new AudioContext({ sampleRate: 24000 });
    }
    if (playbackAudioContext.state === "suspended") {
      await playbackAudioContext.resume();
    }

    const audioBuffer = await playbackAudioContext.decodeAudioData(data.slice(0));
    const source = playbackAudioContext.createBufferSource();
    source.buffer = audioBuffer;
    source.connect(playbackAudioContext.destination);
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
    startBtn.disabled = false;
    stopVAD();
    stopAudio();
    if (ws) { ws.close(); ws = null; }
    if (testBtn) testBtn.style.display = "none";
    if (recordBtn) recordBtn.style.display = "none";
    setStatus("idle");
    logEvent("SYSTEM", "stopped");
    return;
  }

  // Auto-open EVENT LOG so user can see diagnostics
  if (!eventLogEl.classList.contains("open")) {
    eventLogEl.classList.add("open");
    logArrow.classList.add("open");
  }

  startBtn.textContent = "Starting...";
  startBtn.classList.add("active");
  startBtn.disabled = true;

  try {
    logEvent("SYSTEM", "connecting...");
    await connectWS();

    logEvent("SYSTEM", "initializing VAD...");
    running = true;
    await startVAD();

    if (!running) return;
    startBtn.textContent = "Stop";
    startBtn.disabled = false;
    if (testBtn) testBtn.style.display = "inline-block";
    if (recordBtn) recordBtn.style.display = "inline-block";
    setStatus("idle");
    logEvent("SYSTEM", "✓ ready — speak now!");
  } catch (err) {
    const msg = err && err.message ? err.message : String(err);
    logEvent("ERROR", msg);
    console.error("toggleVoice error:", err);
    running = false;
    startBtn.textContent = "Start";
    startBtn.classList.remove("active");
    startBtn.disabled = false;
    stopVAD();
    stopAudio();
    if (ws) { ws.close(); ws = null; }
    setStatus("idle");
  }
}

// ---------------------------------------------------------------------------
// Simulate speech (bypass VAD for testing backend)
// ---------------------------------------------------------------------------
function simulateSpeech() {
  if (!ws || ws.readyState !== WebSocket.OPEN || !running) return;
  logEvent("TEST", "模拟说话 — 发送 vad_start");
  sendJSON({ type: "vad_start" });
  const sr = 16000;
  const dur = 1.5;
  const samples = Math.floor(sr * dur);
  const buf = new Int16Array(samples);
  for (let i = 0; i < samples; i++) {
    buf[i] = Math.sin(2 * Math.PI * 440 * i / sr) * 8000;
  }
  const chunk = 3200;
  for (let i = 0; i < buf.length; i += chunk) {
    sendAudio(buf.subarray(i, Math.min(i + chunk, buf.length)));
  }
  setTimeout(() => {
    logEvent("TEST", "模拟说话 — 发送 vad_end");
    sendJSON({ type: "vad_end" });
  }, 1800);
}

async function recordAndSend() {
  if (!ws || ws.readyState !== WebSocket.OPEN || !running) return;
  try {
    logEvent("RECORD", "开始录制 3 秒，请说话...");
    recordBtn.disabled = true;
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const ctx = new AudioContext({ sampleRate: 16000 });
    const src = ctx.createMediaStreamSource(stream);
    const processor = ctx.createScriptProcessor(4096, 1, 1);

    sendJSON({ type: "vad_start" });
    processor.onaudioprocess = (e) => {
      const f32 = new Float32Array(e.inputBuffer.getChannelData(0));
      sendAudio(float32ToInt16(f32));
    };
    src.connect(processor);
    processor.connect(ctx.destination);

    await new Promise((r) => setTimeout(r, 3000));
    processor.disconnect();
    src.disconnect();
    stream.getTracks().forEach((t) => t.stop());
    ctx.close();

    sendJSON({ type: "vad_end" });
    logEvent("RECORD", "已发送，等待识别...");
  } catch (e) {
    logEvent("RECORD_ERR", e.message || e);
  } finally {
    recordBtn.disabled = false;
  }
}

// ---------------------------------------------------------------------------
// Page load: check CDN readiness
// ---------------------------------------------------------------------------
window.addEventListener("load", () => {
  const cdnOk = typeof ort !== "undefined" && typeof vad_web !== "undefined";
  logEvent("INIT", cdnOk
    ? "CDN loaded (ort + vad_web)"
    : "CDN NOT loaded! ort=" + (typeof ort) + " vad_web=" + (typeof vad_web));
  if (!cdnOk) {
    chatEmpty.textContent = "CDN 资源未加载，请检查网络连接后刷新页面";
    startBtn.disabled = true;
  }
});

// ---------------------------------------------------------------------------
// Requirements Card Functions
// ---------------------------------------------------------------------------
function showRequirementsCard(data) {
  const card = document.getElementById('requirements-card');
  const fields = {
    'req-topic': data.topic,
    'req-subject': data.subject,
    'req-audience': data.target_audience,
    'req-pages': data.page_count,
    'req-difficulty': data.difficulty_level,
    'req-style': data.global_style,
    'req-focus': data.key_points,
    'req-examples': data.case_studies,
    'req-interactive': data.interactive_elements,
    'req-visual': data.visual_preferences,
    'req-duration': data.duration_minutes,
    'req-notes': data.additional_notes
  };

  for (const [id, value] of Object.entries(fields)) {
    const elem = document.getElementById(id);
    if (elem) elem.textContent = value || '未指定';
  }

  card.classList.remove('hidden');
}

function updateRequirementsProgress(data) {
  const indicator = document.querySelector('.progress-indicator');
  if (indicator && data.message) {
    indicator.textContent = data.message;
  }
}

function hideRequirementsCard() {
  const card = document.getElementById('requirements-card');
  card.classList.add('hidden');
}

function addMessage(role, text) {
  chatEmpty.style.display = "none";
  const block = createMsgBlock(role === "assistant" ? "ai" : "user", role === "assistant" ? "AI" : "You");
  block.textEl.textContent = text;
  chatContainer.appendChild(block.el);
  scrollChat();
}

