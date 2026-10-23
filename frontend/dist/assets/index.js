const wails = window.go?.main?.App;

const demoPlan = {
  group: "DEFAULT_GROUP",
  sourceEnv: "local",
  targetEnv: "dev",
  items: [
    {
      sourceDataId: "dxtx-user-server-local.yml",
      targetDataId: "dxtx-user-server-dev.yml",
      serviceName: "dxtx-user-server",
      kind: "environment",
      status: "changed",
      targetExists: true,
      sourceContent: "server:\n  port: 8080\nspring:\n  datasource:\n    url: jdbc:mysql://10.0.0.1:3306/local\nfeature:\n  enabled: true\n",
      targetContent: "server:\n  port: 8081\nspring:\n  datasource:\n    url: jdbc:mysql://10.0.1.1:3306/dev\nfeature:\n  enabled: false\n",
      previewContent: "server:\n  port: 8080\nspring:\n  datasource:\n    url: jdbc:mysql://10.0.1.1:3306/dev\nfeature:\n  enabled: true\n",
      changes: [
        { path: "server.port", kind: "modify", sourceValue: 8080, targetValue: 8081 },
        { path: "feature.enabled", kind: "modify", sourceValue: true, targetValue: false }
      ],
      warnings: [
        { path: "spring.datasource.url", kind: "jdbc", message: "疑似环境相关配置，请确认是否需要同步" }
      ]
    }
  ]
};

const state = {
  tab: "scan",
  settings: { profiles: [], ignoreRules: [], recentLogs: [] },
  savedEndpoints: [],
  form: {
    source: { name: "local", baseUrl: "http://localhost:8080", username: "nacos", password: "nacos", namespaceId: "", apiVersion: "v3" },
    target: { name: "dev", baseUrl: "http://localhost:8080", username: "nacos", password: "nacos", namespaceId: "", apiVersion: "v3" },
    sourceEnv: "local",
    targetEnv: "dev",
    group: "DEFAULT_GROUP"
  },
  plan: null,
  drafts: {},
  decisions: {},
  selectedIndex: 0,
  selectedTargets: new Set(),
  uiScroll: { items: 0, content: 0, sidebar: 0, textareas: {} },
  resetScrollOnNextRender: false,
  toast: null
};

function appCall(name, ...args) {
  if (!wails || typeof wails[name] !== "function") {
    throw new Error("Wails runtime is not connected. Build/run this UI inside the desktop app.");
  }
  return wails[name](...args);
}

function render() {
  if (state.resetScrollOnNextRender) {
    state.uiScroll = { items: 0, content: 0, sidebar: 0, textareas: {} };
    state.resetScrollOnNextRender = false;
  } else {
    captureScrollPositions();
  }
  const selected = currentItem();
  document.getElementById("app").innerHTML = `
    <div class="app">
      <header class="topbar">
        <div class="brand">
          <h1>Nacos Config Sync</h1>
          <span>${wails ? "桌面运行时已连接" : "预览模式"} · ${state.form.sourceEnv || "-"} → ${state.form.targetEnv || "-"}</span>
        </div>
        <div class="top-actions">
          <button id="load-settings">加载配置</button>
          <button id="save-profile">保存连接</button>
          <button class="primary" id="scan">扫描差异</button>
          <button class="primary" id="publish" ${publishPayload().length === 0 ? "disabled" : ""}>发布选中</button>
        </div>
      </header>
      <nav class="tabs">
        ${tabButton("scan", "扫描与对比")}
        ${tabButton("rules", "忽略规则")}
        ${tabButton("logs", "同步日志")}
        <span class="statusline">${statusText()}</span>
      </nav>
      <div class="layout">
        ${sidebar()}
        <main class="content">
          ${state.tab === "scan" ? scanView(selected) : ""}
          ${state.tab === "rules" ? rulesView() : ""}
          ${state.tab === "logs" ? logsView() : ""}
        </main>
      </div>
      ${state.toast ? `<div class="toast ${state.toast.type || ""}">${escapeHtml(state.toast.message)}</div>` : ""}
    </div>
  `;
  bindEvents();
  restoreScrollPositions();
}

function tabButton(id, label) {
  return `<button class="tab ${state.tab === id ? "active" : ""}" data-tab="${id}">${label}</button>`;
}

function statusText() {
  if (!state.plan) return "尚未扫描";
  const counts = countStatuses(state.plan.items || []);
  return `共 ${state.plan.items?.length || 0} 个 · 修改 ${counts.changed || 0} · 新建 ${counts.create || 0} · 目标更多 ${counts.targetExtra || 0} · 跳过 ${counts.skipped || 0}`;
}

function sidebar() {
  return `
    <aside class="sidebar">
      <section class="section">
        <h2>源 Nacos</h2>
        <div class="form-grid">
          ${savedEndpointSelect("source")}
          ${input("source.baseUrl", "地址", "http://localhost:8080")}
          ${select("source.apiVersion", "API版本", ["auto", "v1", "v2", "v3"])}
          <div class="field-row">
            ${input("source.username", "账号", "nacos")}
            ${input("source.password", "密码", "nacos", "password")}
          </div>
          ${input("source.namespaceId", "Namespace", "public")}
        </div>
      </section>
      <div class="swap-row">
        <button id="swap-endpoints" class="swap-button" title="交换源和目标 Nacos">⇅ 交换源/目标</button>
      </div>
      <section class="section">
        <h2>目标 Nacos</h2>
        <div class="form-grid">
          ${savedEndpointSelect("target")}
          ${input("target.baseUrl", "地址", "http://localhost:8080")}
          ${select("target.apiVersion", "API版本", ["auto", "v1", "v2", "v3"])}
          <div class="field-row">
            ${input("target.username", "账号", "nacos")}
            ${input("target.password", "密码", "nacos", "password")}
          </div>
          ${input("target.namespaceId", "Namespace", "public")}
        </div>
      </section>
      <section class="section">
        <h2>同步参数</h2>
        <div class="form-grid">
          <div class="field-row">
            ${select("sourceEnv", "源环境", ["local", "dev", "test", "prod"])}
            ${select("targetEnv", "目标环境", ["local", "dev", "test", "prod"])}
          </div>
          ${input("group", "Group", "DEFAULT_GROUP")}
        </div>
      </section>
    </aside>
  `;
}

function savedEndpointSelect(role) {
  const options = state.savedEndpoints || [];
  const value = endpointIdForForm(state.form[role]);
  return `
    <div class="field">
      <label>已保存配置</label>
      <select data-saved-endpoint="${role}" ${options.length ? "" : "disabled"}>
        <option value="">手动输入</option>
        ${options.map((endpoint) => `<option value="${escapeAttr(endpoint.id)}" ${endpoint.id === value ? "selected" : ""}>${escapeHtml(endpoint.label || endpoint.baseUrl || endpoint.id)}</option>`).join("")}
      </select>
    </div>
  `;
}

