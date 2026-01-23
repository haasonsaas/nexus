(() => {
  const root = document.querySelector(".webchat-shell");
  if (!root) {
    return;
  }

  const wsPath = root.dataset.wsUrl || "/ws";
  const wsUrl = `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}${wsPath}`;

  const connectionEl = document.getElementById("webchat-connection");
  const statusEl = document.getElementById("webchat-status");
  const messagesEl = document.getElementById("webchat-messages");
  const formEl = document.getElementById("webchat-form");
  const inputEl = document.getElementById("webchat-input");
  const tokenEl = document.getElementById("webchat-token");
  const sessionEl = document.getElementById("webchat-session");
  const connectBtn = document.getElementById("webchat-connect");
  const disconnectBtn = document.getElementById("webchat-disconnect");
  const loadBtn = document.getElementById("webchat-load");
  const resetBtn = document.getElementById("webchat-reset");
  const abortBtn = document.getElementById("webchat-abort");

  let ws = null;
  let requestId = 0;
  const pending = new Map();
  const streaming = new Map();

  const clientIdKey = "nexus-webchat-client-id";
  const sessionKey = "nexus-webchat-session-id";
  const storedSession = window.localStorage.getItem(sessionKey);
  if (storedSession) {
    sessionEl.value = storedSession;
  }

  function updateConnection(state, label) {
    if (!connectionEl) {
      return;
    }
    const indicator = connectionEl.querySelector(".indicator");
    const indicatorLabel = connectionEl.querySelector(".indicator-label");
    if (indicator) {
      indicator.classList.remove("indicator-success", "indicator-warning", "indicator-error");
      indicator.classList.add(state);
    }
    if (indicatorLabel) {
      indicatorLabel.textContent = label;
    }
  }

  function setStatus(text) {
    if (statusEl) {
      statusEl.textContent = text;
    }
  }

  function nextRequestId() {
    requestId += 1;
    return String(requestId);
  }

  function ensureClientId() {
    let id = window.localStorage.getItem(clientIdKey);
    if (!id) {
      id = `web-${Math.random().toString(36).slice(2)}`;
      window.localStorage.setItem(clientIdKey, id);
    }
    return id;
  }

  function sendFrame(method, params, onResponse) {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return null;
    }
    const id = nextRequestId();
    const frame = {
      type: "req",
      id,
      method,
      params: params || {},
    };
    if (onResponse) {
      pending.set(id, onResponse);
    }
    ws.send(JSON.stringify(frame));
    return id;
  }

  function connectWS() {
    if (ws && ws.readyState === WebSocket.OPEN) {
      return;
    }
    ws = new WebSocket(wsUrl);
    updateConnection("indicator-warning", "Connecting...");
    setStatus("Connecting...");

    ws.addEventListener("open", () => {
      const token = tokenEl.value.trim();
      const params = {
        minProtocol: 1,
        maxProtocol: 1,
        client: {
          id: ensureClientId(),
          version: "0.1.0",
          platform: "web",
          mode: "webchat",
        },
        userAgent: navigator.userAgent,
      };
      if (token) {
        params.auth = { token };
      }
      sendFrame("connect", params, (frame) => {
        if (frame.ok) {
          updateConnection("indicator-success", "Connected");
          setStatus("Connected");
        } else {
          updateConnection("indicator-error", "Auth failed");
          setStatus(frame.error?.message || "Connection failed");
        }
      });
    });

    ws.addEventListener("close", () => {
      updateConnection("indicator-warning", "Disconnected");
      setStatus("Disconnected");
      ws = null;
    });

    ws.addEventListener("error", () => {
      updateConnection("indicator-error", "Error");
      setStatus("Connection error");
    });

    ws.addEventListener("message", (event) => {
      let frame = null;
      try {
        frame = JSON.parse(event.data);
      } catch (_err) {
        return;
      }
      if (frame.type === "res") {
        const handler = pending.get(frame.id);
        if (handler) {
          pending.delete(frame.id);
          handler(frame);
        }
        return;
      }
      if (frame.type === "event") {
        handleEvent(frame);
      }
    });
  }

  function disconnectWS() {
    if (ws) {
      ws.close();
    }
  }

  function roleClass(role) {
    const norm = (role || "").toLowerCase();
    if (norm.includes("assistant")) return "assistant";
    if (norm.includes("system")) return "system";
    if (norm.includes("tool")) return "tool";
    return "user";
  }

  function roleLabel(role) {
    const norm = (role || "").toLowerCase();
    if (norm.includes("assistant")) return "assistant";
    if (norm.includes("system")) return "system";
    if (norm.includes("tool")) return "tool";
    if (norm.includes("user")) return "user";
    return role || "message";
  }

  function appendMessage(role, content, messageId) {
    const wrapper = document.createElement("div");
    wrapper.className = `message message-${roleClass(role)}`;
    if (messageId) {
      wrapper.dataset.id = messageId;
    }

    const header = document.createElement("div");
    header.className = "message-header";
    const roleSpan = document.createElement("span");
    roleSpan.className = "message-role";
    roleSpan.textContent = roleLabel(role);
    const timeSpan = document.createElement("span");
    timeSpan.className = "message-time";
    timeSpan.textContent = new Date().toLocaleTimeString();
    header.appendChild(roleSpan);
    header.appendChild(timeSpan);

    const contentWrap = document.createElement("div");
    contentWrap.className = "message-content";
    const text = document.createElement("pre");
    text.className = "message-text";
    text.textContent = content;
    contentWrap.appendChild(text);

    wrapper.appendChild(header);
    wrapper.appendChild(contentWrap);
    messagesEl.appendChild(wrapper);
    messagesEl.scrollTop = messagesEl.scrollHeight;
    return { wrapper, text };
  }

  function updateStreaming(messageId, delta) {
    if (!messageId) {
      return;
    }
    let entry = streaming.get(messageId);
    if (!entry) {
      const created = appendMessage("assistant", "", messageId);
      entry = created;
      streaming.set(messageId, entry);
    }
    entry.text.textContent += delta;
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }

  function finalizeMessage(message) {
    if (!message) {
      return;
    }
    const id = message.id;
    const role = message.role || "assistant";
    const content = message.content || "";
    if (id && streaming.has(id)) {
      const entry = streaming.get(id);
      entry.text.textContent = content;
      streaming.delete(id);
      return;
    }
    appendMessage(role, content, id);
  }

  function handleEvent(frame) {
    const event = frame.event;
    const payload = frame.payload || {};
    if (event === "chat.chunk") {
      updateStreaming(payload.messageId, payload.content || "");
      setStatus("Streaming...");
      return;
    }
    if (event === "chat.complete") {
      const msg = payload.message;
      if (msg && msg.session_id) {
        setSessionId(msg.session_id);
      }
      finalizeMessage(msg);
      setStatus("Idle");
      return;
    }
    if (event === "error") {
      setStatus(payload.message || "Error");
      return;
    }
  }

  function setSessionId(id) {
    if (!id) return;
    sessionEl.value = id;
    window.localStorage.setItem(sessionKey, id);
  }

  function clearMessages() {
    messagesEl.innerHTML = "";
    streaming.clear();
  }

  connectBtn.addEventListener("click", () => {
    connectWS();
  });

  disconnectBtn.addEventListener("click", () => {
    disconnectWS();
  });

  loadBtn.addEventListener("click", () => {
    const sessionId = sessionEl.value.trim();
    if (!sessionId) {
      setStatus("Session ID required");
      return;
    }
    clearMessages();
    sendFrame("chat.history", { sessionId, limit: 50 }, (frame) => {
      if (!frame.ok) {
        setStatus(frame.error?.message || "History failed");
        return;
      }
      const payload = frame.payload || {};
      const messages = payload.messages || [];
      messages.forEach((msg) => finalizeMessage(msg));
      setStatus("History loaded");
    });
  });

  resetBtn.addEventListener("click", () => {
    sessionEl.value = "";
    window.localStorage.removeItem(sessionKey);
    clearMessages();
    setStatus("New session");
  });

  abortBtn.addEventListener("click", () => {
    const sessionId = sessionEl.value.trim();
    if (!sessionId) {
      setStatus("Session ID required");
      return;
    }
    sendFrame("chat.abort", { sessionId }, (frame) => {
      if (frame.ok) {
        setStatus("Abort sent");
      } else {
        setStatus(frame.error?.message || "Abort failed");
      }
    });
  });

  formEl.addEventListener("submit", (event) => {
    event.preventDefault();
    const content = inputEl.value.trim();
    if (!content) {
      return;
    }
    const sessionId = sessionEl.value.trim();
    appendMessage("user", content);
    sendFrame("chat.send", { sessionId, content }, (frame) => {
      if (!frame.ok) {
        setStatus(frame.error?.message || "Send failed");
      } else {
        setStatus("Sending...");
      }
    });
    inputEl.value = "";
  });
})();
