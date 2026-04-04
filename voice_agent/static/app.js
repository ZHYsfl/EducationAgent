// ============================================================================
// State Management
// ============================================================================
const state = {
  ws: null,
  vad: null,
  running: false,
  isSpeaking: false,
  audioQueue: [],
  isPlaying: false,
  currentSource: null,
  micAudioContext: null,
  playbackAudioContext: null,
  currentUserBubble: null,
  currentAIBubble: null,
  preSpeechBuffer: [],
};

const PRE_SPEECH_FRAMES = 11;

// ============================================================================
// DOM Elements
// ============================================================================
const dom = {
  statusDot: document.getElementById('status-dot'),
  statusText: document.getElementById('status-text'),
  startBtn: document.getElementById('start-btn'),
  chatContainer: document.getElementById('chat-container'),
  chatEmpty: document.getElementById('chat-empty'),
  requirementsModal: document.getElementById('requirements-modal'),
  progressText: document.getElementById('progress-text'),
  logToggle: document.getElementById('log-toggle'),
  logArrow: document.getElementById('log-arrow'),
  logContent: document.getElementById('log-content'),
};

// ============================================================================
// WebSocket Module
// ============================================================================
const WebSocketModule = {
  getUserId() {
    const params = new URLSearchParams(location.search);
    const fromQuery = params.get('user_id');
    if (fromQuery?.trim()) return fromQuery.trim();

    let id = localStorage.getItem('voice_user_id');
    if (!id) {
      id = crypto.randomUUID();
      localStorage.setItem('voice_user_id', id);
    }
    return id;
  },

  connect() {
    return new Promise((resolve, reject) => {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const uid = encodeURIComponent(this.getUserId());
      const socket = new WebSocket(`${proto}//${location.host}/ws?user_id=${uid}`);
      socket.binaryType = 'arraybuffer';

      const timer = setTimeout(() => {
        socket.close();
        reject(new Error('WebSocket connection timeout'));
      }, 8000);

      socket.onopen = () => {
        clearTimeout(timer);
        state.ws = socket;
        Logger.log('WS', 'connected');
        resolve();
      };

      socket.onerror = () => {
        clearTimeout(timer);
        reject(new Error('WebSocket connection failed'));
      };

      socket.onclose = () => {
        clearTimeout(timer);
        Logger.log('WS', 'disconnected');
        state.ws = null;
        if (state.running) {
          state.running = false;
          dom.startBtn.textContent = 'Start';
          dom.startBtn.classList.remove('active');
          VADModule.stop();
          AudioModule.stopPlayback();
          UI.setStatus('idle');
        }
      };

      socket.onmessage = (event) => {
        if (event.data instanceof ArrayBuffer) {
          AudioModule.queueAudio(event.data);
        } else {
          MessageHandler.handle(JSON.parse(event.data));
        }
      };
    });
  },

  send(obj) {
    if (state.ws?.readyState === WebSocket.OPEN) {
      state.ws.send(JSON.stringify(obj));
    }
  },

  sendAudio(int16Array) {
    if (state.ws?.readyState === WebSocket.OPEN) {
      state.ws.send(int16Array.buffer);
    }
  },
};

// ============================================================================
// Message Handler
// ============================================================================
const MessageHandler = {
  handle(msg) {
    switch (msg.type) {
      case 'status':
        UI.setStatus(msg.state);
        Logger.log('STATUS', msg.state);
        if (msg.state === 'listening') UI.startUserBubble();
        if (msg.state === 'processing') UI.startAIBubble();
        break;
      case 'transcript':
        UI.updateUserBubble(msg.text);
        Logger.log('ASR_PARTIAL', this.truncate(msg.text, 60));
        break;
      case 'transcript_final':
        UI.finalizeUserBubble(msg.text);
        Logger.log('ASR_FINAL', this.truncate(msg.text, 60));
        break;
      case 'response':
        UI.appendAIBubble(msg.text);
        break;
      case 'requirements_progress':
        RequirementsModule.updateProgress(msg);
        break;
      case 'task_created':
        RequirementsModule.hideModal();
        UI.addMessage('ai', `课件创建成功：${msg.topic}`);
        break;
    }
  },

  truncate(text, maxLen) {
    return text.length > maxLen ? text.slice(0, maxLen) + '...' : text;
  },
};