function input(path, label, placeholder, type = "text") {
  return `
    <div class="field">
      <label>${label}</label>
      <input data-field="${path}" type="${type}" value="${escapeAttr(getPath(state.form, path) || "")}" placeholder="${escapeAttr(placeholder)}" />
    </div>
  `;
}

function select(path, label, options) {
  const value = getPath(state.form, path);
  return `
    <div class="field">
      <label>${label}</label>
      <select data-field="${path}">
        ${options.map((option) => `<option value="${option}" ${option === value ? "selected" : ""}>${option}</option>`).join("")}
      </select>
    </div>
  `;
}

function scanView(selected) {
  if (!state.plan) {
    return `
      <section class="panel">
        <div class="panel-header"><h2>扫描与对比</h2></div>
        <div class="empty">输入两端 Nacos 连接后点击扫描。未连接桌面运行时时，可使用预览数据查看界面。</div>
      </section>
    `;
  }
  return `
    <div class="split">
      <section class="panel">
        <div class="panel-header">
          <h2>配置文件</h2>
          <button id="select-all">选择可发布</button>
        </div>
        <div class="panel-body">
          <div class="items">
            ${(state.plan.items || []).map((item, index) => itemButton(item, index)).join("")}
          </div>
        </div>
      </section>
      <section class="panel">
        <div class="panel-header">
          <h2>${selected ? escapeHtml(selected.targetDataId || selected.sourceDataId) : "差异详情"}</h2>
          ${selected ? `<label class="target-select-label"><input type="checkbox" data-target-select="${escapeAttr(selected.targetDataId || "")}" ${state.selectedTargets.has(selected.targetDataId) ? "checked" : ""} ${canPublish(selected) ? "" : "disabled"} /> 选中发布</label>` : ""}
        </div>
        <div class="panel-body">${selected ? diffView(selected) : `<div class="empty">请选择一个配置文件</div>`}</div>
      </section>
    </div>
  `;
}

function itemButton(item, index) {
  const target = item.targetDataId || "";
  const message = item.message || "";
  const pending = pendingChangeCount(item);
  const handled = handledChangeCount(item);
  const warnings = visibleWarnings(item).length;
  const badge = itemBadge(item);
  return `
    <button class="item ${state.selectedIndex === index ? "active" : ""}" data-index="${index}">
      <span class="item-title">
        <span>${escapeHtml(item.sourceDataId)}</span>
        <span class="badge ${escapeAttr(badge.className)}">${escapeHtml(badge.label)}</span>
      </span>
      <span class="item-subtitle">${escapeHtml(target || message || "")}</span>
      ${message ? `<span class="item-error">${escapeHtml(message)}</span>` : ""}
      <span class="item-subtitle">${itemSubtitle(item, pending, handled, warnings)}</span>
    </button>
  `;
}

function diffView(item) {
  const warnings = visibleWarnings(item);
  const warningBox = warnings.length
    ? `<div class="warning-box">${warnings.map((w) => `${escapeHtml(w.path)} · ${escapeHtml(sensitiveLabel(w.kind))} · ${escapeHtml(w.message || "")}`).join("<br>")}</div>`
    : "";
  const error = item.status === "error" && item.message
    ? `<div class="error-box">${escapeHtml(item.message)}</div>`
    : "";
  const createNotice = item.status === "create" || !item.targetExists
    ? `<div class="info-box">目标配置不存在，将按右侧内容新建；右侧内容可直接编辑。</div>`
    : "";
  const targetExtraNotice = isTargetExtra(item)
    ? `<div class="info-box target-extra-box">目标存在额外配置，源侧没有对应项；相同部分一致，无需同步到目标。</div>`
    : "";
  const mergeActions = item.status === "create" || !item.targetExists || isTargetExtra(item)
    ? ""
    : `<div class="merge-actions">
        <button data-apply-all="${escapeAttr(item.targetDataId || "")}" ${canPublish(item) ? "" : "disabled"}>应用全部差异</button>
        <button data-reset-draft="${escapeAttr(item.targetDataId || "")}" ${canPublish(item) ? "" : "disabled"}>恢复目标原文</button>
      </div>`;
  const targetTitle = item.targetExists ? "目标内容（可编辑，发布以此为准）" : "新建内容（可编辑，发布以此为准）";
  return `
    ${error}
    ${createNotice}
    ${targetExtraNotice}
    ${warningBox}
    ${mergeActions}
    <div class="diff-grid">
      ${codePane("源配置", item, "source")}
      ${editorPane(targetTitle, item)}
    </div>
  `;
}

function editorPane(title, item) {
  const key = draftKey(item);
  const content = draftContent(item);
  return `
    <div class="code-pane">
      <div class="code-title">${title}${hasDraftChange(item) ? `<span class="dirty">已编辑</span>` : ""}</div>
      <div class="editor-wrap target-editor-wrap">
        <div class="line-gutter" data-line-gutter="${escapeAttr(key)}">${lineNumbers(content)}</div>
        <div class="editor-shell">
          <textarea class="code-editor" data-draft="${escapeAttr(key)}" data-target-id="${escapeAttr(item.targetDataId || "")}" spellcheck="false">${escapeHtml(content)}</textarea>
          ${targetLineOverlay(item, content)}
        </div>
      </div>
    </div>
  `;
}

function changeRail(item) {
  const changes = item.changes || [];
  if (!changes.length) {
    return `<div class="empty compact">没有需要同步的差异</div>`;
  }
  return `
    <div class="changes compact-changes">
      ${changes.map((change) => changeRow(item, change)).join("")}
    </div>
  `;
}

function codePane(title, item, side) {
  const content = side === "source" ? item.sourceContent || "" : draftContent(item);
  const rendered = renderLines(content, item, side);
  return `
    <div class="code-pane">
      <div class="code-title">${title}</div>
      <pre>${rendered}</pre>
    </div>
  `;
}

