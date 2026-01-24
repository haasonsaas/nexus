package canvas

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"

	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/ratelimit"
	"github.com/haasonsaas/nexus/pkg/models"
)

const (
	dompurifyPlaceholder = "/* DOMPURIFY_PLACEHOLDER */"
	defaultIndexHTML     = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Nexus Canvas</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f2ea;
      --bg-2: #ece6db;
      --ink: #141414;
      --muted: #5c574f;
      --accent: #0f8b8d;
      --accent-2: #f26b4f;
      --panel: rgba(255, 255, 255, 0.86);
      --panel-border: #e1d9cb;
      --shadow: 0 24px 60px rgba(20, 16, 8, 0.12);
      --mono: "IBM Plex Mono", "Fira Mono", "Menlo", monospace;
      --sans: "Space Grotesk", "Sora", "Fira Sans", sans-serif;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: var(--sans);
      background:
        radial-gradient(1200px 700px at 15% 10%, #fff9f1, transparent 60%),
        radial-gradient(1000px 600px at 85% 20%, #f0f7ff, transparent 55%),
        linear-gradient(120deg, var(--bg), var(--bg-2));
      color: var(--ink);
    }
    .app {
      min-height: 100vh;
      padding: 32px;
      display: grid;
      gap: 24px;
    }
    header.top {
      display: flex;
      align-items: center;
      justify-content: space-between;
      flex-wrap: wrap;
      gap: 16px;
    }
    .brand {
      display: flex;
      align-items: center;
      gap: 14px;
    }
    .brand .dot {
      width: 14px;
      height: 14px;
      border-radius: 50%;
      background: linear-gradient(135deg, var(--accent), #5fd1c3);
      box-shadow: 0 0 0 6px rgba(15, 139, 141, 0.15);
    }
    .title {
      font-size: 22px;
      letter-spacing: 0.4px;
      font-weight: 600;
    }
    .subtitle {
      font-size: 13px;
      color: var(--muted);
      font-family: var(--mono);
    }
    .status {
      display: flex;
      gap: 10px;
      align-items: center;
      flex-wrap: wrap;
    }
    .pill {
      padding: 6px 12px;
      border-radius: 999px;
      font-size: 12px;
      text-transform: uppercase;
      letter-spacing: 0.6px;
      background: var(--accent);
      color: white;
    }
    .pill.offline {
      background: #b44b4b;
    }
    .pill.outline {
      background: transparent;
      color: var(--muted);
      border: 1px solid var(--panel-border);
      text-transform: none;
      letter-spacing: 0;
    }
    main.layout {
      display: grid;
      grid-template-columns: minmax(0, 1fr) 320px;
      gap: 24px;
    }
    .panel, .card {
      background: var(--panel);
      border: 1px solid var(--panel-border);
      border-radius: 20px;
      box-shadow: var(--shadow);
      padding: 20px 22px;
    }
    .panel h2 {
      margin: 0 0 12px;
      font-size: 16px;
      letter-spacing: 0.3px;
      text-transform: uppercase;
      color: var(--muted);
    }
    .canvas-root {
      min-height: 420px;
      display: grid;
      align-content: start;
      gap: 16px;
    }
    .empty {
      border: 1px dashed #d9d2c5;
      border-radius: 16px;
      padding: 24px;
      background: rgba(255, 255, 255, 0.6);
      text-align: center;
    }
    .empty h3 {
      margin: 0 0 6px;
      font-size: 18px;
    }
    .empty p {
      margin: 0 0 12px;
      color: var(--muted);
    }
    button {
      border: none;
      background: var(--accent);
      color: white;
      padding: 10px 16px;
      border-radius: 12px;
      font-weight: 600;
      cursor: pointer;
      box-shadow: 0 10px 22px rgba(15, 139, 141, 0.2);
    }
    button.secondary {
      background: var(--accent-2);
    }
    .sidebar {
      display: grid;
      gap: 18px;
      align-content: start;
    }
    .card h3 {
      margin: 0 0 8px;
      font-size: 14px;
      text-transform: uppercase;
      letter-spacing: 0.4px;
      color: var(--muted);
    }
    .meta {
      font-size: 13px;
      color: var(--muted);
      margin-bottom: 6px;
    }
    .meta span {
      font-family: var(--mono);
      color: var(--ink);
    }
    .log {
      max-height: 320px;
      overflow: auto;
    }
    .log-entry {
      font-family: var(--mono);
      font-size: 12px;
      padding: 8px 10px;
      border-radius: 10px;
      background: rgba(15, 139, 141, 0.08);
      margin-bottom: 8px;
    }
    .a2ui-stack {
      display: grid;
      gap: 12px;
    }
    .a2ui-row {
      display: flex;
      gap: 12px;
      flex-wrap: wrap;
    }
    .a2ui-card {
      padding: 14px;
      border-radius: 14px;
      border: 1px solid var(--panel-border);
      background: rgba(255, 255, 255, 0.7);
    }
    .a2ui-text {
      margin: 0;
      line-height: 1.5;
    }
    .a2ui-button {
      align-self: start;
    }
    @media (max-width: 900px) {
      main.layout {
        grid-template-columns: 1fr;
      }
    }
  </style>
</head>
<body>
  <div class="app">
    <header class="top">
      <div class="brand">
        <span class="dot"></span>
        <div>
          <div class="title">Nexus Canvas</div>
          <div class="subtitle" id="session-id">Session</div>
        </div>
      </div>
      <div class="status">
        <span class="pill offline" id="stream-status">offline</span>
        <span class="pill outline" id="update-count">0 updates</span>
      </div>
    </header>

    <main class="layout">
      <section class="panel">
        <h2>Live View</h2>
        <div id="canvas-root" class="canvas-root">
          <div class="empty">
            <h3>No payload yet</h3>
            <p>Waiting for canvas updates on this session.</p>
            <button id="demo-action">Send demo action</button>
          </div>
        </div>
      </section>

      <aside class="sidebar">
        <div class="card">
          <h3>Connection</h3>
          <div class="meta">Stream: <span id="stream-label">starting...</span></div>
          <div class="meta">Session: <span id="session-label">--</span></div>
          <div class="meta">Last update: <span id="last-update">--</span></div>
        </div>
        <div class="card log">
          <h3>Event Log</h3>
          <div id="event-log"></div>
        </div>
      </aside>
    </main>
  </div>

  <script>
    /* DOMPURIFY_PLACEHOLDER */
    (() => {
      const root = document.getElementById("canvas-root");
      const statusEl = document.getElementById("stream-status");
      const streamLabel = document.getElementById("stream-label");
      const sessionLabel = document.getElementById("session-label");
      const sessionIdEl = document.getElementById("session-id");
      const updateCountEl = document.getElementById("update-count");
      const lastUpdateEl = document.getElementById("last-update");
      const logEl = document.getElementById("event-log");
      const demoBtn = document.getElementById("demo-action");
      let updateCount = 0;
      let a2uiState = { rootId: null, components: new Map() };

      const parseSessionInfo = () => {
        const parts = window.location.pathname.split("/").filter(Boolean);
        const canvasIdx = parts.indexOf("canvas");
        const namespace = canvasIdx > 0 ? "/" + parts.slice(0, canvasIdx).join("/") : "";
        const sessionId = canvasIdx >= 0 ? parts[canvasIdx + 1] : "";
        const basePath = namespace + "/canvas";
        const token = new URLSearchParams(window.location.search).get("token") || "";
        return { basePath, sessionId, token };
      };

      const setStatus = (state) => {
        const online = state === "online";
        statusEl.textContent = online ? "online" : state;
        statusEl.classList.toggle("offline", !online);
        streamLabel.textContent = state;
      };

      const addLog = (entry) => {
        if (!logEl) return;
        const item = document.createElement("div");
        item.className = "log-entry";
        item.textContent = entry;
        logEl.prepend(item);
      };

      const sanitizeHTML = (html) => {
        if (!html || typeof html !== "string") {
          return "";
        }
        if (window.DOMPurify && typeof window.DOMPurify.sanitize === "function") {
          const cleaned = window.DOMPurify.sanitize(html, {
            USE_PROFILES: { html: true },
            ADD_ATTR: ["target", "rel"],
          });
          return cleaned;
        }
        const template = document.createElement("template");
        template.innerHTML = html;
        const allowedTags = new Set([
          "a", "b", "blockquote", "br", "code", "div", "em", "h1", "h2", "h3", "h4", "h5", "h6",
          "hr", "i", "img", "li", "ol", "p", "pre", "span", "strong", "table", "tbody", "td", "th",
          "thead", "tr", "ul"
        ]);
        const globalAttrs = new Set(["class", "id", "title"]);
        const tagAttrs = {
          a: new Set(["href", "rel", "target", "title"]),
          img: new Set(["alt", "height", "src", "title", "width"]),
        };
        const isAllowedAttr = (tag, name) => {
          if (name.startsWith("data-") || name.startsWith("aria-")) {
            return true;
          }
          if (globalAttrs.has(name)) {
            return true;
          }
          const allowed = tagAttrs[tag];
          return !!(allowed && allowed.has(name));
        };
        const isSafeUrl = (value) => {
          if (!value) return false;
          const trimmed = value.trim();
          if (trimmed.startsWith("#") || trimmed.startsWith("/") || trimmed.startsWith("./") || trimmed.startsWith("../")) {
            return true;
          }
          try {
            const url = new URL(trimmed, window.location.origin);
            return ["http:", "https:", "mailto:", "tel:"].includes(url.protocol);
          } catch {
            return false;
          }
        };
        const sanitizeNode = (node) => {
          if (node.nodeType === Node.ELEMENT_NODE) {
            const tag = node.tagName.toLowerCase();
            if (!allowedTags.has(tag)) {
              const text = document.createTextNode(node.textContent || "");
              node.replaceWith(text);
              return;
            }
            for (const attr of Array.from(node.attributes)) {
              const name = attr.name.toLowerCase();
              const value = attr.value || "";
              if (name.startsWith("on")) {
                node.removeAttribute(attr.name);
                continue;
              }
              if (!isAllowedAttr(tag, name)) {
                node.removeAttribute(attr.name);
                continue;
              }
              if ((name === "href" || name === "src") && !isSafeUrl(value)) {
                node.removeAttribute(attr.name);
              }
            }
            if (tag === "a") {
              const target = node.getAttribute("target");
              if (target && target.toLowerCase() === "_blank") {
                const rel = (node.getAttribute("rel") || "").split(/\s+/).filter(Boolean);
                if (!rel.includes("noopener")) rel.push("noopener");
                if (!rel.includes("noreferrer")) rel.push("noreferrer");
                node.setAttribute("rel", rel.join(" "));
              }
            }
          }
          for (const child of Array.from(node.childNodes)) {
            sanitizeNode(child);
          }
        };
        for (const child of Array.from(template.content.childNodes)) {
          sanitizeNode(child);
        }
        return template.innerHTML;
      };

      const renderPayload = (payload) => {
        if (!payload) {
          root.innerHTML = '<div class="empty"><h3>Waiting...</h3><p>No payload available yet.</p></div>';
          return;
        }
        if (payload.html) {
          root.innerHTML = sanitizeHTML(payload.html);
          const links = root.querySelectorAll("a[target='_blank']");
          for (const link of links) {
            const rel = (link.getAttribute("rel") || "").split(/\s+/).filter(Boolean);
            if (!rel.includes("noopener")) rel.push("noopener");
            if (!rel.includes("noreferrer")) rel.push("noreferrer");
            link.setAttribute("rel", rel.join(" "));
          }
          return;
        }
        if (payload.text || payload.markdown) {
          const text = payload.text || payload.markdown;
          root.innerHTML = '<div class="a2ui-card"><p class="a2ui-text"></p></div>';
          root.querySelector("p").textContent = text;
          return;
        }
        if (payload.a2ui_jsonl || payload.a2ui) {
          renderA2UI(payload.a2ui_jsonl || payload.a2ui);
          return;
        }
        if (payload.surfaceUpdate || payload.beginRendering) {
          renderA2UI(JSON.stringify(payload));
          return;
        }
        root.innerHTML = '<pre class="a2ui-card"></pre>';
        root.querySelector("pre").textContent = JSON.stringify(payload, null, 2);
      };

      const renderA2UI = (input) => {
        let lines = [];
        if (Array.isArray(input)) {
          lines = input;
        } else if (typeof input === "string") {
          lines = input.split(/\r?\n/).filter(Boolean);
        } else if (input && typeof input === "object") {
          lines = [JSON.stringify(input)];
        }
        lines.forEach((line) => {
          try {
            const msg = typeof line === "string" ? JSON.parse(line) : line;
            if (msg.surfaceUpdate && msg.surfaceUpdate.components) {
              msg.surfaceUpdate.components.forEach((item) => {
                a2uiState.components.set(item.id, item);
              });
            }
            if (msg.beginRendering && msg.beginRendering.root) {
              a2uiState.rootId = msg.beginRendering.root;
            }
            if (msg.deleteSurface) {
              a2uiState = { rootId: null, components: new Map() };
            }
          } catch (err) {
            console.warn("Invalid A2UI line", err);
          }
        });

        if (!a2uiState.rootId) {
          root.innerHTML = '<div class="empty"><h3>No A2UI root</h3><p>Waiting for beginRendering.</p></div>';
          return;
        }
        root.innerHTML = "";
        root.appendChild(renderComponent(a2uiState.rootId));
      };

      const renderComponent = (id) => {
        const node = a2uiState.components.get(id);
        if (!node) {
          const missing = document.createElement("div");
          missing.className = "a2ui-card";
          missing.textContent = "Missing component " + id;
          return missing;
        }
        const component = node.component || node;
        const entry = Object.entries(component)[0];
        if (!entry) {
          const empty = document.createElement("div");
          empty.className = "a2ui-card";
          empty.textContent = JSON.stringify(component);
          return empty;
        }
        const [type, value] = entry;
        switch (type) {
          case "Column": {
            const wrapper = document.createElement("div");
            wrapper.className = "a2ui-stack";
            const children = value?.children?.explicitList || value?.children?.implicitList || [];
            children.forEach((childId) => wrapper.appendChild(renderComponent(childId)));
            return wrapper;
          }
          case "Row": {
            const wrapper = document.createElement("div");
            wrapper.className = "a2ui-row";
            const children = value?.children?.explicitList || value?.children?.implicitList || [];
            children.forEach((childId) => wrapper.appendChild(renderComponent(childId)));
            return wrapper;
          }
          case "Card": {
            const card = document.createElement("div");
            card.className = "a2ui-card";
            const children = value?.children?.explicitList || value?.children?.implicitList || [];
            if (children.length === 0) {
              card.textContent = "Card";
            } else {
              children.forEach((childId) => card.appendChild(renderComponent(childId)));
            }
            return card;
          }
          case "Text": {
            const text = value?.text?.literalString || value?.text?.markdownString || value?.text?.plainText || "";
            const p = document.createElement(value?.usageHint === "h1" ? "h1" : "p");
            p.className = "a2ui-text";
            p.textContent = text;
            return p;
          }
          case "Button": {
            const label = value?.label?.literalString || value?.text?.literalString || value?.title?.literalString || "Action";
            const btn = document.createElement("button");
            btn.className = "a2ui-button";
            btn.textContent = label;
            btn.addEventListener("click", () => {
              sendUserAction({
                name: value?.action?.name || value?.action?.action || label,
                source_component_id: id,
                context: { component: id, label }
              });
            });
            return btn;
          }
          case "Image": {
            const img = document.createElement("img");
            img.src = value?.url || value?.src || "";
            img.alt = value?.alt || "";
            img.style.maxWidth = "100%";
            img.style.borderRadius = "12px";
            return img;
          }
          default: {
            const fallback = document.createElement("pre");
            fallback.className = "a2ui-card";
            fallback.textContent = JSON.stringify(component, null, 2);
            return fallback;
          }
        }
      };

      const sendUserAction = (action) => {
        const info = parseSessionInfo();
        if (!info.sessionId) return false;
        const url = new URL(info.basePath + "/api/action", window.location.origin);
        const payload = {
          session_id: info.sessionId,
          name: action.name || "action",
          id: action.id || (crypto?.randomUUID ? crypto.randomUUID() : String(Date.now())),
          source_component_id: action.source_component_id,
          context: action.context || {},
        };
        return fetch(url.toString() + (info.token ? "?token=" + encodeURIComponent(info.token) : ""), {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        }).then((res) => res.ok).catch(() => false);
      };

      window.nexusSendUserAction = sendUserAction;

      if (demoBtn) {
        demoBtn.addEventListener("click", () => {
          sendUserAction({
            name: "demo.click",
            source_component_id: "demo.button",
            context: { at: Date.now() },
          });
        });
      }

      const info = parseSessionInfo();
      if (!info.sessionId) {
        setStatus("no session");
        sessionLabel.textContent = "--";
        sessionIdEl.textContent = "No session";
        return;
      }
      sessionIdEl.textContent = info.sessionId;
      sessionLabel.textContent = info.sessionId;

      const streamUrl = new URL(info.basePath + "/api/stream", window.location.origin);
      streamUrl.searchParams.set("session", info.sessionId);
      if (info.token) {
        streamUrl.searchParams.set("token", info.token);
      }
      setStatus("connecting");
      const es = new EventSource(streamUrl.toString());
      es.onopen = () => setStatus("online");
      es.onerror = () => setStatus("offline");
      es.onmessage = (evt) => {
        try {
          const msg = JSON.parse(evt.data);
          updateCount += 1;
          updateCountEl.textContent = updateCount + " updates";
          if (msg.ts) {
            const ts = new Date(msg.ts);
            lastUpdateEl.textContent = ts.toLocaleTimeString();
          }
          addLog(msg.type + " @ " + new Date().toLocaleTimeString());
          if (msg.payload !== undefined) {
            renderPayload(msg.payload);
          }
        } catch (err) {
          console.warn("stream parse", err);
        }
      };
    })();
  </script>
</body>
</html>`
	defaultA2UIIndexHTML = `<!doctype html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Nexus A2UI</title>
  <style>
    :root { color-scheme: light; }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Space Grotesk", "Sora", "Fira Sans", sans-serif;
      background: radial-gradient(1200px 600px at 85% 15%, #f6f1ff, #f2f6ff 55%, #e9eef8 100%);
      color: #14121a;
    }
    main {
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 32px;
    }
    .panel {
      width: min(860px, 100%);
      background: rgba(255, 255, 255, 0.9);
      border: 1px solid #d9d7e8;
      border-radius: 22px;
      padding: 28px;
      box-shadow: 0 30px 70px rgba(26, 20, 52, 0.12);
      backdrop-filter: blur(8px);
    }
    .header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
    }
    h1 {
      margin: 0;
      font-size: 28px;
      letter-spacing: 0.3px;
    }
    .badge {
      font-size: 12px;
      padding: 6px 12px;
      border-radius: 999px;
      background: #2b1d4f;
      color: #f8f6ff;
      text-transform: uppercase;
      letter-spacing: 0.6px;
    }
    p {
      margin: 12px 0 0;
      color: #3c3a4a;
      line-height: 1.6;
    }
    .grid {
      margin-top: 18px;
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
      gap: 12px;
    }
    button {
      appearance: none;
      border: 1px solid #d6d2e6;
      background: #f7f6fb;
      padding: 12px 14px;
      border-radius: 12px;
      font-weight: 600;
      cursor: pointer;
      transition: transform 0.15s ease, box-shadow 0.15s ease;
    }
    button:hover {
      transform: translateY(-1px);
      box-shadow: 0 10px 20px rgba(39, 28, 80, 0.08);
    }
    .log {
      margin-top: 18px;
      font-family: "IBM Plex Mono", "Fira Mono", "Menlo", monospace;
      font-size: 12px;
      background: #f3f0fb;
      border: 1px solid #ddd8f0;
      padding: 12px;
      border-radius: 12px;
      color: #2a243d;
      min-height: 96px;
      white-space: pre-wrap;
    }
    .status {
      margin-top: 12px;
      font-size: 12px;
      color: #5b5970;
    }
    code {
      font-family: "IBM Plex Mono", "Fira Mono", "Menlo", monospace;
      background: #efeaf8;
      padding: 2px 6px;
      border-radius: 6px;
    }
  </style>
</head>
<body>
  <main>
    <section class="panel">
      <div class="header">
        <h1>Nexus A2UI Bridge</h1>
        <span class="badge">demo</span>
      </div>
      <p>Use this page to validate A2UI action bridges. Click a button to send a user action.</p>
      <div class="grid">
        <button data-action="hello" data-source="demo.hello">Hello</button>
        <button data-action="time" data-source="demo.time">Time</button>
        <button data-action="photo" data-source="demo.photo">Photo</button>
        <button data-action="dalek" data-source="demo.dalek">Dalek</button>
      </div>
      <div class="status" id="status">Bridge: checking...</div>
      <div class="log" id="log">Ready.</div>
      <p>Tip: replace this file in <code>a2ui_root</code> with your own UI when ready.</p>
    </section>
  </main>
  <script>
  (() => {
    const logEl = document.getElementById("log");
    const statusEl = document.getElementById("status");
    const log = (msg) => { logEl.textContent = String(msg); };

    const send =
      window.nexusSendUserAction ||
      window.Nexus?.sendUserAction ||
      window.clawdbotSendUserAction ||
      window.Clawdbot?.sendUserAction;

    const hasIOS = () => !!(window.webkit && window.webkit.messageHandlers &&
      (window.webkit.messageHandlers.nexusCanvasA2UIAction || window.webkit.messageHandlers.clawdbotCanvasA2UIAction));
    const hasAndroid = () => !!(window.nexusCanvasA2UIAction || window.clawdbotCanvasA2UIAction);
    statusEl.textContent =
      "Bridge: " + (send ? "ready" : "missing") +
      " | iOS=" + (hasIOS() ? "yes" : "no") +
      " | Android=" + (hasAndroid() ? "yes" : "no");

    const sendAction = (name, sourceComponentId) => {
      if (typeof send !== "function") {
        log("No action bridge found. Open this page in a Nexus node with A2UI enabled.");
        return;
      }
      const ok = send({
        name,
        surfaceId: "main",
        sourceComponentId,
        context: { t: Date.now() },
      });
      log(ok ? ("Sent action: " + name) : ("Failed to send action: " + name));
    };

    document.querySelectorAll("button[data-action]").forEach((btn) => {
      btn.addEventListener("click", () => {
        sendAction(btn.dataset.action, btn.dataset.source);
      });
    });
  })();
</script>
</body>
</html>`
)

//go:embed assets/dompurify.min.js
var dompurifyMinJS string

func renderIndexHTML() string {
	if dompurifyMinJS == "" {
		return defaultIndexHTML
	}
	return strings.Replace(defaultIndexHTML, dompurifyPlaceholder, dompurifyMinJS, 1)
}

// Host serves a canvas directory on a dedicated HTTP server with optional live reload.
type Host struct {
	host              string
	port              int
	root              string
	rootReal          string
	namespace         string
	a2uiRoot          string
	a2uiRootReal      string
	liveReload        bool
	injectClient      bool
	autoIndex         bool
	tokenSecret       []byte
	tokenTTL          time.Duration
	manager           *Manager
	actionCallback    ActionHandler
	actionLimiter     *ratelimit.Limiter
	actionDefaultRole string
	authService       *auth.Service
	metrics           *Metrics

	logger *slog.Logger

	server   *http.Server
	listener net.Listener

	mu          sync.RWMutex
	clients     map[*websocket.Conn]struct{}
	watcher     *fsnotify.Watcher
	watchCancel context.CancelFunc
	upgrader    websocket.Upgrader
}

type CanvasURLParams struct {
	RequestHost    string
	ForwardedProto string
	LocalAddress   string
	Scheme         string
	SessionID      string
	Token          string
}

// NewHost creates a canvas host for the given configuration.
func NewHost(cfg config.CanvasHostConfig, canvasCfg config.CanvasConfig, logger *slog.Logger) (*Host, error) {
	if strings.TrimSpace(cfg.Root) == "" {
		return nil, fmt.Errorf("canvas root is required")
	}
	if cfg.Port <= 0 {
		return nil, fmt.Errorf("canvas port must be set")
	}
	if logger == nil {
		logger = slog.Default()
	}
	namespace := normalizeNamespace(cfg.Namespace)
	liveReload := cfg.LiveReload != nil && *cfg.LiveReload
	injectClient := cfg.InjectClient != nil && *cfg.InjectClient
	autoIndex := cfg.AutoIndex != nil && *cfg.AutoIndex
	tokenSecret := strings.TrimSpace(canvasCfg.Tokens.Secret)
	var actionLimiter *ratelimit.Limiter
	if canvasCfg.Actions.RateLimit.Enabled {
		actionLimiter = ratelimit.NewLimiter(canvasCfg.Actions.RateLimit)
	}
	actionDefaultRole := strings.TrimSpace(canvasCfg.Actions.DefaultRole)
	if actionDefaultRole == "" {
		actionDefaultRole = RoleViewer
	}
	actionDefaultRole = NormalizeRole(actionDefaultRole)
	return &Host{
		host:              cfg.Host,
		port:              cfg.Port,
		root:              cfg.Root,
		namespace:         namespace,
		a2uiRoot:          strings.TrimSpace(cfg.A2UIRoot),
		liveReload:        liveReload,
		injectClient:      injectClient,
		autoIndex:         autoIndex,
		tokenSecret:       []byte(tokenSecret),
		tokenTTL:          canvasCfg.Tokens.TTL,
		logger:            logger.With("component", "canvas"),
		actionLimiter:     actionLimiter,
		actionDefaultRole: actionDefaultRole,
		clients:           make(map[*websocket.Conn]struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  8192,
			WriteBufferSize: 8192,
			CheckOrigin: func(*http.Request) bool {
				return true
			},
		},
	}, nil
}

// SetManager attaches a canvas manager to enable realtime APIs.
func (h *Host) SetManager(manager *Manager) {
	if h == nil {
		return
	}
	h.manager = manager
}

// SetActionHandler registers a handler for canvas UI actions.
func (h *Host) SetActionHandler(handler ActionHandler) {
	if h == nil {
		return
	}
	h.actionCallback = handler
}

// SetAuthService attaches an auth service for canvas requests.
func (h *Host) SetAuthService(service *auth.Service) {
	if h == nil {
		return
	}
	h.authService = service
}

// SetMetrics attaches metrics collectors for canvas activity.
func (h *Host) SetMetrics(metrics *Metrics) {
	if h == nil {
		return
	}
	h.metrics = metrics
}

// Start begins serving the canvas host and optional live reload watcher.
func (h *Host) Start(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if h.server != nil {
		return nil
	}
	if err := h.ensureRoot(); err != nil {
		return err
	}
	rootReal, err := filepath.EvalSymlinks(h.root)
	if err != nil {
		return fmt.Errorf("resolve canvas root: %w", err)
	}
	h.rootReal = rootReal
	if h.autoIndex {
		h.ensureIndex(h.root)
		if h.a2uiRoot != "" {
			h.ensureA2UI(h.a2uiRoot)
		}
	}

	mux := http.NewServeMux()

	canvasPrefix := h.canvasPrefix()
	mux.Handle(canvasPrefix+"/", http.StripPrefix(canvasPrefix+"/", h.canvasHandler()))
	mux.HandleFunc(canvasPrefix, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, canvasPrefix+"/", http.StatusFound)
	})
	mux.Handle(path.Join(canvasPrefix, "api/stream"), h.streamHandler())
	mux.Handle(path.Join(canvasPrefix, "api/action"), h.actionsHandler())

	if h.liveReload {
		mux.Handle(h.liveReloadScriptPath(), h.liveReloadScriptHandler())
		mux.Handle(h.liveReloadWSPath(), h.liveReloadWSHandler())
	}

	if h.a2uiRoot != "" {
		if info, err := os.Stat(h.a2uiRoot); err == nil && info.IsDir() {
			if a2uiReal, err := filepath.EvalSymlinks(h.a2uiRoot); err == nil {
				h.a2uiRootReal = a2uiReal
			} else {
				h.logger.Warn("canvas a2ui root resolve failed", "path", h.a2uiRoot, "error", err)
			}
			a2uiPrefix := h.a2uiPrefix()
			mux.Handle(a2uiPrefix+"/", http.StripPrefix(a2uiPrefix+"/", h.a2uiHandler()))
			mux.HandleFunc(a2uiPrefix, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, a2uiPrefix+"/", http.StatusFound)
			})
		} else if err != nil && !os.IsNotExist(err) {
			h.logger.Warn("canvas a2ui root unavailable", "path", h.a2uiRoot, "error", err)
		}
	}

	addr := net.JoinHostPort(h.host, strconv.Itoa(h.port))
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("canvas listen: %w", err)
	}
	var watcher *fsnotify.Watcher
	if h.liveReload {
		watcher, err = fsnotify.NewWatcher()
		if err != nil {
			if closeErr := listener.Close(); closeErr != nil {
				h.logger.Warn("failed to close canvas listener", "error", closeErr)
			}
			return err
		}
		if err := h.watchRecursive(watcher, h.root); err != nil {
			if closeErr := watcher.Close(); closeErr != nil {
				h.logger.Warn("failed to close canvas watcher", "error", closeErr)
			}
			if closeErr := listener.Close(); closeErr != nil {
				h.logger.Warn("failed to close canvas listener", "error", closeErr)
			}
			return err
		}
		if h.a2uiRoot != "" && h.a2uiRoot != h.root {
			if err := h.watchRecursive(watcher, h.a2uiRoot); err != nil {
				h.logger.Warn("failed to watch a2ui root", "path", h.a2uiRoot, "error", err)
			}
		}
	}

	h.server = server
	h.listener = listener

	if watcher != nil {
		watchCtx := ctx
		if watchCtx == nil {
			watchCtx = context.Background()
		}
		watchCtx, cancel := context.WithCancel(watchCtx)
		h.watchCancel = cancel
		h.watcher = watcher
		go h.watchLoop(watchCtx, watcher)
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.logger.Error("canvas server error", "error", err)
		}
	}()

	h.logger.Info("starting canvas host", "addr", addr, "root", h.root, "namespace", h.namespace)
	return nil
}

// Close shuts down the canvas host and watcher.
func (h *Host) Close() error {
	if h == nil {
		return nil
	}
	if h.watchCancel != nil {
		h.watchCancel()
		h.watchCancel = nil
	}
	if h.watcher != nil {
		if err := h.watcher.Close(); err != nil {
			h.logger.Warn("failed to close canvas watcher", "error", err)
		}
		h.watcher = nil
	}
	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.server.Shutdown(ctx); err != nil {
			h.logger.Warn("canvas server shutdown error", "error", err)
		}
		h.server = nil
		h.listener = nil
	}
	h.closeClients()
	return nil
}

// CanvasURL returns the absolute URL for the canvas root.
// requestHost should be the host name from the incoming client request (without port).
func (h *Host) CanvasURL(requestHost string) string {
	return h.CanvasURLWithParams(CanvasURLParams{RequestHost: requestHost})
}

// CanvasSessionURL returns the absolute URL for a session-specific canvas path.
func (h *Host) CanvasSessionURL(params CanvasURLParams, sessionID string) string {
	params.SessionID = sessionID
	return h.CanvasURLWithParams(params)
}

// SignedSessionURL returns a signed session-specific canvas URL.
func (h *Host) SignedSessionURL(params CanvasURLParams, sessionID string, role string, userID string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("canvas host is nil")
	}
	token, err := h.signSessionToken(sessionID, role, userID)
	if err != nil {
		return "", err
	}
	params.SessionID = sessionID
	params.Token = token
	return h.CanvasURLWithParams(params), nil
}

// CanvasURLWithParams returns the absolute URL for the canvas root using request details.
func (h *Host) CanvasURLWithParams(params CanvasURLParams) string {
	if h == nil {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(params.Scheme))
	if scheme == "" {
		if strings.EqualFold(strings.TrimSpace(firstForwardedProto(params.ForwardedProto)), "https") {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	override := normalizeHost(h.host, true)
	requestHost := normalizeHost(parseHostHeader(params.RequestHost), override != "")
	localAddress := normalizeHost(parseHostHeader(params.LocalAddress), override != "" || requestHost != "")

	host := override
	if host == "" {
		host = requestHost
	}
	if host == "" {
		host = localAddress
	}
	if host == "" {
		host = "localhost"
	}
	host = trimHostBrackets(host)
	hostPort := net.JoinHostPort(host, strconv.Itoa(h.port))
	basePath := h.canvasPrefix()
	if params.SessionID != "" {
		basePath = path.Join(basePath, params.SessionID)
	}
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	parsed := url.URL{
		Scheme: scheme,
		Host:   hostPort,
		Path:   basePath,
	}
	if token := strings.TrimSpace(params.Token); token != "" {
		q := parsed.Query()
		q.Set("token", token)
		parsed.RawQuery = q.Encode()
	}
	return parsed.String()
}

func (h *Host) canvasHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte("Method Not Allowed")) //nolint:errcheck
			return
		}
		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if strings.HasPrefix(clean, "/..") {
			http.NotFound(w, r)
			return
		}
		sessionID, sessionPath, err := h.sessionFromPath(clean)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if sessionID != "" && strings.TrimSpace(h.root) != "" {
			candidate := filepath.Join(h.root, sessionID)
			if info, statErr := os.Stat(candidate); statErr == nil && info != nil {
				if h.manager != nil && h.manager.Store() != nil {
					if _, err := h.manager.Store().GetSession(r.Context(), sessionID); errors.Is(err, ErrNotFound) {
						sessionID = ""
						sessionPath = clean
					}
				}
			}
		}

		var fullPath string
		if sessionID != "" {
			if _, _, err := h.authorizeSessionRequest(r, sessionID); err != nil {
				h.writeTokenError(w, err)
				return
			}
			sessionRoot, err := h.ensureSessionRoot(sessionID)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			fullPath, err = h.resolveFilePathWithRoot(sessionPath, sessionRoot, "", h.autoIndex)
			if err != nil {
				if sessionPath == "/" || strings.HasSuffix(sessionPath, "/") {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte("<!doctype html><meta charset=\"utf-8\" /><title>Nexus Canvas</title><pre>Missing file. Create index.html</pre>")) //nolint:errcheck
					return
				}
				http.NotFound(w, r)
				return
			}
		} else {
			var err error
			fullPath, err = h.resolveFilePath(clean)
			if err != nil {
				if clean == "/" || strings.HasSuffix(clean, "/") {
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte("<!doctype html><meta charset=\"utf-8\" /><title>Nexus Canvas</title><pre>Missing file. Create index.html</pre>")) //nolint:errcheck
					return
				}
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasSuffix(strings.ToLower(fullPath), ".html") {
			h.serveHTML(w, r, fullPath)
			return
		}
		http.ServeFile(w, r, fullPath)
	})
}

func (h *Host) streamHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h == nil || h.manager == nil || h.manager.Hub() == nil {
			http.Error(w, "canvas stream unavailable", http.StatusServiceUnavailable)
			return
		}
		sessionID := strings.TrimSpace(r.URL.Query().Get("session"))
		if sessionID == "" {
			http.Error(w, "missing session", http.StatusBadRequest)
			return
		}
		if !validSessionID(sessionID) {
			http.NotFound(w, r)
			return
		}
		if _, _, err := h.authorizeSessionRequest(r, sessionID); err != nil {
			h.writeTokenError(w, err)
			return
		}
		if h.metrics != nil {
			h.metrics.ViewerConnected()
			defer h.metrics.ViewerDisconnected()
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		if store := h.manager.Store(); store != nil {
			if state, err := store.GetState(r.Context(), sessionID); err == nil && state != nil {
				if err := writeStreamMessage(w, StreamMessage{
					Type:      "state",
					SessionID: sessionID,
					Payload:   state.StateJSON,
					Timestamp: time.Now(),
				}); err != nil {
					h.logger.Warn("canvas stream write failed", "error", err)
					return
				}
				flusher.Flush()
			}
		}

		stream, cancel := h.manager.Hub().Subscribe(sessionID)
		defer cancel()

		keepalive := time.NewTicker(15 * time.Second)
		defer keepalive.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-stream:
				if err := writeStreamMessage(w, msg); err != nil {
					return
				}
				flusher.Flush()
			case <-keepalive.C:
				_, _ = w.Write([]byte(": keepalive\n\n")) //nolint:errcheck
				flusher.Flush()
			}
		}
	})
}

func (h *Host) actionsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte("Method Not Allowed")) //nolint:errcheck
			return
		}
		if h == nil || h.actionCallback == nil {
			http.Error(w, "canvas action handler unavailable", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			SessionID         string          `json:"session_id"`
			ID                string          `json:"id"`
			Name              string          `json:"name"`
			SourceComponentID string          `json:"source_component_id"`
			Context           json.RawMessage `json:"context"`
			UserID            string          `json:"user_id"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := decoder.Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		sessionID := strings.TrimSpace(req.SessionID)
		if sessionID == "" {
			http.Error(w, "session_id is required", http.StatusBadRequest)
			return
		}
		if !validSessionID(sessionID) {
			http.NotFound(w, r)
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		access, user, err := h.authorizeSessionRequest(r, sessionID)
		if err != nil {
			h.writeTokenError(w, err)
			return
		}
		role := RoleEditor
		if access != nil {
			role = NormalizeRole(access.Role)
		} else if user != nil {
			role = NormalizeRole(h.actionDefaultRole)
		}
		if !RoleAllowsAction(role) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		userID := strings.TrimSpace(req.UserID)
		if access != nil && strings.TrimSpace(access.UserID) != "" {
			userID = strings.TrimSpace(access.UserID)
		}
		if user != nil && strings.TrimSpace(user.ID) != "" {
			userID = strings.TrimSpace(user.ID)
		}
		if h.actionLimiter != nil {
			key := sessionID
			if userID != "" {
				key += ":" + userID
			}
			if !h.actionLimiter.Allow(key) {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
		}
		if h.metrics != nil {
			h.metrics.RecordAction()
		}

		action := Action{
			SessionID:         sessionID,
			ID:                strings.TrimSpace(req.ID),
			Name:              strings.TrimSpace(req.Name),
			SourceComponentID: strings.TrimSpace(req.SourceComponentID),
			Context:           req.Context,
			UserID:            userID,
			ReceivedAt:        time.Now(),
		}
		ctx := r.Context()
		if user != nil {
			ctx = auth.WithUser(ctx, user)
		}
		if err := h.actionCallback(ctx, action); err != nil {
			h.logger.Warn("canvas action handler failed", "error", err)
			http.Error(w, "action failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		if err := json.NewEncoder(w).Encode(map[string]any{"ok": true}); err != nil {
			h.logger.Warn("canvas action response failed", "error", err)
		}
	})
}

func (h *Host) a2uiHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte("Method Not Allowed")) //nolint:errcheck
			return
		}
		clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if strings.HasPrefix(clean, "/..") {
			http.NotFound(w, r)
			return
		}
		fullPath, err := h.resolveFilePathWithRoot(clean, h.a2uiRoot, h.a2uiRootReal, false)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasSuffix(strings.ToLower(fullPath), ".html") {
			h.serveHTML(w, r, fullPath)
			return
		}
		http.ServeFile(w, r, fullPath)
	})
}