// ============================================================================
// UI Module
// ============================================================================
const UI = {
  setStatus(state) {
    dom.statusDot.className = `status-dot ${state}`;
    dom.statusText.textContent = state.charAt(0).toUpperCase() + state.slice(1);
    if (state === 'listening') AudioModule.stopPlayback();
  },

  startUserBubble() {
    dom.chatEmpty.style.display = 'none';
    const block = this.createMessageBlock('user', 'You');
    dom.chatContainer.appendChild(block.el);
    state.currentUserBubble = block;
    this.scrollChat();
  },

  updateUserBubble(text) {
    if (state.currentUserBubble) {
      state.currentUserBubble.textEl.textContent = text;
      this.scrollChat();
    }
  },

  finalizeUserBubble(text) {
    if (state.currentUserBubble) {
      state.currentUserBubble.textEl.textContent = text;
      state.currentUserBubble.textEl.classList.add('finalizing');
      setTimeout(() => state.currentUserBubble.textEl.classList.remove('finalizing'), 400);
      state.currentUserBubble = null;
    }
  },

  startAIBubble() {
    const block = this.createMessageBlock('ai', 'AI');
    dom.chatContainer.appendChild(block.el);
    state.currentAIBubble = block;
    this.scrollChat();
  },

  appendAIBubble(text) {
    if (state.currentAIBubble) {
      state.currentAIBubble.textEl.textContent += text;
      this.scrollChat();
    }
  },

  addMessage(role, text) {
    dom.chatEmpty.style.display = 'none';
    const block = this.createMessageBlock(role === 'ai' ? 'ai' : 'user', role === 'ai' ? 'AI' : 'You');
    block.textEl.textContent = text;
    dom.chatContainer.appendChild(block.el);
    this.scrollChat();
  },

  createMessageBlock(type, label) {
    const el = document.createElement('div');
    el.className = 'msg-block';
    
    const indicator = document.createElement('div');
    indicator.className = `msg-indicator ${type}`;
    
    const content = document.createElement('div');
    content.className = 'msg-content';
    
    const labelEl = document.createElement('div');
    labelEl.className = 'msg-label';
    labelEl.textContent = label;
    
    const textEl = document.createElement('div');
    textEl.className = 'msg-text';
    
    content.appendChild(labelEl);
    content.appendChild(textEl);
    el.appendChild(indicator);
    el.appendChild(content);
    
    return { el, textEl };
  },

  scrollChat() {
    dom.chatContainer.scrollTop = dom.chatContainer.scrollHeight;
  },
};

// ============================================================================
// Requirements Module
// ============================================================================
const RequirementsModule = {
  updateProgress(data) {
    const collected = data.collected_fields?.length || 0;
    const missing = data.missing_fields?.length || 0;
    const total = collected + missing;
    dom.progressText.textContent = `已收集 ${collected}/${total} 个字段`;

    if (data.status === 'ready' && data.requirements) {
      this.showModal(data.requirements);
    }
  },

  showModal(req) {
    document.getElementById('req-topic').textContent = req.topic || '未指定';
    document.getElementById('req-subject').textContent = req.subject || '未指定';
    document.getElementById('req-audience').textContent = req.target_audience || '未指定';
    document.getElementById('req-pages').textContent = req.total_pages || '未指定';
    document.getElementById('req-focus').textContent = req.knowledge_points?.join('、') || '未指定';
    document.getElementById('req-goals').textContent = req.teaching_goals?.join('、') || '未指定';
    document.getElementById('req-logic').textContent = req.teaching_logic || '未指定';
    document.getElementById('req-difficulty').textContent = req.key_difficulties?.join('、') || '未指定';
    document.getElementById('req-duration').textContent = req.duration || '未指定';
    document.getElementById('req-style').textContent = req.global_style || '未指定';
    document.getElementById('req-interactive').textContent = req.interaction_design || '未指定';
    document.getElementById('req-formats').textContent = req.output_formats?.join('、') || '未指定';
    document.getElementById('req-notes').textContent = req.additional_notes || '未指定';

    dom.requirementsModal.classList.remove('hidden');
  },

  hideModal() {
    dom.requirementsModal.classList.add('hidden');
  },
};