function renderLines(content, item, side) {
  if (item.status === "create" || !item.targetExists || isTargetExtra(item)) {
    return String(content || "")
      .split("\n")
      .map((line, index) => `<span class="line"><span class="line-no">${index + 1}</span><span class="line-text">${escapeHtml(line || " ")}</span></span>`)
      .join("");
  }
  const changesByPath = new Map((item.changes || []).map((change) => [change.path, change]));
  const listDiffs = listDiffsForChanges(item.changes || []);
  const stack = [];
  return String(content || "")
    .split("\n")
    .map((line, index) => {
      const info = yamlLineInfo(line, stack);
      const path = info.path;
      const exactChange = path ? changesByPath.get(path) : null;
      const listDiff = path ? listDiffs.get(path) : null;
      const sourceOnlyListItem = side === "source" && info.type === "listItem" && !!listDiff?.sourceOnly.has(info.valueKey);
      const showExactChange = exactChange && !listDiff?.hasItemDiff;
      const change = sourceOnlyListItem ? changesByPath.get(path) : (showExactChange ? exactChange : null);
      const decision = sourceOnlyListItem ? listDecisionValue(item, path, info.valueKey, "source") : (change ? decisionValue(item, change.path) : "");
      if (decision) {
        return `<span class="line"><span class="line-no">${index + 1}</span><span class="line-text">${escapeHtml(line || " ")}</span></span>`;
      }
      const klass = change
        ? `${side === "source" ? "add" : "mod"} ${decision ? `handled-line ${decision}` : ""}`
        : "";
      const actions = sourceOnlyListItem ? inlineListItemActions(item, path, info.valueKey, "source", "add", "源多此项") : (side === "source" && change ? inlineChangeActions(item, change) : "");
      return `<span class="line ${klass}"><span class="line-no">${index + 1}</span><span class="line-text">${escapeHtml(line || " ")}</span>${actions}</span>`;
    })
    .join("");
}