func (h *Host) liveReloadWSHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := h.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		h.addClient(conn)
		defer h.removeClient(conn)

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
}

func (h *Host) liveReloadScriptHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		script := h.liveReloadScript()
		if _, err := io.WriteString(w, script); err != nil {
			h.logger.Warn("failed to write live reload script", "error", err)
		}
	})
}

func (h *Host) serveHTML(w http.ResponseWriter, r *http.Request, fullPath string) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	html := string(data)
	if h.injectClient && h.liveReload {
		html = h.injectLiveReload(html)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.WriteString(w, html); err != nil {
		h.logger.Warn("failed to write canvas html", "error", err)
	}
}

func (h *Host) injectLiveReload(html string) string {
	snippet := fmt.Sprintf("<script src=\"%s\"></script>", h.liveReloadScriptPath())
	if strings.Contains(html, snippet) || strings.Contains(html, h.liveReloadScriptPath()) {
		return html
	}
	if strings.Contains(html, "</body>") {
		return strings.Replace(html, "</body>", snippet+"</body>", 1)
	}
	if strings.Contains(html, "</head>") {
		return strings.Replace(html, "</head>", snippet+"</head>", 1)
	}
	return html + snippet
}

func (h *Host) liveReloadScript() string {
	return fmt.Sprintf(`(() => {
  const wsPath = %q;
  const scheme = window.location.protocol === "https:" ? "wss" : "ws";
  const wsUrl = scheme + "://" + window.location.host + wsPath;
  let socket = null;
  const handlerNames = ["nexusCanvasA2UIAction", "clawdbotCanvasA2UIAction"];

  const postToNode = (payload) => {
    try {
      const raw = typeof payload === "string" ? payload : JSON.stringify(payload);
      for (const handlerName of handlerNames) {
        const iosHandler = globalThis.webkit?.messageHandlers?.[handlerName];
        if (iosHandler && typeof iosHandler.postMessage === "function") {
          iosHandler.postMessage(raw);
          return true;
        }
        const androidHandler = globalThis[handlerName];
        if (androidHandler && typeof androidHandler.postMessage === "function") {
          androidHandler.postMessage(raw);
          return true;
        }
      }
    } catch {}
    return false;
  };

  const sendUserAction = (userAction) => {
    const id =
      (userAction && typeof userAction.id === "string" && userAction.id.trim()) ||
      (globalThis.crypto?.randomUUID?.() ?? String(Date.now()));
    const action = { ...userAction, id };
    return postToNode({ userAction: action });
  };

  globalThis.Nexus = globalThis.Nexus ?? {};
  globalThis.Nexus.sendUserAction = sendUserAction;
  globalThis.nexusSendUserAction = sendUserAction;
  globalThis.nexusPostMessage = postToNode;
  globalThis.Clawdbot = globalThis.Clawdbot ?? {};
  globalThis.Clawdbot.sendUserAction = sendUserAction;
  globalThis.clawdbotSendUserAction = sendUserAction;
  globalThis.clawdbotPostMessage = postToNode;

  const connect = () => {
    socket = new WebSocket(wsUrl);
    socket.addEventListener("message", (event) => {
      if (event.data === "reload") {
        window.location.reload();
      }
    });
    socket.addEventListener("close", () => {
      setTimeout(connect, 1000);
    });
  };

  connect();
})();
`, h.liveReloadWSPath())
}