// ============================================================================
// Audio Module
// ============================================================================
const AudioModule = {
  queueAudio(arrayBuffer) {
    state.audioQueue.push(arrayBuffer);
    if (!state.isPlaying) this.playNext();
  },

  async playNext() {
    if (state.audioQueue.length === 0) {
      state.isPlaying = false;
      return;
    }

    state.isPlaying = true;
    const buf = state.audioQueue.shift();

    if (!state.playbackAudioContext) {
      state.playbackAudioContext = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 24000 });
    }

    const ctx = state.playbackAudioContext;
    const pcm = new Int16Array(buf);
    const float32 = new Float32Array(pcm.length);
    for (let i = 0; i < pcm.length; i++) {
      float32[i] = pcm[i] / 32768.0;
    }

    const audioBuf = ctx.createBuffer(1, float32.length, 24000);
    audioBuf.getChannelData(0).set(float32);

    const source = ctx.createBufferSource();
    source.buffer = audioBuf;
    source.connect(ctx.destination);
    state.currentSource = source;

    source.onended = () => {
      state.currentSource = null;
      this.playNext();
    };

    source.start(0);
  },

  stopPlayback() {
    if (state.currentSource) {
      state.currentSource.stop();
      state.currentSource = null;
    }
    state.audioQueue = [];
    state.isPlaying = false;
  },
};

// ============================================================================
// VAD Module
// ============================================================================
const VADModule = {
  async init() {
    if (!state.micAudioContext) {
      state.micAudioContext = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 16000 });
    }

    const stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, sampleRate: 16000 } });
    const myvad = await window.vad_web.MicVAD.new({
      stream,
      positiveSpeechThreshold: 0.8,
      negativeSpeechThreshold: 0.8 - 0.15,
      redemptionFrames: 8,
      preSpeechPadFrames: 1,
      minSpeechFrames: 3,
      onSpeechStart: () => {
        state.preSpeechBuffer = [];
        WebSocketModule.send({ type: 'vad_start' });
      },
      onSpeechEnd: () => {
        WebSocketModule.send({ type: 'vad_end' });
      },
      onFrameProcessed: (probs) => {
        const frame = probs.audio;
        if (probs.isSpeech) {
          WebSocketModule.sendAudio(frame);
        } else {
          state.preSpeechBuffer.push(frame);
          if (state.preSpeechBuffer.length > PRE_SPEECH_FRAMES) {
            state.preSpeechBuffer.shift();
          }
        }
      },
    });

    state.vad = myvad;
    myvad.start();
  },

  stop() {
    if (state.vad) {
      state.vad.pause();
      state.vad = null;
    }
  },
};

// ============================================================================
// Logger Module
// ============================================================================
const Logger = {
  log(event, detail = '') {
    const now = new Date();
    const ts = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}:${String(now.getSeconds()).padStart(2, '0')}`;
    
    const line = document.createElement('div');
    line.className = 'log-line';
    line.innerHTML = `<span class="ts">${ts}</span> <span class="evt">${event}</span> ${detail}`;
    
    dom.logContent.appendChild(line);
    dom.logContent.scrollTop = dom.logContent.scrollHeight;
  },
};

// ============================================================================
// Main Controller
// ============================================================================
const App = {
  async start() {
    if (state.running) {
      this.stop();
      return;
    }

    try {
      dom.startBtn.disabled = true;
      dom.startBtn.textContent = 'Connecting...';

      await WebSocketModule.connect();
      await VADModule.init();

      state.running = true;
      dom.startBtn.textContent = 'Stop';
      dom.startBtn.classList.add('active');
      dom.startBtn.disabled = false;
      UI.setStatus('idle');
    } catch (err) {
      Logger.log('ERROR', err.message);
      dom.startBtn.textContent = 'Start';
      dom.startBtn.disabled = false;
      alert('启动失败: ' + err.message);
    }
  },

  stop() {
    state.running = false;
    dom.startBtn.textContent = 'Start';
    dom.startBtn.classList.remove('active');
    VADModule.stop();
    AudioModule.stopPlayback();
    if (state.ws) state.ws.close();
    UI.setStatus('idle');
  },

  toggleLog() {
    dom.logContent.classList.toggle('hidden');
    dom.logArrow.classList.toggle('open');
  },
};

// ============================================================================
// Event Listeners
// ============================================================================
dom.startBtn.addEventListener('click', () => App.start());
dom.logToggle.addEventListener('click', () => App.toggleLog());

// ============================================================================
// Initialization
// ============================================================================
window.addEventListener('load', () => {
  const cdnOk = typeof ort !== 'undefined' && typeof vad_web !== 'undefined';
  Logger.log('INIT', cdnOk ? 'CDN loaded' : 'CDN failed');
  
  if (!cdnOk) {
    dom.chatEmpty.innerHTML = '<p>CDN 资源未加载，请检查网络连接后刷新页面</p>';
    dom.startBtn.disabled = true;
  }
});