function yamlLineInfo(line, stack) {
  const listMatch = String(line).match(/^(\s*)-\s*(.*)$/);
  if (listMatch) {
    const indent = listMatch[1].length;
    while (stack.length && stack[stack.length - 1].indent > indent) {
      stack.pop();
    }
    const path = stack.map((entry) => entry.key).join(".");
    return { path, type: "listItem", valueKey: lineListValueKey(listMatch[2].trim()) };
  }
  const match = String(line).match(/^(\s*)(?:-\s*)?(['"]?[^:'"]+['"]?)\s*:/);
  if (!match) return { path: "", type: "" };
  const indent = match[1].length;
  const key = match[2].replace(/^['"]|['"]$/g, "");
  while (stack.length && stack[stack.length - 1].indent >= indent) {
    stack.pop();
  }
  const path = [...stack.map((entry) => entry.key), key].join(".");
  const rest = line.slice(match[0].length).trim();
  if (rest === "") {
    stack.push({ indent, key });
  }
  return { path, type: "key", valueKey: "" };
}

function inlineChangeActions(item, change, hint = "") {
  const decision = decisionValue(item, change.path);
  const ignored = decision === "ignore";
  return `
    <span class="inline-actions ${ignored ? "disabled" : ""}">
      ${hint ? `<span class="inline-hint">${escapeHtml(hint)}</span>` : ""}
      <button class="icon-btn ${decision === "source" ? "active" : ""}" title="${escapeAttr(applyLabel(change.kind))}" data-apply-change data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(change.path)}" ${ignored ? "disabled" : ""}>→</button>
      <button class="icon-btn ${decision === "target" ? "active" : ""}" title="${escapeAttr(keepLabel(change.kind))}" data-keep-change data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(change.path)}" ${ignored ? "disabled" : ""}>×</button>
      <button class="icon-btn ${ignored ? "active" : ""}" title="永久忽略" data-ignore="${escapeAttr(item.targetDataId)}|${escapeAttr(change.path)}" ${ignored ? "disabled" : ""}>忽</button>
    </span>
  `;
}

function inlineListItemActions(item, path, value, side, action, hint) {
  const decision = listDecisionValue(item, path, value, side);
  const ignored = decision === "ignore";
  return `
    <span class="inline-actions ${ignored ? "disabled" : ""}">
      ${hint ? `<span class="inline-hint">${escapeHtml(hint)}</span>` : ""}
      <button class="icon-btn ${decision === "source" ? "active" : ""}" title="${action === "delete" ? "删除目标项" : "插入此项"}" data-list-apply data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(path)}" data-value-key="${escapeAttr(value)}" data-list-side="${escapeAttr(side)}" data-action="${escapeAttr(action)}" ${ignored ? "disabled" : ""}>→</button>
      <button class="icon-btn ${decision === "target" ? "active" : ""}" title="${action === "delete" ? "保留目标项" : "不需要此项"}" data-list-keep data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(path)}" data-value-key="${escapeAttr(value)}" data-list-side="${escapeAttr(side)}" ${ignored ? "disabled" : ""}>×</button>
      <button class="icon-btn ${ignored ? "active" : ""}" title="永久忽略此项" data-list-ignore data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(path)}" data-value-key="${escapeAttr(value)}" data-list-side="${escapeAttr(side)}" ${ignored ? "disabled" : ""}>忽</button>
    </span>
  `;
}

function targetLineOverlay(item, content) {
  if (item.status === "create" || !item.targetExists || isTargetExtra(item)) return "";
  const listDiffs = listDiffsForChanges(item.changes || []);
  if (!listDiffs.size) return "";
  const stack = [];
  const marks = String(content || "")
    .split("\n")
    .map((line, index) => {
      const info = yamlLineInfo(line, stack);
      const listDiff = info.path ? listDiffs.get(info.path) : null;
      if (info.type !== "listItem" || !listDiff?.targetOnly.has(info.valueKey)) {
        return "";
      }
      const decision = listDecisionValue(item, info.path, info.valueKey, "target");
      if (decision) {
        return "";
      }
      return `
        <div class="editor-line-mark ${decision ? `handled-line ${escapeAttr(decision)}` : ""}" style="top:${10 + index * 18}px">
          <span class="inline-hint">目标多此项</span>
          <button class="icon-btn ${decision === "source" ? "active" : ""}" title="删除目标项" data-list-apply data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(info.path)}" data-value-key="${escapeAttr(info.valueKey)}" data-list-side="target" data-action="delete">×</button>
          <button class="icon-btn ${decision === "target" ? "active" : ""}" title="保留目标项" data-list-keep data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(info.path)}" data-value-key="${escapeAttr(info.valueKey)}" data-list-side="target">→</button>
          <button class="icon-btn" title="永久忽略此项" data-list-ignore data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(info.path)}" data-value-key="${escapeAttr(info.valueKey)}" data-list-side="target">忽</button>
        </div>
      `;
    })
    .join("");
  if (!marks.trim()) return "";
  return `<div class="editor-overlay"><div class="editor-overlay-content" data-editor-overlay="${escapeAttr(draftKey(item))}">${marks}</div></div>`;
}

function changeRow(item, change) {
  const decision = decisionValue(item, change.path);
  const ignored = decision === "ignore";
  return `
    <div class="change ${decision ? `handled ${escapeAttr(decision)}` : ""}">
      <span>${escapeHtml(change.path)} · ${escapeHtml(changeKindLabel(change.kind))}${changeSummary(change)}${change.sensitive && change.sensitive !== "none" ? ` · ${escapeHtml(sensitiveLabel(change.sensitive))}` : ""}${decision ? ` · ${escapeHtml(decisionLabel(decision))}` : ""}</span>
      <span class="change-actions">
        <button class="${decision === "source" ? "active-action" : ""}" data-apply-change data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(change.path)}" ${ignored ? "disabled" : ""}>${escapeHtml(applyLabel(change.kind))}</button>
        <button class="${decision === "target" ? "active-action" : ""}" data-keep-change data-target-id="${escapeAttr(item.targetDataId || "")}" data-path="${escapeAttr(change.path)}" ${ignored ? "disabled" : ""}>${escapeHtml(keepLabel(change.kind))}</button>
        <button class="${ignored ? "active-action" : ""}" data-ignore="${escapeAttr(item.targetDataId)}|${escapeAttr(change.path)}" ${ignored ? "disabled" : ""}>永久忽略</button>
      </span>
    </div>
  `;
}

function rulesView() {
  return `
    <section class="panel">
      <div class="panel-header"><h2>忽略规则</h2></div>
      <div class="panel-body">
        <div class="rules-list">
          ${(state.settings.ignoreRules || []).map((rule) => `
            <div class="change">
              <span>${escapeHtml(rule.dataId)} · ${escapeHtml(rule.path)}</span>
              <span class="change-actions">
                <span>${escapeHtml(rule.reason || "")}</span>
                <button data-remove-ignore="${escapeAttr(rule.dataId)}|${escapeAttr(rule.path)}">取消忽略</button>
              </span>
            </div>
          `).join("") || `<div class="empty">暂无永久忽略规则</div>`}
        </div>
      </div>
    </section>
  `;
}

function logsView() {
  return `
    <section class="panel">
      <div class="panel-header"><h2>同步日志</h2></div>
      <div class="panel-body">
        <div class="rules-list">
          ${(state.settings.recentLogs || []).slice().reverse().map((log) => `
            <div class="change">
              <span>${escapeHtml(log.sourceDataId)} → ${escapeHtml(log.targetDataId)}</span>
              <span>${escapeHtml(log.time || "")}</span>
            </div>
          `).join("") || `<div class="empty">暂无发布记录</div>`}
        </div>
      </div>
    </section>
  `;
}

function bindEvents() {
  document.querySelectorAll("[data-field]").forEach((el) => {
    el.addEventListener("input", () => {
      setPath(state.form, el.dataset.field, el.value);
    });
  });
  document.querySelectorAll("[data-saved-endpoint]").forEach((el) => {
    el.addEventListener("change", () => {
      applySavedEndpoint(el.dataset.savedEndpoint, el.value);
      render();
    });
  });
  document.querySelectorAll("[data-tab]").forEach((el) => {
    el.addEventListener("click", () => {
      state.tab = el.dataset.tab;
      render();
    });
  });
  document.querySelectorAll("[data-index]").forEach((el) => {
    el.addEventListener("click", () => {
      state.selectedIndex = Number(el.dataset.index);
      render();
    });
  });
  document.querySelectorAll("[data-target-select]").forEach((el) => {
    el.addEventListener("change", () => {
      if (el.checked) state.selectedTargets.add(el.dataset.targetSelect);
      else state.selectedTargets.delete(el.dataset.targetSelect);
      render();
    });
  });
  document.querySelectorAll("[data-draft]").forEach((el) => {
    el.addEventListener("input", () => {
      state.drafts[el.dataset.draft] = el.value;
      if (el.dataset.targetId) state.selectedTargets.add(el.dataset.targetId);
      state.uiScroll.textareas[el.dataset.draft] = el.scrollTop;
      updateLineGutter(el);
      refreshPublishState();
    });
    el.addEventListener("scroll", () => {
      const gutter = document.querySelector(`[data-line-gutter="${cssEscape(el.dataset.draft)}"]`);
      if (gutter) gutter.scrollTop = el.scrollTop;
      state.uiScroll.textareas[el.dataset.draft] = el.scrollTop;
      syncEditorOverlay(el);
    });
  });
  document.querySelectorAll("[data-apply-change]").forEach((el) => {
    el.addEventListener("click", async () => {
      await applyChange(el.dataset.targetId, el.dataset.path, "source");
    });
  });
  document.querySelectorAll("[data-keep-change]").forEach((el) => {
    el.addEventListener("click", async () => {
      await applyChange(el.dataset.targetId, el.dataset.path, "target");
    });
  });
  document.querySelectorAll("[data-list-apply]").forEach((el) => {
    el.addEventListener("click", async () => {
      await applyListItemChange(el.dataset.targetId, el.dataset.path, el.dataset.valueKey, el.dataset.action, el.dataset.listSide);
    });
  });
  document.querySelectorAll("[data-list-keep]").forEach((el) => {
    el.addEventListener("click", async () => {
      await keepListItemChange(el.dataset.targetId, el.dataset.path, el.dataset.valueKey, el.dataset.listSide);
    });
  });
  document.querySelectorAll("[data-list-ignore]").forEach((el) => {
    el.addEventListener("click", async () => {
      await ignoreListItemChange(el.dataset.targetId, el.dataset.path, el.dataset.valueKey, el.dataset.listSide);
    });
  });
  document.querySelectorAll("[data-apply-all]").forEach((el) => {
    el.addEventListener("click", async () => {
      await applyAllChanges(el.dataset.applyAll);
    });
  });
  document.querySelectorAll("[data-reset-draft]").forEach((el) => {
    el.addEventListener("click", () => {
      resetDraft(el.dataset.resetDraft);
    });
  });
  document.querySelectorAll("[data-ignore]").forEach((el) => {
    el.addEventListener("click", async () => {
      const [dataId, path] = el.dataset.ignore.split("|");
      await ignoreChange(dataId, path);
    });
  });
  document.querySelectorAll("[data-remove-ignore]").forEach((el) => {
    el.addEventListener("click", async () => {
      const [dataId, path] = el.dataset.removeIgnore.split("|");
      await removeIgnoreRule(dataId, path);
    });
  });
  document.getElementById("swap-endpoints")?.addEventListener("click", swapEndpoints);
  document.getElementById("load-settings")?.addEventListener("click", loadSettings);
  document.getElementById("save-profile")?.addEventListener("click", saveProfile);
  document.getElementById("scan")?.addEventListener("click", scan);
  document.getElementById("publish")?.addEventListener("click", publishSelected);
  document.getElementById("select-all")?.addEventListener("click", () => {
    (state.plan?.items || []).filter((item) => canPublish(item) && hasDraftChange(item)).forEach((item) => state.selectedTargets.add(item.targetDataId));
    render();
  });
}

async function loadSettings() {
  await run(async () => {
    state.settings = await appCall("LoadSettings");
    await refreshSavedEndpoints();
    const profile = state.settings.profiles?.[0];
    if (profile) {
      state.form = {
        source: {
          name: profile.source?.name || "local",
          baseUrl: profile.source?.baseUrl || "http://localhost:8080",
          username: profile.source?.username || "nacos",
          password: profile.source?.passwordRef ? "" : "nacos",
          passwordRef: profile.source?.passwordRef || "",
          namespaceId: profile.source?.namespaceId || "",
          apiVersion: profile.source?.apiVersion || "v3"
        },
        target: {
          name: profile.target?.name || "dev",
          baseUrl: profile.target?.baseUrl || "http://localhost:8080",
          username: profile.target?.username || "nacos",
          password: profile.target?.passwordRef ? "" : "nacos",
          passwordRef: profile.target?.passwordRef || "",
          namespaceId: profile.target?.namespaceId || "",
          apiVersion: profile.target?.apiVersion || "v3"
        },
        sourceEnv: profile.sourceEnv || "local",
        targetEnv: profile.targetEnv || "dev",
        group: profile.group || "DEFAULT_GROUP"
      };
    }
    toast("配置已加载");
  });
}

async function saveProfile() {
  await run(async () => {
    if (!wails) {
      state.settings = {
        ...state.settings,
        profiles: [{
          name: `${state.form.sourceEnv}-to-${state.form.targetEnv}`,
          source: endpointForSave(state.form.source),
          target: endpointForSave(state.form.target),
          sourceEnv: state.form.sourceEnv,
          targetEnv: state.form.targetEnv,
          group: state.form.group
        }]
      };
      toast("预览模式下已模拟保存");
      return;
    }
    state.settings = await appCall("SaveProfile", state.form);
    await refreshSavedEndpoints();
    toast("连接配置已保存，密码不会写入配置文件");
  });
}

async function scan() {
  await run(async () => {
    if (!wails) {
      state.plan = demoPlan;
      initializeDrafts();
      state.selectedIndex = 0;
      state.selectedTargets.clear();
      state.resetScrollOnNextRender = true;
      toast("当前是预览模式，已加载示例差异");
      return;
    }
    state.plan = await appCall("Scan", state.form);
    state.settings = await appCall("SaveProfile", state.form);
    await refreshSavedEndpoints();
    initializeDrafts();
    state.selectedIndex = 0;
    state.selectedTargets.clear();
    state.resetScrollOnNextRender = true;
    toast("扫描完成，连接配置已保存");
  });
}

async function publishSelected() {
  await run(async () => {
    const payload = publishPayload();
    if (payload.length === 0) {
      toast("没有可发布的编辑内容，请先应用差异或修改右侧目标内容", "error");
      return;
    }
    const edited = Object.fromEntries(payload.map((item) => [item.targetDataId, item.content]));
    const result = await appCall("Publish", state.form, payload.map((item) => item.targetDataId), edited);
    markPublishedItems(result, edited);
    state.settings = await appCall("LoadSettings");
    toast(`发布完成：${(result.logs || []).length} 个配置`);
  });
}

function markPublishedItems(result, editedContents) {
  const successIDs = new Set((result?.logs || []).filter((log) => log.success).map((log) => log.targetDataId));
  if (!successIDs.size) return;
  (state.plan?.items || []).forEach((item) => {
    if (!successIDs.has(item.targetDataId)) return;
    const content = editedContents[item.targetDataId] ?? draftContent(item);
    item.targetExists = true;
    item.targetContent = content;
    item.previewContent = content;
    item.changes = [];
    item.warnings = [];
    item.status = "unchanged";
    state.drafts[draftKey(item)] = content;
    state.selectedTargets.delete(item.targetDataId);
    Object.keys(state.decisions).forEach((key) => {
      if (key.startsWith(`${item.targetDataId}|`)) delete state.decisions[key];
    });
  });
}

async function applyChange(targetDataId, path, side) {
  if (!targetDataId || !path) return;
  await run(async () => {
    const item = findItemByTarget(targetDataId);
    const current = item ? draftContent(item) : "";
    if (!wails) {
      state.drafts[targetDataId] = side === "source" ? (item.previewContent || item.sourceContent || current) : (item.targetContent || current);
    } else if (side === "source") {
      state.drafts[targetDataId] = await appCall("ApplyChange", targetDataId, path, current);
    } else {
      state.drafts[targetDataId] = await appCall("KeepTargetChange", targetDataId, path, current);
    }
    state.decisions[decisionKey({ targetDataId }, path)] = side;
    syncSelectionForItem(findItemByTarget(targetDataId));
    toast(side === "source" ? "已应用源配置" : "已保留目标配置");
  });
}

async function ignoreChange(targetDataId, path) {
  if (!targetDataId || !path) return;
  await run(async () => {
    const item = findItemByTarget(targetDataId);
    const current = item ? draftContent(item) : "";
    if (!wails) {
      state.drafts[targetDataId] = item?.targetContent || current;
    } else {
      state.drafts[targetDataId] = await appCall("KeepTargetChange", targetDataId, path, current);
      state.settings = await appCall("AddIgnoreRule", { dataId: targetDataId, path, reason: "永久忽略配置差异" });
    }
    state.decisions[decisionKey({ targetDataId }, path)] = "ignore";
    syncSelectionForItem(findItemByTarget(targetDataId));
    toast(`已永久忽略 ${path}`);
  });
}

async function ignoreListItemChange(targetDataId, path, value, side) {
  if (!targetDataId || !path || value === undefined || !side) return;
  const ignorePath = listItemRulePath(path, value);
  await run(async () => {
    if (!wails) {
      state.settings.ignoreRules ||= [];
      state.settings.ignoreRules.push({ dataId: targetDataId, path: ignorePath, reason: "永久忽略列表项差异" });
    } else {
      state.settings = await appCall("AddIgnoreRule", { dataId: targetDataId, path: ignorePath, reason: "永久忽略列表项差异" });
    }
    state.decisions[listDecisionKey({ targetDataId }, path, value, side)] = "ignore";
    syncSelectionForItem(findItemByTarget(targetDataId));
    toast(`已永久忽略 ${ignorePath}`);
  });
}

async function applyListItemChange(targetDataId, path, value, action, side) {
  if (!targetDataId || !path || value === undefined || !action || !side) return;
  await run(async () => {
    const item = findItemByTarget(targetDataId);
    const current = item ? draftContent(item) : "";
    if (!wails) {
      state.drafts[targetDataId] = current;
    } else {
      state.drafts[targetDataId] = await appCall("ApplyListItemChange", targetDataId, path, value, action, current);
    }
    state.decisions[listDecisionKey({ targetDataId }, path, value, side)] = "source";
    syncSelectionForItem(findItemByTarget(targetDataId));
    toast(action === "delete" ? "已删除目标列表项" : "已插入源列表项");
  });
}

async function keepListItemChange(targetDataId, path, value, side) {
  if (!targetDataId || !path || value === undefined || !side) return;
  await run(async () => {
    state.decisions[listDecisionKey({ targetDataId }, path, value, side)] = "target";
    syncSelectionForItem(findItemByTarget(targetDataId));
    toast(side === "target" ? "已保留目标列表项" : "已标记为不需要此项");
  });
}

async function removeIgnoreRule(dataId, path) {
  if (!dataId || !path) return;
  await run(async () => {
    if (!wails) {
      state.settings.ignoreRules = (state.settings.ignoreRules || []).filter((rule) => !(rule.dataId === dataId && rule.path === path));
    } else {
      state.settings = await appCall("RemoveIgnoreRule", { dataId, path });
    }
    toast(`已取消忽略 ${path}`);
  });
}

async function applyAllChanges(targetDataId) {
  if (!targetDataId) return;
  await run(async () => {
    const item = findItemByTarget(targetDataId);
    if (!item) return;
    const current = draftContent(item);
    state.drafts[targetDataId] = current;
    const changes = activeChanges(item);
    for (const change of changes) {
      if (wails) {
        state.drafts[targetDataId] = await appCall("ApplyChange", targetDataId, change.path, state.drafts[targetDataId]);
      }
      const keys = changeDecisionKeys({ ...item, changes: [change] });
      keys.forEach((key) => {
        state.decisions[key] = "source";
      });
    }
    if (!wails) {
      state.drafts[targetDataId] = (item.previewContent || item.sourceContent || current);
    }
    syncSelectionForItem(item);
    toast("已应用全部差异");
  });
}

function resetDraft(targetDataId) {
  const item = findItemByTarget(targetDataId);
  if (!item) return;
  state.drafts[targetDataId] = item.targetExists ? (item.targetContent || "") : "";
  (item.changes || []).forEach((change) => {
    delete state.decisions[decisionKey(item, change.path)];
    changeDecisionKeys({ ...item, changes: [change] }).forEach((key) => delete state.decisions[key]);
  });
  state.selectedTargets.delete(targetDataId);
  render();
}

async function refreshSavedEndpoints() {
  if (!wails) return;
  state.savedEndpoints = await appCall("SavedEndpoints");
}

function applySavedEndpoint(role, endpointId) {
  if (!endpointId) return;
  const endpoint = (state.savedEndpoints || []).find((item) => item.id === endpointId);
  if (!endpoint) return;
  const fallbackName = role === "source" ? "local" : "dev";
  state.form[role] = {
    name: endpoint.name || fallbackName,
    baseUrl: endpoint.baseUrl || "",
    username: endpoint.username || "nacos",
    password: endpoint.password || "",
    passwordRef: endpoint.passwordRef || "",
    namespaceId: endpoint.namespaceId || "",
    apiVersion: endpoint.apiVersion || "v3"
  };
}

function swapEndpoints() {
  const source = cloneEndpoint(state.form.source);
  const target = cloneEndpoint(state.form.target);
  state.form.source = target;
  state.form.target = source;
  [state.form.sourceEnv, state.form.targetEnv] = [state.form.targetEnv, state.form.sourceEnv];
  clearPlanState();
  toast("已交换源和目标 Nacos，请重新扫描差异");
  render();
}

function cloneEndpoint(endpoint) {
  return {
    name: endpoint?.name || "",
    baseUrl: endpoint?.baseUrl || "",
    username: endpoint?.username || "",
    password: endpoint?.password || "",
    passwordRef: endpoint?.passwordRef || "",
    namespaceId: endpoint?.namespaceId || "",
    apiVersion: endpoint?.apiVersion || "v3"
  };
}

function clearPlanState() {
  state.plan = null;
  state.drafts = {};
  state.decisions = {};
  state.selectedIndex = 0;
  state.selectedTargets.clear();
}

async function run(fn) {
  try {
    await fn();
  } catch (error) {
    toast(error?.message || String(error), "error");
  } finally {
    render();
  }
}

function endpointForSave(endpoint) {
  return {
    name: endpoint.name || "",
    baseUrl: endpoint.baseUrl || "",
    username: endpoint.username || "",
    namespaceId: endpoint.namespaceId || "",
    apiVersion: endpoint.apiVersion || "v3",
    passwordRef: endpoint.password ? `manual://${endpoint.name || endpoint.baseUrl || "endpoint"}` : ""
  };
}

function endpointIdForForm(endpoint) {
  if (!endpoint?.baseUrl) return "";
  return [
    String(endpoint.baseUrl || "").trim().toLowerCase(),
    String(endpoint.username || "").trim(),
    String(endpoint.namespaceId || "").trim(),
    String(endpoint.apiVersion || "").trim()
  ].join("|");
}

function currentItem() {
  return state.plan?.items?.[state.selectedIndex] || null;
}

function canPublish(item) {
  return item && item.targetDataId && !["error", "invalid", "skipped", "targetExtra"].includes(item.status);
}

function findItemByTarget(targetDataId) {
  return (state.plan?.items || []).find((item) => item.targetDataId === targetDataId) || null;
}

function draftKey(item) {
  return item.targetDataId || item.sourceDataId || "";
}

function initialDraft(item) {
  if (!item) return "";
  return item.targetExists ? (item.targetContent || "") : (item.previewContent || item.sourceContent || "");
}

function draftContent(item) {
  const key = draftKey(item);
  return Object.prototype.hasOwnProperty.call(state.drafts, key) ? state.drafts[key] : initialDraft(item);
}

function baselineContent(item) {
  return item?.targetExists ? (item.targetContent || "") : "";
}

function hasDraftChange(item) {
  return !!item && draftContent(item) !== baselineContent(item);
}

function initializeDrafts() {
  state.drafts = {};
  state.decisions = {};
  (state.plan?.items || []).forEach((item) => {
    const key = draftKey(item);
    if (key) state.drafts[key] = initialDraft(item);
  });
}

function publishPayload() {
  if (!state.plan) return [];
  return [...state.selectedTargets]
    .map(findItemByTarget)
    .filter((item) => canPublish(item) && hasDraftChange(item))
    .map((item) => ({ targetDataId: item.targetDataId, content: draftContent(item) }));
}

function refreshPublishState() {
  const publish = document.getElementById("publish");
  if (publish) publish.disabled = publishPayload().length === 0;
  document.querySelectorAll("[data-target-select]").forEach((el) => {
    el.checked = state.selectedTargets.has(el.dataset.targetSelect);
  });
}

function syncSelectionForItem(item) {
  if (!item?.targetDataId) return;
  if (hasDraftChange(item)) state.selectedTargets.add(item.targetDataId);
  else state.selectedTargets.delete(item.targetDataId);
}

function decisionValue(item, path) {
  return state.decisions[decisionKey(item, path)] || "";
}

function listDecisionValue(item, path, value, side) {
  return state.decisions[listDecisionKey(item, path, value, side)] || "";
}

function activeChanges(item) {
  return (item?.changes || []).filter((change) => decisionValue(item, change.path) !== "ignore");
}

function pendingChangeCount(item) {
  if (item?.status === "create" || !item?.targetExists || isTargetExtra(item)) return 0;
  return changeDecisionKeys(item).filter((key) => !state.decisions[key]).length;
}

function handledChangeCount(item) {
  if (item?.status === "create" || !item?.targetExists || isTargetExtra(item)) return 0;
  return changeDecisionKeys(item).filter((key) => !!state.decisions[key]).length;
}

function visibleWarnings(item) {
  return (item?.warnings || []).filter((warning) => decisionValue(item, warning.path) !== "ignore");
}

function listDiffsForChanges(changes) {
  const out = new Map();
  changes.forEach((change) => {
    if (Array.isArray(change.listItems) && change.listItems.length) {
      const sourceOnly = new Set(change.listItems.filter((item) => item.side === "source").map((item) => item.key));
      const targetOnly = new Set(change.listItems.filter((item) => item.side === "target").map((item) => item.key));
      out.set(change.path, {
        sourceOnly,
        targetOnly,
        hasItemDiff: sourceOnly.size > 0 || targetOnly.size > 0
      });
      return;
    }
    const sourceValues = arrayValue(change.sourceValue);
    const targetValues = arrayValue(change.targetValue);
    if (!sourceValues && !targetValues) return;
    const sourceKeys = new Set((sourceValues || []).map(valueKey));
    const targetKeys = new Set((targetValues || []).map(valueKey));
    const sourceOnly = new Set([...sourceKeys].filter((value) => !targetKeys.has(value)));
    const targetOnly = new Set([...targetKeys].filter((value) => !sourceKeys.has(value)));
    out.set(change.path, {
      sourceOnly,
      targetOnly,
      hasItemDiff: sourceOnly.size > 0 || targetOnly.size > 0
    });
  });
  return out;
}

function changeDecisionKeys(item) {
  const keys = [];
  (item?.changes || []).forEach((change) => {
    if (Array.isArray(change.listItems) && change.listItems.length) {
      change.listItems.forEach((listItem) => keys.push(listDecisionKey(item, change.path, listItem.key, listItem.side)));
      return;
    }
    const sourceValues = arrayValue(change.sourceValue);
    const targetValues = arrayValue(change.targetValue);
    if (sourceValues || targetValues) {
      const sourceKeys = new Set((sourceValues || []).map(valueKey));
      const targetKeys = new Set((targetValues || []).map(valueKey));
      const sourceOnly = [...sourceKeys].filter((value) => !targetKeys.has(value));
      const targetOnly = [...targetKeys].filter((value) => !sourceKeys.has(value));
      if (sourceOnly.length || targetOnly.length) {
        sourceOnly.forEach((value) => keys.push(listDecisionKey(item, change.path, value, "source")));
        targetOnly.forEach((value) => keys.push(listDecisionKey(item, change.path, value, "target")));
        return;
      }
    }
    keys.push(decisionKey(item, change.path));
  });
  return keys;
}

function changeSummary(change) {
  if (Array.isArray(change.listItems) && change.listItems.length) {
    const sourceOnly = change.listItems.filter((item) => item.side === "source").map((item) => item.key);
    const targetOnly = change.listItems.filter((item) => item.side === "target").map((item) => item.key);
    const bits = [];
    if (sourceOnly.length) bits.push(`源多 ${sourceOnly.length} 项：${sourceOnly.slice(0, 3).join("、")}`);
    if (targetOnly.length) bits.push(`目标多 ${targetOnly.length} 项：${targetOnly.slice(0, 3).join("、")}`);
    return ` · ${escapeHtml(bits.join("；"))}`;
  }
  const sourceValues = arrayValue(change.sourceValue);
  const targetValues = arrayValue(change.targetValue);
  if (!sourceValues && !targetValues) return "";
  const sourceKeys = new Set((sourceValues || []).map(valueKey));
  const targetKeys = new Set((targetValues || []).map(valueKey));
  const sourceOnly = [...sourceKeys].filter((value) => !targetKeys.has(value));
  const targetOnly = [...targetKeys].filter((value) => !sourceKeys.has(value));
  const bits = [];
  if (sourceOnly.length) bits.push(`源多 ${sourceOnly.length} 项：${sourceOnly.slice(0, 3).join("、")}`);
  if (targetOnly.length) bits.push(`目标多 ${targetOnly.length} 项：${targetOnly.slice(0, 3).join("、")}`);
  if (!bits.length) bits.push("列表顺序或重复项不同");
  return ` · ${escapeHtml(bits.join("；"))}`;
}

function listDiffAnchorHint(listDiff) {
  if (!listDiff) return "列表内容不同";
  const bits = [];
  if (listDiff.targetOnly.size) bits.push(`目标多 ${listDiff.targetOnly.size} 项`);
  if (listDiff.sourceOnly.size) bits.push(`源多 ${listDiff.sourceOnly.size} 项`);
  return `列表内容不同：${bits.join("，") || "顺序或重复项不同"}`;
}

function arrayValue(value) {
  return Array.isArray(value) ? value : null;
}

function valueKey(value) {
  if (value === null || value === undefined) return "";
  if (typeof value === "object") {
    const identity = objectIdentityKey(value);
    return identity || JSON.stringify(value);
  }
  return String(value);
}

function lineListValueKey(value) {
  const trimmed = String(value || "").trim();
  const quoted = trimmed.match(/^(['"])(.*)\1$/);
  const unquoted = quoted ? quoted[2] : trimmed;
  const inlineIdentity = unquoted.match(/^(id|name|key|code)\s*:\s*(.+)$/);
  if (inlineIdentity) {
    const identityValue = inlineIdentity[2].trim().replace(/^['"]|['"]$/g, "");
    if (identityValue) return `${inlineIdentity[1]}=${identityValue}`;
  }
  return valueKey(unquoted);
}

function objectIdentityKey(value) {
  for (const field of ["id", "name", "key", "code"]) {
    const raw = value?.[field];
    if (raw === null || raw === undefined) continue;
    const text = String(raw).trim();
    if (text) return `${field}=${text}`;
  }
  return "";
}

function lineNumbers(content) {
  const count = String(content || "").split("\n").length;
  return Array.from({ length: count }, (_, index) => `<span>${index + 1}</span>`).join("");
}

function updateLineGutter(textarea) {
  const gutter = document.querySelector(`[data-line-gutter="${cssEscape(textarea.dataset.draft)}"]`);
  if (!gutter) return;
  gutter.innerHTML = lineNumbers(textarea.value);
  gutter.scrollTop = textarea.scrollTop;
  syncEditorOverlay(textarea);
}

function syncEditorOverlay(textarea) {
  const overlay = document.querySelector(`[data-editor-overlay="${cssEscape(textarea.dataset.draft)}"]`);
  if (!overlay) return;
  overlay.style.transform = `translateY(${-textarea.scrollTop}px)`;
}

function captureScrollPositions() {
  const items = document.querySelector(".items");
  const content = document.querySelector(".content");
  const sidebar = document.querySelector(".sidebar");
  if (items) state.uiScroll.items = items.scrollTop;
  if (content) state.uiScroll.content = content.scrollTop;
  if (sidebar) state.uiScroll.sidebar = sidebar.scrollTop;
  document.querySelectorAll("[data-draft]").forEach((el) => {
    state.uiScroll.textareas[el.dataset.draft] = el.scrollTop;
  });
}

function restoreScrollPositions() {
  const items = document.querySelector(".items");
  const content = document.querySelector(".content");
  const sidebar = document.querySelector(".sidebar");
  if (items) items.scrollTop = state.uiScroll.items || 0;
  if (content) content.scrollTop = state.uiScroll.content || 0;
  if (sidebar) sidebar.scrollTop = state.uiScroll.sidebar || 0;
  document.querySelectorAll("[data-draft]").forEach((el) => {
    el.scrollTop = state.uiScroll.textareas[el.dataset.draft] || 0;
    const gutter = document.querySelector(`[data-line-gutter="${cssEscape(el.dataset.draft)}"]`);
    if (gutter) gutter.scrollTop = el.scrollTop;
    syncEditorOverlay(el);
  });
}

function cssEscape(value) {
  if (window.CSS?.escape) return CSS.escape(value || "");
  return String(value || "").replaceAll("\\", "\\\\").replaceAll('"', '\\"');
}

function countStatuses(items) {
  return items.reduce((acc, item) => {
    acc[item.status] = (acc[item.status] || 0) + 1;
    return acc;
  }, {});
}

function statusLabel(status) {
  return {
    changed: "修改",
    create: "新建",
    targetExtra: "目标更多",
    skipped: "跳过",
    invalid: "异常",
    error: "错误",
    unchanged: "一致"
  }[status] || status;
}

function itemBadge(item) {
  if (canPublish(item) && pendingChangeCount(item) === 0 && handledChangeCount(item) > 0) {
    return hasDraftChange(item)
      ? { className: "ready", label: "待发布" }
      : { className: "handled", label: "已处理" };
  }
  return { className: item.status, label: statusLabel(item.status) };
}

function itemSubtitle(item, pending, handled, warnings) {
  if (item.status === "create" || !item.targetExists) {
    return `目标不存在 · ${warnings} 条提醒`;
  }
  if (isTargetExtra(item)) {
    const count = targetExtraCount(item);
    return `目标更多 ${count} 项 · 无需同步 · ${warnings} 条提醒`;
  }
  return `${pending} 处待处理 · ${handled} 处已处理 · ${warnings} 条提醒`;
}

function isTargetExtra(item) {
  return item?.status === "targetExtra";
}

function targetExtraCount(item) {
  let count = 0;
  (item?.changes || []).forEach((change) => {
    if (Array.isArray(change.listItems) && change.listItems.length) {
      count += change.listItems.filter((listItem) => listItem.side === "target").length;
      return;
    }
    const sourceValues = arrayValue(change.sourceValue);
    const targetValues = arrayValue(change.targetValue);
    if (sourceValues || targetValues) {
      const sourceKeys = new Set((sourceValues || []).map(valueKey));
      const targetKeys = new Set((targetValues || []).map(valueKey));
      count += [...targetKeys].filter((value) => !sourceKeys.has(value)).length;
      return;
    }
    if (change.kind === "delete") count += 1;
  });
  return count;
}

function changeKindLabel(kind) {
  return { add: "新增", modify: "修改", delete: "删除" }[kind] || kind;
}

function sensitiveLabel(kind) {
  return {
    ip: "IP地址",
    url: "URL",
    jdbc: "JDBC地址",
    redis: "Redis地址",
    mq: "MQ地址",
    host: "域名/主机"
  }[kind] || kind;
}

function applyLabel(kind) {
  return { add: "插入配置", modify: "应用源值", delete: "删除目标项" }[kind] || "应用源配置";
}

function keepLabel(kind) {
  return { add: "不需要此配置", modify: "保留目标值", delete: "保留目标项" }[kind] || "保留目标";
}

function decisionLabel(value) {
  if (value === "source") return "已应用源配置";
  if (value === "target") return "已保留目标";
  if (value === "ignore") return "已永久忽略";
  return "";
}

function decisionKey(item, path) {
  return `${item.targetDataId || ""}|${path}`;
}

function listDecisionKey(item, path, value, side) {
  return `${item.targetDataId || ""}|${path}|list|${side}|${value}`;
}

function listItemRulePath(path, value) {
  return `${path}[${value}]`;
}

function toast(message, type = "") {
  state.toast = { message, type };
  setTimeout(() => {
    state.toast = null;
    render();
  }, 3200);
}

function getPath(obj, path) {
  return path.split(".").reduce((acc, key) => acc?.[key], obj);
}

function setPath(obj, path, value) {
  const parts = path.split(".");
  let current = obj;
  for (const part of parts.slice(0, -1)) {
    current[part] ||= {};
    current = current[part];
  }
  current[parts.at(-1)] = value;
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function escapeAttr(value) {
  return escapeHtml(value).replaceAll("'", "&#39;");
}

render();

if (wails) {
  refreshSavedEndpoints()
    .then(render)
    .catch(() => {});
}