func (h *Host) addClient(conn *websocket.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
}

func (h *Host) removeClient(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	_ = conn.Close() //nolint:errcheck
}

func (h *Host) closeClients() {
	h.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.clients = make(map[*websocket.Conn]struct{})
	h.mu.Unlock()
	for _, conn := range clients {
		_ = conn.Close() //nolint:errcheck
	}
}

func (h *Host) broadcastReload() {
	h.mu.RLock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for conn := range h.clients {
		clients = append(clients, conn)
	}
	h.mu.RUnlock()

	for _, conn := range clients {
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
		if err := conn.WriteMessage(websocket.TextMessage, []byte("reload")); err != nil {
			h.removeClient(conn)
		}
	}
}

func (h *Host) watchLoop(ctx context.Context, watcher *fsnotify.Watcher) {
	if watcher == nil {
		return
	}
	var mu sync.Mutex
	var timer *time.Timer
	debounce := 200 * time.Millisecond

	schedule := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounce, func() {
			h.broadcastReload()
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-watcher.Events:
			if !ok {
				return
			}
			if evt.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) != 0 {
				if shouldIgnorePath(evt.Name) {
					continue
				}
				if evt.Op&fsnotify.Create != 0 {
					info, err := os.Stat(evt.Name)
					if err == nil && info.IsDir() {
						if err := h.watchRecursive(watcher, evt.Name); err != nil {
							h.logger.Warn("failed to watch new directory", "path", evt.Name, "error", err)
						}
					}
				}
				schedule()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			h.logger.Warn("canvas watch error", "error", err)
		}
	}
}

