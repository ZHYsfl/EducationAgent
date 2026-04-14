(function () {
  const EMPTY_REQUIREMENTS = {
    topic: "",
    description: "",
    audience: "",
    knowledge_points: [],
    teaching_goals: [],
    teaching_logic: "",
    key_difficulties: [],
    duration: "",
    total_pages: 0,
    global_style: "",
    interaction_design: "",
    output_formats: [],
    additional_notes: "",
    collected_fields: [],
    status: "collecting",
  };

  const state = {
    userId: getOrCreateUserId(),
    ws: null,
    connection: "offline",
    sessionStatus: "idle",
    taskStatus: "idle",
    micReady: false,
    micSpeaking: false,
    micError: "",
    notice: "等待连接。",
    requirements: { ...EMPTY_REQUIREMENTS },
    collectedFields: [],
    missingFields: [
      "topic",
      "description",
      "audience",
      "knowledge_points",
      "teaching_goals",
      "teaching_logic",
      "key_difficulties",
      "duration",
      "total_pages",
      "global_style",
      "interaction_design",
      "output_formats",
    ],
    summaryText: "",
    conflictQuestion: "",
    referenceFiles: [],
    selectedFile: null,
    latestUserText: "",
    latestAssistantText: "",
    messages: [],
    activeTaskId: "",
    tasks: {},
    previewPages: [],
    currentViewingPageId: "",
    exportUrl: "",
    exportFormat: "",
    previewTimer: null,
    uploadBusy: false,
    audioQueue: [],
    audioPlaying: false,
    playbackContext: null,
    playbackSource: null,
    playbackRemainder: new Uint8Array(0),
    micContext: null,
    micStream: null,
    micSource: null,
    micProcessor: null,
    micSink: null,
    preSpeechChunks: [],
    speechStarted: false,
    silenceSince: 0,
    isMicCapturing: false,
  };

  const dom = {
    landingScreen: byId("landing-screen"),
    conversationScreen: byId("conversation-screen"),
    renderingScreen: byId("rendering-screen"),
    exportScreen: byId("export-screen"),
    connectionBadge: byId("connection-badge"),
    sessionBadge: byId("session-badge"),
    micBadge: byId("mic-badge"),
    taskBadge: byId("task-badge"),
    connectBtn: byId("connect-btn"),
    disconnectBtn: byId("disconnect-btn"),
    toggleMicBtn: byId("toggle-mic-btn"),
    sendTextBtn: byId("send-text-btn"),
    uploadBtn: byId("upload-btn"),
    attachBtn: byId("attach-btn"),
    fileInput: byId("file-input"),
    fileInstruction: byId("file-instruction"),
    fileName: byId("file-name"),
    fileSize: byId("file-size"),
    uploadPill: byId("upload-pill"),
    referenceFiles: byId("reference-files"),
    referenceFilesSide: byId("reference-files-side"),
    conversationFeed: byId("conversation-feed"),
    assistantOrb: byId("assistant-orb"),
    liveCaptionText: byId("live-caption-text"),
    textInput: byId("text-input"),
    noticeText: byId("notice-text"),
    latestUserText: byId("latest-user-text"),
    latestAssistantText: byId("latest-assistant-text"),
    collectedFieldsText: byId("collected-fields-text"),
    missingFieldsText: byId("missing-fields-text"),
    requirementsProgressText: byId("requirements-progress-text"),
    taskPills: byId("task-pills"),
    userIdText: byId("user-id-text"),
    taskTitleText: byId("task-title-text"),
    taskSubtitleText: byId("task-subtitle-text"),
    renderProgressText: byId("render-progress-text"),
    renderRatioText: byId("render-ratio-text"),
    progressBar: byId("progress-bar"),
    slideGrid: byId("slide-grid"),
    previewImage: byId("preview-image"),
    previewPlaceholder: byId("preview-placeholder"),
    requirementsTopicText: byId("requirements-topic-text"),
    requirementsPagesText: byId("requirements-pages-text"),
    requirementsAudienceText: byId("requirements-audience-text"),
    requirementsStyleText: byId("requirements-style-text"),
    requirementsDurationText: byId("requirements-duration-text"),
    knowledgeList: byId("knowledge-list"),
    goalsList: byId("goals-list"),
    successMark: byId("success-mark"),
    exportDescription: byId("export-description"),
    exportTaskId: byId("export-task-id"),
    exportFormatText: byId("export-format-text"),
    exportTopicText: byId("export-topic-text"),
    downloadLink: byId("download-link"),
    backToTaskBtn: byId("back-to-task-btn"),
    requirementsModal: byId("requirements-modal"),
    modalTitle: byId("modal-title"),
    modalCopy: byId("modal-copy"),
    modalTopic: byId("modal-topic"),
    modalDescription: byId("modal-description"),
    modalAudience: byId("modal-audience"),
    modalPages: byId("modal-pages"),
    modalStyle: byId("modal-style"),
    modalFormat: byId("modal-format"),
    modalSummary: byId("modal-summary"),
    modalConflict: byId("modal-conflict"),
    modalLockText: byId("modal-lock-text"),
    logToggle: byId("log-toggle"),
    logContent: byId("log-content"),
  };

  function byId(id) {
    return document.getElementById(id);
  }

  function getOrCreateUserId() {
    const params = new URLSearchParams(window.location.search);
    const fromQuery = params.get("user_id");
    if (fromQuery && fromQuery.trim()) {
      localStorage.setItem("voice_agent_v2_user_id", fromQuery.trim());
      return fromQuery.trim();
    }
    const cached = localStorage.getItem("voice_agent_v2_user_id");
    if (cached) {
      return cached;
    }
    const created = window.crypto?.randomUUID ? window.crypto.randomUUID() : `user_${Date.now()}`;
    localStorage.setItem("voice_agent_v2_user_id", created);
    return created;
  }

  function logEvent(label, detail) {
    const line = document.createElement("div");
    const now = new Date().toLocaleTimeString("zh-CN", { hour12: false });
    line.textContent = `[${now}] ${label}${detail ? `  ${detail}` : ""}`;
    dom.logContent.prepend(line);
  }

  function setNotice(text) {
    state.notice = text;
    renderMeta();
  }

  function setScreen(mode) {
    dom.landingScreen.classList.toggle("hidden", mode !== "landing");
    dom.conversationScreen.classList.toggle("hidden", mode !== "conversation");
    dom.renderingScreen.classList.toggle("hidden", mode !== "rendering");
    dom.exportScreen.classList.toggle("hidden", mode !== "export");
  }

  function computeScreenMode() {
    if (state.exportUrl) return "export";
    if (state.activeTaskId) return "rendering";
    if (state.connection === "live" || state.messages.length > 0) return "conversation";
    return "landing";
  }

  function statusText(value) {
    const labels = {
      offline: "离线",
      connecting: "连接中",
      live: "在线",
      idle: "空闲",
      listening: "监听中",
      processing: "处理中",
      speaking: "播报中",
      ready: "就绪",
      rendering: "渲染中",
      completed: "已完成",
      ready_to_export: "可导出",
      error: "异常",
      queued: "排队中",
    };
    return labels[value] || value || "未知";
  }

  function normalizeRequirements(input) {
    return {
      ...EMPTY_REQUIREMENTS,
      ...input,
      knowledge_points: Array.isArray(input?.knowledge_points) ? input.knowledge_points : [],
      teaching_goals: Array.isArray(input?.teaching_goals) ? input.teaching_goals : [],
      key_difficulties: Array.isArray(input?.key_difficulties) ? input.key_difficulties : [],
      output_formats: Array.isArray(input?.output_formats) ? input.output_formats : [],
      collected_fields: Array.isArray(input?.collected_fields) ? input.collected_fields : [],
    };
  }

  function formatBytes(size) {
    if (!size) return "大小未知";
    if (size < 1024) return `${size} B`;
    if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`;
    return `${(size / 1024 / 1024).toFixed(1)} MB`;
  }

  function escapeHtml(text) {
    return String(text)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;")
      .replace(/'/g, "&#39;");
  }

  function upsertMessage(role, text, options) {
    const live = Boolean(options?.live);
    const kind = options?.kind || role;
    const liveId = `${kind}-live`;
    const existingIndex = live ? state.messages.findIndex((item) => item.id === liveId) : -1;
    const message = {
      id: live ? liveId : `${kind}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`,
      role,
      kind,
      live,
      text,
      at: new Date().toISOString(),
    };

    if (existingIndex >= 0) {
      state.messages[existingIndex] = { ...state.messages[existingIndex], text };
    } else {
      state.messages.push(message);
    }

    if (!live) {
      state.messages = state.messages.filter((item) => item.id !== liveId);
    }
    if (state.messages.length > 60) {
      state.messages = state.messages.slice(-60);
    }
    renderMessages();
  }

  function finalizeLiveMessage(kind) {
    const liveId = `${kind}-live`;
    const index = state.messages.findIndex((item) => item.id === liveId);
    if (index < 0) return;
    state.messages[index] = {
      ...state.messages[index],
      id: `${kind}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`,
      live: false,
      at: new Date().toISOString(),
    };
    renderMessages();
  }

  function renderMessages() {
    if (!state.messages.length) {
      dom.conversationFeed.innerHTML = '<div class="empty-state">会话建立后，这里会显示语音转写、模型响应以及任务相关事件。</div>';
      return;
    }

    dom.conversationFeed.innerHTML = "";
    state.messages.slice(-10).forEach((message) => {
      const article = document.createElement("article");
      const visualRole = message.role === "assistant" ? "assistant" : message.role === "user" ? "user" : "system";
      article.className = `dialog-card dialog-${visualRole}${message.live ? " dialog-live" : ""}`;

      const meta = document.createElement("div");
      meta.className = "dialog-meta";
      meta.innerHTML = `
        <span>${visualRole === "assistant" ? "AI 助手" : visualRole === "user" ? "用户" : "系统"}</span>
        <span>${new Date(message.at).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</span>
      `;

      const body = document.createElement("p");
      body.textContent = message.text || "...";
      article.appendChild(meta);
      article.appendChild(body);
      dom.conversationFeed.appendChild(article);
    });
    dom.conversationFeed.scrollTop = dom.conversationFeed.scrollHeight;
  }

  function renderReferenceFiles() {
    const html = state.referenceFiles.length
      ? state.referenceFiles.map((file) => {
          const name = escapeHtml(file.filename || file.file_id || "未命名文件");
          const meta = escapeHtml(`${file.file_type || "file"} · ${formatBytes(file.file_size || 0)}`);
          const instruction = escapeHtml(file.instruction || "未附加说明");
          return `<div class="file-chip"><strong>${name}</strong><small>${meta}</small><small>${instruction}</small></div>`;
        }).join("")
      : '<div class="empty-state compact-empty">尚未上传参考材料。</div>';

    dom.referenceFiles.innerHTML = html;
    dom.referenceFilesSide.innerHTML = html;
    dom.uploadPill.textContent = state.referenceFiles.length ? `已上传 ${state.referenceFiles.length} 份` : "待上传";
  }

  function renderChipList(items) {
    if (!items || !items.length) {
      return '<span class="empty-inline">暂无</span>';
    }
    return items.map((item) => `<span class="knowledge-chip">${escapeHtml(item)}</span>`).join("");
  }

  function renderRequirements() {
    const req = state.requirements;
    dom.requirementsTopicText.textContent = req.topic || "待采集";
    dom.requirementsPagesText.textContent = req.total_pages ? `${req.total_pages} 页` : "待定";
    dom.requirementsAudienceText.textContent = req.audience || "待定";
    dom.requirementsStyleText.textContent = req.global_style || "待定";
    dom.requirementsDurationText.textContent = req.duration || "待定";
    dom.knowledgeList.innerHTML = renderChipList(req.knowledge_points);
    dom.goalsList.innerHTML = renderChipList(req.teaching_goals);

    dom.modalTopic.textContent = req.topic || "待定";
    dom.modalDescription.textContent = req.description || "待定";
    dom.modalAudience.textContent = req.audience || "待定";
    dom.modalPages.textContent = req.total_pages ? `${req.total_pages} 页` : "待定";
    dom.modalStyle.textContent = req.global_style || "待定";
    dom.modalFormat.textContent = req.output_formats.length ? req.output_formats.join(" / ") : "待定";
    dom.modalSummary.textContent = state.summaryText || "等待摘要...";
    dom.modalConflict.innerHTML = state.conflictQuestion
      ? `<div class="sheet-item"><span>冲突问题</span><strong>${escapeHtml(state.conflictQuestion)}</strong></div>`
      : '<div class="empty-state compact-empty">当前没有冲突问题。</div>';

    const showModal = req.status === "ready" || Boolean(state.conflictQuestion);
    dom.requirementsModal.classList.toggle("hidden", !showModal);
    dom.modalTitle.textContent = state.conflictQuestion ? "当前存在冲突待确认" : "请核对当前采集结果";
    dom.modalCopy.textContent = state.conflictQuestion
      ? "后端返回了冲突问题，请继续通过语音或文本作答。"
      : "需求字段已基本齐全，页面会持续展示摘要和关键参数。";
    dom.modalLockText.textContent = state.missingFields.length
      ? `仍有 ${state.missingFields.length} 项字段待补充，继续输入即可。`
      : "需求已齐备，等待任务初始化与后续渲染结果。";
  }

  function renderTasks() {
    const ids = Object.keys(state.tasks);
    if (!ids.length) {
      dom.taskPills.innerHTML = '<span class="empty-inline">尚未创建任务</span>';
      return;
    }

    dom.taskPills.innerHTML = "";
    ids.forEach((taskId) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `task-pill${taskId === state.activeTaskId ? " active" : ""}`;
      button.textContent = state.tasks[taskId] || taskId;
      button.addEventListener("click", () => activateTask(taskId));
      dom.taskPills.appendChild(button);
    });
  }

  function renderUploadSelection() {
    if (!state.selectedFile) {
      dom.fileName.textContent = "未选择文件";
      dom.fileSize.textContent = "支持 PPT / PPTX / PDF / DOC / 图片";
      dom.uploadBtn.disabled = true;
      return;
    }
    dom.fileName.textContent = state.selectedFile.name;
    dom.fileSize.textContent = formatBytes(state.selectedFile.size);
    dom.uploadBtn.disabled = state.uploadBusy;
  }

  function renderPreview() {
    const pages = state.previewPages;
    const total = Math.max(pages.length, state.requirements.total_pages || 0);
    const completed = pages.filter((item) => item.status === "completed").length;
    const progress = total ? Math.round((completed / total) * 100) : 0;

    dom.taskTitleText.textContent = state.tasks[state.activeTaskId] || state.requirements.topic || "正在等待任务创建";
    dom.taskSubtitleText.textContent = state.activeTaskId
      ? `任务 ${state.activeTaskId} 正在同步预览状态。`
      : "任务创建后会自动轮询预览状态。";
    dom.renderProgressText.textContent = `${progress}%`;
    dom.renderRatioText.textContent = `${completed} / ${total || 0} 页`;
    dom.progressBar.style.width = `${progress}%`;

    if (!pages.length) {
      dom.slideGrid.innerHTML = '<div class="empty-state compact-empty">暂无预览页数据。</div>';
      dom.previewImage.classList.add("hidden");
      dom.previewPlaceholder.classList.remove("hidden");
      dom.previewPlaceholder.textContent = "渲染页面完成后，这里会展示当前页预览。";
      return;
    }

    dom.slideGrid.innerHTML = "";
    pages.forEach((page, index) => {
      const card = document.createElement("button");
      card.type = "button";
      card.className = `slide-card slide-${page.status || "queued"}${page.page_id === state.currentViewingPageId ? " slide-active" : ""}`;
      card.innerHTML = `
        <strong>${String(index + 1).padStart(2, "0")}</strong>
        <span>${escapeHtml(statusText(page.status || "queued"))}</span>
        <small>${escapeHtml(page.page_id || "-")}</small>
      `;
      card.addEventListener("click", () => activatePage(page.page_id));
      dom.slideGrid.appendChild(card);
    });

    const current = pages.find((item) => item.page_id === state.currentViewingPageId) || pages.find((item) => item.render_url) || pages[0];
    if (current?.render_url) {
      dom.previewImage.src = current.render_url;
      dom.previewImage.classList.remove("hidden");
      dom.previewPlaceholder.classList.add("hidden");
    } else {
      dom.previewImage.classList.add("hidden");
      dom.previewPlaceholder.classList.remove("hidden");
      dom.previewPlaceholder.textContent = "当前页还没有可展示的渲染结果。";
    }
  }

  function renderExport() {
    dom.successMark.textContent = state.requirements.total_pages || "OK";
    dom.exportTaskId.textContent = state.activeTaskId || "-";
    dom.exportFormatText.textContent = state.exportFormat || "未知";
    dom.exportTopicText.textContent = state.requirements.topic || state.tasks[state.activeTaskId] || "-";
    dom.exportDescription.textContent = state.exportUrl
      ? `任务《${state.requirements.topic || state.tasks[state.activeTaskId] || "未命名任务"}》已生成完成，可以直接打开导出链接。`
      : "等待导出地址。";
    dom.downloadLink.href = state.exportUrl || "#";
    dom.downloadLink.classList.toggle("disabled-link", !state.exportUrl);
  }

  function renderMeta() {
    dom.connectionBadge.textContent = statusText(state.connection);
    dom.sessionBadge.textContent = statusText(state.sessionStatus);
    dom.micBadge.textContent = state.micError ? "异常" : state.micSpeaking ? "收音中" : state.micReady ? "就绪" : "未授权";
    dom.taskBadge.textContent = state.activeTaskId ? statusText(state.taskStatus) : "未创建";
    dom.noticeText.textContent = state.notice;
    dom.latestUserText.textContent = state.latestUserText || "暂无";
    dom.latestAssistantText.textContent = state.latestAssistantText || "暂无";
    dom.collectedFieldsText.textContent = String(state.collectedFields.length);
    dom.missingFieldsText.textContent = String(state.missingFields.length);
    dom.requirementsProgressText.textContent = state.missingFields.length ? `待补充 ${state.missingFields.length} 项` : "需求已齐备";
    dom.userIdText.textContent = state.userId;
    dom.liveCaptionText.textContent = state.latestAssistantText || "连接建立后，这里会显示当前正在输出的内容。";
    dom.assistantOrb.classList.toggle("orb-active", state.sessionStatus === "speaking");
    dom.sendTextBtn.disabled = state.connection !== "live";
    dom.disconnectBtn.disabled = state.connection === "offline";
    dom.toggleMicBtn.disabled = state.connection !== "live";
    dom.toggleMicBtn.textContent = state.isMicCapturing ? "关闭麦克风" : "开启麦克风";
    dom.attachBtn.disabled = !(state.connection === "live" && state.referenceFiles.length);
    setScreen(computeScreenMode());
  }

  async function connectSession() {
    if (state.ws && state.ws.readyState === WebSocket.OPEN) return;

    state.connection = "connecting";
    renderMeta();
    logEvent("WS", "connecting");

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/ws?user_id=${encodeURIComponent(state.userId)}`;

    await new Promise((resolve, reject) => {
      const socket = new WebSocket(url);
      socket.binaryType = "arraybuffer";
      const timer = window.setTimeout(() => {
        socket.close();
        reject(new Error("WebSocket 连接超时"));
      }, 8000);

      socket.onopen = () => {
        window.clearTimeout(timer);
        state.ws = socket;
        state.connection = "live";
        state.referenceFiles.forEach((file) => {
          file.attached = false;
        });
        setNotice("连接已建立，可以开始语音或文本输入。");
        renderMeta();
        renderTasks();
        if (state.referenceFiles.length) {
          sendReferenceFiles(state.referenceFiles);
        }
        resolve();
      };

      socket.onerror = () => {
        window.clearTimeout(timer);
        reject(new Error("WebSocket 连接失败"));
      };

      socket.onclose = () => {
        window.clearTimeout(timer);
        state.ws = null;
        state.connection = "offline";
        state.sessionStatus = "idle";
        stopMicCapture();
        stopPreviewPolling();
        stopPlayback();
        setNotice("连接已断开。");
        renderMeta();
      };

      socket.onmessage = (event) => {
        if (event.data instanceof ArrayBuffer) {
          queuePlaybackChunk(event.data);
          return;
        }
        try {
          handleWSMessage(JSON.parse(event.data));
        } catch (error) {
          logEvent("WS_PARSE_ERR", error instanceof Error ? error.message : "unknown");
        }
      };
    });
  }

  function disconnectSession() {
    if (state.ws) {
      state.ws.close();
    }
  }

  function sendJSON(payload) {
    if (state.ws && state.ws.readyState === WebSocket.OPEN) {
      state.ws.send(JSON.stringify(payload));
    }
  }

  function handleWSMessage(message) {
    logEvent(message.type || "message", message.task_id || message.status || "");

    switch (message.type) {
      case "status":
        state.sessionStatus = message.status || message.state || state.sessionStatus;
        if (state.sessionStatus !== "speaking") {
          finalizeLiveMessage("assistant");
        }
        renderMeta();
        break;
      case "transcript":
        state.latestUserText = message.text || "";
        upsertMessage("user", message.text || "", { live: true, kind: "user" });
        renderMeta();
        break;
      case "transcript_final":
        state.latestUserText = message.text || state.latestUserText;
        upsertMessage("user", message.text || state.latestUserText, { live: false, kind: "user" });
        renderMeta();
        break;
      case "response": {
        const existing = state.messages.find((item) => item.id === "assistant-live");
        const nextText = `${existing?.text || ""}${message.text || ""}`;
        state.latestAssistantText = nextText;
        upsertMessage("assistant", nextText, { live: true, kind: "assistant" });
        renderMeta();
        break;
      }
      case "requirements_progress":
        state.requirements = normalizeRequirements(message.requirements || state.requirements);
        state.collectedFields = Array.isArray(message.collected_fields) ? message.collected_fields : state.requirements.collected_fields;
        state.missingFields = Array.isArray(message.missing_fields) ? message.missing_fields : [];
        state.requirements.status = message.status || state.requirements.status;
        setNotice(state.missingFields.length ? `需求采集中，还缺少 ${state.missingFields.length} 个字段。` : "需求字段已齐备。");
        renderRequirements();
        renderMeta();
        break;
      case "requirements_summary":
        state.requirements = normalizeRequirements(message.requirements || state.requirements);
        state.summaryText = message.summary_text || "";
        state.requirements.status = "ready";
        setNotice("需求摘要已生成，等待任务初始化。");
        renderRequirements();
        renderMeta();
        break;
      case "task_list_update":
        state.tasks = { ...state.tasks, ...(message.tasks || {}) };
        state.activeTaskId = message.active_task_id || state.activeTaskId;
        state.taskStatus = "rendering";
        setNotice(`任务已创建：${state.tasks[state.activeTaskId] || state.activeTaskId}`);
        renderTasks();
        renderMeta();
        fetchTaskPreview();
        startPreviewPolling();
        break;
      case "task_status":
        state.taskStatus = message.status || state.taskStatus;
        if (message.task_id) {
          state.activeTaskId = message.task_id;
        }
        setNotice(message.text || `任务状态更新：${statusText(state.taskStatus)}`);
        renderMeta();
        break;
      case "ppt_preview":
        mergePreviewPages(message.page_order || [], message.pages_info || []);
        renderPreview();
        renderMeta();
        break;
      case "page_rendered":
        mergeRenderedPage(message);
        renderPreview();
        break;
      case "export_ready":
        state.exportUrl = message.download_url || "";
        state.exportFormat = message.format || "";
        state.taskStatus = "ready_to_export";
        if (message.task_id) {
          state.activeTaskId = message.task_id;
        }
        setNotice("导出地址已生成。");
        stopPreviewPolling();
        renderExport();
        renderMeta();
        break;
      case "conflict_ask":
        state.conflictQuestion = message.question || "";
        setNotice("出现冲突待确认，请继续回答。");
        renderRequirements();
        renderMeta();
        break;
      case "ppt_mod_result":
      case "search_result":
      case "kb_result":
      case "memory_result":
        state.latestAssistantText = message.text || state.latestAssistantText;
        upsertMessage("system", message.text || "", { live: false, kind: message.type });
        renderMeta();
        break;
      case "error":
        setNotice(message.message || "后端返回错误。");
        upsertMessage("system", message.message || "后端返回错误。", { live: false, kind: "error" });
        renderMeta();
        break;
      default:
        break;
    }
  }

  function mergePreviewPages(pageOrder, pagesInfo) {
    const infoById = new Map();
    pagesInfo.forEach((item) => infoById.set(item.page_id, item));

    const ordered = (pageOrder || []).map((pageId) => {
      const current = infoById.get(pageId) || {};
      return {
        page_id: pageId,
        status: current.status || "queued",
        last_update: current.last_update || 0,
        render_url: current.render_url || "",
      };
    });

    state.previewPages = ordered.length ? ordered : (pagesInfo || []).map((item) => ({
      page_id: item.page_id,
      status: item.status || "queued",
      last_update: item.last_update || 0,
      render_url: item.render_url || "",
    }));

    if (!state.currentViewingPageId && state.previewPages.length) {
      state.currentViewingPageId = state.previewPages[0].page_id;
    }
  }

  function mergeRenderedPage(message) {
    const existing = state.previewPages.find((item) => item.page_id === message.page_id);
    if (existing) {
      existing.status = "completed";
      existing.render_url = message.render_url || existing.render_url;
    } else {
      state.previewPages.push({
        page_id: message.page_id,
        status: "completed",
        render_url: message.render_url || "",
      });
    }
    if (!state.currentViewingPageId || !state.previewPages.find((item) => item.page_id === state.currentViewingPageId)?.render_url) {
      state.currentViewingPageId = message.page_id || state.currentViewingPageId;
    }
    setNotice(`页面 ${message.page_index || state.previewPages.length} 已完成渲染。`);
  }

  async function fetchTaskPreview() {
    if (!state.activeTaskId) return;
    try {
      const response = await fetch(`/api/v1/tasks/${encodeURIComponent(state.activeTaskId)}/preview`);
      const payload = await response.json();
      if (!response.ok || payload.code !== 200) {
        throw new Error(payload.message || "预览拉取失败");
      }
      const data = payload.data || {};
      state.taskStatus = data.status || state.taskStatus;
      state.currentViewingPageId = data.current_viewing_page_id || state.currentViewingPageId;
      mergePreviewPages(data.page_order || [], data.pages || []);
      renderPreview();
      renderMeta();
    } catch (error) {
      logEvent("PREVIEW_ERR", error instanceof Error ? error.message : "unknown");
    }
  }

  function startPreviewPolling() {
    stopPreviewPolling();
    if (!state.activeTaskId || state.exportUrl) return;
    state.previewTimer = window.setInterval(fetchTaskPreview, 2500);
  }

  function stopPreviewPolling() {
    if (state.previewTimer) {
      window.clearInterval(state.previewTimer);
      state.previewTimer = null;
    }
  }

  function activateTask(taskId) {
    state.activeTaskId = taskId;
    sendJSON({ type: "page_navigate", task_id: taskId });
    renderTasks();
    renderMeta();
    fetchTaskPreview();
    startPreviewPolling();
  }

  function activatePage(pageId) {
    state.currentViewingPageId = pageId;
    sendJSON({ type: "page_navigate", task_id: state.activeTaskId, page_id: pageId });
    renderPreview();
  }

  async function uploadSelectedFile() {
    if (!state.selectedFile || state.uploadBusy) return;

    state.uploadBusy = true;
    renderUploadSelection();

    try {
      const form = new FormData();
      form.append("file", state.selectedFile);

      const response = await fetch("/api/v1/files/upload", { method: "POST", body: form });
      const payload = await response.json();
      if (!response.ok || payload.code !== 200) {
        throw new Error(payload.message || "上传失败");
      }

      const raw = payload.data || {};
      const normalized = {
        file_id: raw.file_id || raw.id || "",
        filename: raw.filename || state.selectedFile.name,
        file_type: raw.file_type || state.selectedFile.type || "file",
        file_size: raw.file_size || state.selectedFile.size,
        storage_url: raw.storage_url || raw.file_url || "",
        instruction: dom.fileInstruction.value.trim(),
        attached: false,
      };

      state.referenceFiles.push(normalized);
      state.selectedFile = null;
      dom.fileInput.value = "";
      dom.fileInstruction.value = "";
      setNotice(`文件已上传：${normalized.filename}`);
      renderReferenceFiles();
      renderUploadSelection();

      if (state.connection === "live") {
        sendReferenceFiles([normalized]);
      }
    } catch (error) {
      setNotice(error instanceof Error ? error.message : "上传失败");
    } finally {
      state.uploadBusy = false;
      renderUploadSelection();
      renderMeta();
    }
  }

  function sendReferenceFiles(files) {
    if (!files.length || state.connection !== "live") return;
    const payloadFiles = files
      .filter((file) => file.file_id && !file.attached)
      .map((file) => ({
        file_id: file.file_id,
        file_url: file.storage_url || "",
        file_type: file.file_type || "file",
        instruction: file.instruction || "",
      }));
    if (!payloadFiles.length) return;
    sendJSON({ type: "add_reference_files", files: payloadFiles });
    files.forEach((file) => {
      if (file.file_id) {
        file.attached = true;
      }
    });
    setNotice(`已向当前会话附加 ${payloadFiles.length} 份参考材料。`);
  }

  function sendTextInput() {
    const text = dom.textInput.value.trim();
    if (!text) return;
    sendJSON({ type: "text_input", text });
    dom.textInput.value = "";
    renderMeta();
  }

  function stopPlayback() {
    if (state.playbackSource) {
      try {
        state.playbackSource.stop();
      } catch (_error) {
      }
      state.playbackSource = null;
    }
    state.audioQueue = [];
    state.audioPlaying = false;
    state.playbackRemainder = new Uint8Array(0);
  }

  function queuePlaybackChunk(arrayBuffer) {
    let bytes = new Uint8Array(arrayBuffer);
    if (bytes.length >= 4 && bytes[0] === 82 && bytes[1] === 73 && bytes[2] === 70 && bytes[3] === 70) {
      bytes = bytes.slice(44);
    }

    if (state.playbackRemainder.length) {
      const merged = new Uint8Array(state.playbackRemainder.length + bytes.length);
      merged.set(state.playbackRemainder, 0);
      merged.set(bytes, state.playbackRemainder.length);
      bytes = merged;
      state.playbackRemainder = new Uint8Array(0);
    }

    if (bytes.length % 2 === 1) {
      state.playbackRemainder = bytes.slice(bytes.length - 1);
      bytes = bytes.slice(0, bytes.length - 1);
    }
    if (!bytes.length) return;

    const pcm = new Int16Array(bytes.buffer, bytes.byteOffset, bytes.byteLength / 2);
    const float32 = new Float32Array(pcm.length);
    for (let i = 0; i < pcm.length; i += 1) {
      float32[i] = pcm[i] / 32768;
    }
    state.audioQueue.push(float32);
    if (!state.audioPlaying) {
      playNextChunk();
    }
  }

  async function playNextChunk() {
    if (!state.audioQueue.length) {
      state.audioPlaying = false;
      return;
    }

    if (!state.playbackContext) {
      state.playbackContext = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 24000 });
    }
    if (state.playbackContext.state === "suspended") {
      await state.playbackContext.resume();
    }

    state.audioPlaying = true;
    const chunk = state.audioQueue.shift();
    const buffer = state.playbackContext.createBuffer(1, chunk.length, 24000);
    buffer.copyToChannel(chunk, 0);
    const source = state.playbackContext.createBufferSource();
    source.buffer = buffer;
    source.connect(state.playbackContext.destination);
    source.onended = () => {
      if (state.playbackSource === source) {
        state.playbackSource = null;
      }
      playNextChunk();
    };
    state.playbackSource = source;
    source.start();
  }

  async function startMicCapture() {
    if (state.isMicCapturing) return;

    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      });
      const AudioContextCtor = window.AudioContext || window.webkitAudioContext;
      const context = new AudioContextCtor();
      const source = context.createMediaStreamSource(stream);
      const processor = context.createScriptProcessor(4096, 1, 1);
      const sink = context.createGain();
      sink.gain.value = 0;

      state.micContext = context;
      state.micStream = stream;
      state.micSource = source;
      state.micProcessor = processor;
      state.micSink = sink;
      state.micReady = true;
      state.micError = "";
      state.isMicCapturing = true;
      state.preSpeechChunks = [];
      state.speechStarted = false;
      state.silenceSince = 0;

      processor.onaudioprocess = (event) => {
        const input = event.inputBuffer.getChannelData(0);
        const downsampled = downsampleTo16k(input, context.sampleRate);
        if (!downsampled.length) return;

        const rms = computeRms(input);
        const now = performance.now();
        const threshold = 0.03;
        const silenceDelayMs = 550;

        if (rms > threshold) {
          state.silenceSince = 0;
          if (!state.speechStarted) {
            state.speechStarted = true;
            state.micSpeaking = true;
            stopPlayback();
            sendJSON({ type: "vad_start" });
            flushPreSpeechChunks();
          }
          sendAudioChunk(downsampled);
        } else if (state.speechStarted) {
          sendAudioChunk(downsampled);
          if (!state.silenceSince) state.silenceSince = now;
          if (now - state.silenceSince >= silenceDelayMs) {
            state.speechStarted = false;
            state.micSpeaking = false;
            state.silenceSince = 0;
            sendJSON({ type: "vad_end" });
          }
        } else {
          pushPreSpeechChunk(downsampled);
        }
        renderMeta();
      };

      source.connect(processor);
      processor.connect(sink);
      sink.connect(context.destination);
      if (context.state === "suspended") {
        await context.resume();
      }
      setNotice("麦克风已开启，可以直接说话。");
      renderMeta();
    } catch (error) {
      state.micError = error instanceof Error ? error.message : "无法获取麦克风权限";
      state.micReady = false;
      state.isMicCapturing = false;
      setNotice(state.micError);
      renderMeta();
    }
  }

  function stopMicCapture() {
    state.isMicCapturing = false;
    state.micSpeaking = false;
    state.speechStarted = false;
    state.silenceSince = 0;
    state.preSpeechChunks = [];

    if (state.micProcessor) {
      state.micProcessor.disconnect();
      state.micProcessor.onaudioprocess = null;
      state.micProcessor = null;
    }
    if (state.micSource) {
      state.micSource.disconnect();
      state.micSource = null;
    }
    if (state.micSink) {
      state.micSink.disconnect();
      state.micSink = null;
    }
    if (state.micStream) {
      state.micStream.getTracks().forEach((track) => track.stop());
      state.micStream = null;
    }
    if (state.micContext) {
      state.micContext.close().catch(() => undefined);
      state.micContext = null;
    }
    renderMeta();
  }

  function toggleMicCapture() {
    if (state.isMicCapturing) {
      stopMicCapture();
      setNotice("麦克风已关闭。");
      return;
    }
    startMicCapture();
  }

  function pushPreSpeechChunk(chunk) {
    const copied = new Int16Array(chunk);
    state.preSpeechChunks.push(copied);
    if (state.preSpeechChunks.length > 8) state.preSpeechChunks.shift();
  }

  function flushPreSpeechChunks() {
    state.preSpeechChunks.forEach((chunk) => sendAudioChunk(chunk));
    state.preSpeechChunks = [];
  }

  function sendAudioChunk(int16Chunk) {
    if (!state.ws || state.ws.readyState !== WebSocket.OPEN) return;
    state.ws.send(int16Chunk.buffer.slice(0));
  }

  function computeRms(samples) {
    let sum = 0;
    for (let i = 0; i < samples.length; i += 1) {
      sum += samples[i] * samples[i];
    }
    return Math.sqrt(sum / samples.length);
  }

  function downsampleTo16k(float32Samples, sourceRate) {
    if (sourceRate === 16000) {
      const direct = new Int16Array(float32Samples.length);
      for (let i = 0; i < float32Samples.length; i += 1) {
        const sample = Math.max(-1, Math.min(1, float32Samples[i]));
        direct[i] = sample < 0 ? sample * 32768 : sample * 32767;
      }
      return direct;
    }

    const ratio = sourceRate / 16000;
    const newLength = Math.round(float32Samples.length / ratio);
    const result = new Int16Array(newLength);
    let offsetResult = 0;
    let offsetBuffer = 0;
    while (offsetResult < result.length) {
      const nextOffsetBuffer = Math.round((offsetResult + 1) * ratio);
      let accum = 0;
      let count = 0;
      for (let i = offsetBuffer; i < nextOffsetBuffer && i < float32Samples.length; i += 1) {
        accum += float32Samples[i];
        count += 1;
      }
      const sample = count ? accum / count : 0;
      const clamped = Math.max(-1, Math.min(1, sample));
      result[offsetResult] = clamped < 0 ? clamped * 32768 : clamped * 32767;
      offsetResult += 1;
      offsetBuffer = nextOffsetBuffer;
    }
    return result;
  }

  function setupEvents() {
    dom.connectBtn.addEventListener("click", async () => {
      try {
        await connectSession();
        await startMicCapture();
      } catch (error) {
        setNotice(error instanceof Error ? error.message : "连接失败");
      }
    });
    dom.disconnectBtn.addEventListener("click", () => disconnectSession());
    dom.toggleMicBtn.addEventListener("click", () => toggleMicCapture());
    dom.sendTextBtn.addEventListener("click", () => sendTextInput());
    dom.uploadBtn.addEventListener("click", () => uploadSelectedFile());
    dom.attachBtn.addEventListener("click", () => sendReferenceFiles(state.referenceFiles));
    dom.backToTaskBtn.addEventListener("click", () => setScreen("rendering"));
    dom.fileInput.addEventListener("change", (event) => {
      const files = event.target.files;
      state.selectedFile = files && files[0] ? files[0] : null;
      renderUploadSelection();
    });
    dom.textInput.addEventListener("keydown", (event) => {
      if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
        event.preventDefault();
        sendTextInput();
      }
    });
    dom.logToggle.addEventListener("click", () => {
      dom.logContent.classList.toggle("hidden");
      dom.logToggle.textContent = dom.logContent.classList.contains("hidden") ? "展开事件日志" : "收起事件日志";
    });
  }

  function initialRender() {
    dom.userIdText.textContent = state.userId;
    renderUploadSelection();
    renderReferenceFiles();
    renderRequirements();
    renderTasks();
    renderPreview();
    renderExport();
    renderMessages();
    renderMeta();
  }

  setupEvents();
  initialRender();
  logEvent("INIT", `user_id=${state.userId}`);
})();