func (h *Host) watchRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && shouldIgnorePath(path) {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

func (h *Host) ensureRoot() error {
	info, err := os.Stat(h.root)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(h.root, 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("canvas root is not a directory: %s", h.root)
	}
	return nil
}

func (h *Host) ensureIndex(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.logger.Warn("failed to create canvas directory", "path", dir, "error", err)
		return
	}
	if err := os.WriteFile(indexPath, []byte(renderIndexHTML()), 0o644); err != nil {
		h.logger.Warn("failed to write canvas index", "path", indexPath, "error", err)
	}
}

func (h *Host) ensureA2UI(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.logger.Warn("failed to create a2ui directory", "path", dir, "error", err)
		return
	}
	if err := os.WriteFile(indexPath, []byte(defaultA2UIIndexHTML), 0o644); err != nil {
		h.logger.Warn("failed to write a2ui index", "path", indexPath, "error", err)
	}
}

func (h *Host) canvasPrefix() string {
	return h.namespacedPath("canvas")
}

func (h *Host) a2uiPrefix() string {
	return h.namespacedPath("a2ui")
}

func (h *Host) liveReloadWSPath() string {
	return h.namespacedPath("ws")
}

func (h *Host) liveReloadScriptPath() string {
	return h.namespacedPath("live.js")
}

func (h *Host) namespacedPath(suffix string) string {
	suffix = strings.TrimPrefix(suffix, "/")
	if h.namespace == "/" {
		return "/" + suffix
	}
	return h.namespace + "/" + suffix
}

func trimHostBrackets(value string) string {
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	}
	return value
}

func isLoopbackHost(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(trimHostBrackets(value)))
	if normalized == "" {
		return false
	}
	switch normalized {
	case "localhost", "::1", "0.0.0.0", "::":
		return true
	}
	return strings.HasPrefix(normalized, "127.")
}

func normalizeHost(value string, rejectLoopback bool) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if rejectLoopback && isLoopbackHost(trimmed) {
		return ""
	}
	return trimmed
}

func parseHostHeader(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse("http://" + trimmed)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func firstForwardedProto(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	return strings.TrimSpace(parts[0])
}

func normalizeNamespace(namespace string) string {
	clean := strings.TrimSpace(namespace)
	if clean == "" {
		clean = "/__nexus__"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	clean = strings.TrimRight(clean, "/")
	if clean == "" {
		clean = "/"
	}
	return clean
}

func (h *Host) sessionFromPath(clean string) (string, string, error) {
	trimmed := strings.TrimPrefix(clean, "/")
	if trimmed == "" || trimmed == "." {
		return "", clean, nil
	}
	parts := strings.SplitN(trimmed, "/", 2)
	sessionID := parts[0]
	if h != nil && strings.Contains(sessionID, ".") && h.root != "" {
		candidate := filepath.Join(h.root, sessionID)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return "", clean, nil
		}
	}
	if !validSessionID(sessionID) {
		return "", "", os.ErrNotExist
	}
	sessionPath := "/"
	if len(parts) > 1 && parts[1] != "" {
		sessionPath = "/" + parts[1]
	}
	return sessionID, sessionPath, nil
}

func (h *Host) ensureSessionRoot(sessionID string) (string, error) {
	if !validSessionID(sessionID) {
		return "", os.ErrNotExist
	}
	sessionRoot := filepath.Join(h.root, sessionID)
	if h.autoIndex {
		h.ensureIndex(sessionRoot)
	} else if err := os.MkdirAll(sessionRoot, 0o755); err != nil {
		return "", err
	}
	return sessionRoot, nil
}

func (h *Host) authorizeSessionRequest(r *http.Request, sessionID string) (*AccessToken, *models.User, error) {
	if h == nil {
		return nil, nil, ErrUnauthorized
	}
	var access *AccessToken
	if len(h.tokenSecret) > 0 {
		token := extractCanvasToken(r)
		if token != "" {
			parsed, err := ParseAccessToken(h.tokenSecret, token)
			if err != nil {
				if h.authService == nil || !h.authService.Enabled() {
					return nil, nil, err
				}
			} else {
				if parsed.SessionID != sessionID {
					return nil, nil, ErrTokenInvalid
				}
				parsed.Role = NormalizeRole(parsed.Role)
				parsed.UserID = strings.TrimSpace(parsed.UserID)
				access = parsed
			}
		} else if h.authService == nil || !h.authService.Enabled() {
			return nil, nil, ErrTokenInvalid
		}
	}

	var user *models.User
	if h.authService != nil && h.authService.Enabled() {
		user = h.authenticateRequest(r)
		if user == nil && access == nil {
			return access, nil, ErrUnauthorized
		}
	}

	return access, user, nil
}

func (h *Host) authenticateRequest(r *http.Request) *models.User {
	if h == nil || h.authService == nil || !h.authService.Enabled() || r == nil {
		return nil
	}
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token := strings.TrimSpace(authHeader[len("bearer "):])
		if user, err := h.authService.ValidateJWT(token); err == nil {
			return user
		}
	}

	apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if apiKey == "" {
		apiKey = strings.TrimSpace(r.Header.Get("Api-Key"))
	}
	if apiKey != "" {
		if user, err := h.authService.ValidateAPIKey(apiKey); err == nil {
			return user
		}
	}

	if cookie, err := r.Cookie("nexus_session"); err == nil && strings.TrimSpace(cookie.Value) != "" {
		if user, err := h.authService.ValidateJWT(strings.TrimSpace(cookie.Value)); err == nil {
			return user
		}
	}

	return nil
}

func (h *Host) signSessionToken(sessionID string, role string, userID string) (string, error) {
	if h == nil || len(h.tokenSecret) == 0 {
		return "", ErrTokenInvalid
	}
	ttl := h.tokenTTL
	if ttl < 0 {
		ttl = 0
	}
	token := AccessToken{
		SessionID: sessionID,
		UserID:    strings.TrimSpace(userID),
		Role:      NormalizeRole(role),
	}
	if ttl > 0 {
		token.ExpiresAt = time.Now().Add(ttl).Unix()
	}
	return SignAccessToken(h.tokenSecret, token)
}

func (h *Host) writeTokenError(w http.ResponseWriter, err error) {
	status := http.StatusUnauthorized
	message := "Unauthorized"
	if errors.Is(err, ErrTokenExpired) {
		message = "Token expired"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(message)) //nolint:errcheck
}

func (h *Host) resolveFilePath(urlPath string) (string, error) {
	return h.resolveFilePathWithRoot(urlPath, h.root, h.rootReal, h.autoIndex)
}

func (h *Host) resolveFilePathWithRoot(urlPath string, root string, rootReal string, autoIndex bool) (string, error) {
	rootReal = strings.TrimSpace(rootReal)
	if rootReal == "" {
		rootReal = root
	}
	if resolved, err := filepath.EvalSymlinks(rootReal); err == nil {
		rootReal = resolved
	}

	normalized := path.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	if strings.HasPrefix(normalized, "/..") {
		return "", os.ErrNotExist
	}
	rel := strings.TrimPrefix(normalized, "/")
	candidate := filepath.Join(root, filepath.FromSlash(rel))

	info, err := os.Stat(candidate)
	if err == nil && info.IsDir() {
		if autoIndex {
			h.ensureIndex(candidate)
		}
		candidate = filepath.Join(candidate, "index.html")
	}

	lstat, err := os.Lstat(candidate)
	if err != nil {
		return "", err
	}
	if lstat.Mode()&os.ModeSymlink != 0 {
		return "", os.ErrNotExist
	}
	realPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}

	rootReal = filepath.Clean(rootReal)
	realPath = filepath.Clean(realPath)
	rootPrefix := rootReal
	if !strings.HasSuffix(rootPrefix, string(os.PathSeparator)) {
		rootPrefix += string(os.PathSeparator)
	}
	if realPath != rootReal && !strings.HasPrefix(realPath, rootPrefix) {
		return "", os.ErrNotExist
	}
	return realPath, nil
}

func extractCanvasToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		return token
	}
	if token := strings.TrimSpace(r.Header.Get("X-Canvas-Token")); token != "" {
		return token
	}
	if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); authHeader != "" {
		lower := strings.ToLower(authHeader)
		if strings.HasPrefix(lower, "bearer ") {
			return strings.TrimSpace(authHeader[len("bearer "):])
		}
	}
	return ""
}

func validSessionID(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "..") {
		return false
	}
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
}

func writeStreamMessage(w io.Writer, msg StreamMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
	return err
}

func shouldIgnorePath(p string) bool {
	if p == "" {
		return false
	}
	parts := strings.FieldsFunc(p, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			continue
		}
		if strings.HasPrefix(part, ".") {
			return true
		}
		if part == "node_modules" {
			return true
		}
	}
	return false
}
