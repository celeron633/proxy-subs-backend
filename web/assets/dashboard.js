"use strict";

const state = {
  dashboard: null,
  query: "",
};

const editorBackdrop = $("#editor-backdrop");
const fileBrowserBackdrop = $("#file-browser-backdrop");
const tokenBackdrop = $("#token-backdrop");
const editorForm = $("#editor-form");
let fileBrowserCurrentPath = "";
let fileBrowserParentPath = null;
let fileBrowserLoadSequence = 0;

async function loadDashboard() {
  try {
    state.dashboard = await api("/admin/api/dashboard");
    renderDashboard();
  } catch (error) {
    if (error.status === 401) {
      window.location.replace("/");
      return;
    }
    toast(error.message, true);
  }
}

function renderDashboard() {
  const { username, api_enabled: apiEnabled, subscriptions } = state.dashboard;
  $("#account-name").textContent = username;
  $("#api-switch").checked = apiEnabled;
  $("#service-dot").classList.toggle("off", !apiEnabled);
  $("#service-copy").textContent = apiEnabled ? "所有已启用的订阅入口均可访问" : "公开订阅入口已全部暂停";
  $("#total-count").textContent = subscriptions.length;
  $("#ready-count").textContent = subscriptions.filter((item) => item.file_status === "ready").length;
  renderSubscriptions();
}

function renderSubscriptions() {
  const subscriptions = state.dashboard?.subscriptions || [];
  const query = state.query.trim().toLocaleLowerCase();
  const filtered = subscriptions.filter((item) =>
    [item.name, item.url_path, item.file_path, item.note].some((value) => (value || "").toLocaleLowerCase().includes(query))
  );
  $("#subscription-list").replaceChildren(...filtered.map(subscriptionRow));
  $("#empty-state").hidden = subscriptions.length !== 0;
  $("#no-results").hidden = subscriptions.length === 0 || filtered.length !== 0;
}

function subscriptionRow(item) {
  const row = document.createElement("article");
  row.className = "subscription-row";
  const url = `${location.origin}/api/${encodeURIComponent(item.url_path)}`;
  const fileBadge = item.file_status === "ready"
    ? `<span class="badge">文件正常</span>`
    : `<span class="badge missing">文件缺失</span>`;
  const serviceBadge = item.enabled
    ? `<span class="badge">已启用</span>`
    : `<span class="badge off">已停用</span>`;
  row.innerHTML = `
    <div class="subscription-main">
      <h3>${escapeHTML(item.name)}</h3>
      <p title="${escapeHTML(item.note || "无备注")}">${escapeHTML(item.note || "无备注")}</p>
      <div class="row-meta">${serviceBadge}<span>${escapeHTML(item.token_hint)}</span></div>
    </div>
    <div class="url-cell">
      <div class="url-code"><code title="${escapeHTML(url)}">${escapeHTML(url)}</code><button class="copy-mini" type="button" data-copy-url="${escapeHTML(url)}">复制地址</button></div>
      <div class="row-meta"><span>使用 ?token=你的token 或 Bearer token</span></div>
    </div>
    <div class="file-cell">
      <p title="${escapeHTML(item.file_path)}">${escapeHTML(item.file_path)}</p>
      <div class="row-meta">${fileBadge}<span>${item.file_status === "ready" ? formatBytes(item.file_size) : "请检查路径"}</span></div>
    </div>
    <div class="row-actions">
      <button class="button button-secondary" type="button" data-edit="${item.id}">编辑</button>
      <button class="button button-quiet button-danger" type="button" data-delete="${item.id}">删除</button>
    </div>`;
  return row;
}

function openEditor(item = null) {
  editorForm.reset();
  fileBrowserBackdrop.hidden = true;
  $("#subscription-id").value = item?.id || "";
  $("#subscription-name").value = item?.name || "";
  $("#subscription-url").value = item?.url_path || "";
  $("#subscription-file").value = item?.file_path || "";
  $("#subscription-token").value = "";
  $("#subscription-note").value = item?.note || "";
  $("#subscription-enabled").checked = item?.enabled ?? true;
  $("#editor-title").textContent = item ? "编辑订阅" : "新建订阅";
  $("#token-help").textContent = item ? `当前 token ${item.token_hint}，留空表示不修改` : "留空将自动生成；创建后仅展示一次";
  $("#editor-error").textContent = "";
  $("#subscription-token").type = "password";
  $("#toggle-token").textContent = "显示";
  editorBackdrop.hidden = false;
  document.body.style.overflow = "hidden";
  setTimeout(() => $("#subscription-name").focus(), 30);
}

function closeEditor() {
  fileBrowserLoadSequence += 1;
  fileBrowserBackdrop.hidden = true;
  editorBackdrop.hidden = true;
  document.body.style.overflow = "";
}

function openFileBrowser() {
  fileBrowserBackdrop.hidden = false;
  document.body.style.overflow = "hidden";
  loadFileDirectory(fileBrowserCurrentPath);
}

function closeFileBrowser() {
  fileBrowserLoadSequence += 1;
  fileBrowserBackdrop.hidden = true;
  document.body.style.overflow = editorBackdrop.hidden ? "" : "hidden";
  $("#subscription-file").focus();
}

async function loadFileDirectory(relativePath) {
  const loadSequence = ++fileBrowserLoadSequence;
  const list = $("#file-browser-list");
  $("#file-browser-error").textContent = "";
  $("#file-browser-empty").hidden = true;
  $("#file-browser-current").textContent = "正在读取…";
  $("#file-browser-up").disabled = true;
  list.setAttribute("aria-busy", "true");
  list.replaceChildren();

  try {
    const query = new URLSearchParams({ path: relativePath });
    const result = await api(`/admin/api/files?${query}`);
    if (loadSequence !== fileBrowserLoadSequence) return;

    fileBrowserCurrentPath = result.relative_path;
    fileBrowserParentPath = result.relative_path ? result.parent_path : null;
    $("#file-browser-root").textContent = result.root;
    $("#file-browser-current").textContent = result.current_path;
    $("#file-browser-up").disabled = fileBrowserParentPath === null;
    list.replaceChildren(...result.entries.map(fileBrowserEntry));
    $("#file-browser-empty").hidden = result.entries.length !== 0;
  } catch (error) {
    if (loadSequence !== fileBrowserLoadSequence) return;
    if (error.status === 401) {
      window.location.replace("/");
      return;
    }
    $("#file-browser-current").textContent = "读取失败";
    $("#file-browser-error").textContent = error.message;
  } finally {
    if (loadSequence === fileBrowserLoadSequence) list.removeAttribute("aria-busy");
  }
}

function fileBrowserEntry(entry) {
  const button = document.createElement("button");
  button.className = "file-browser-entry";
  button.type = "button";
  button.dataset.fileBrowserPath = entry.relative_path;
  button.dataset.absolutePath = entry.path;
  button.dataset.directory = String(entry.is_directory);

  const kind = document.createElement("span");
  kind.className = `file-browser-kind${entry.is_directory ? " directory" : ""}`;
  kind.textContent = entry.is_directory ? "目录" : "文件";

  const copy = document.createElement("span");
  copy.className = "file-browser-copy";
  const name = document.createElement("strong");
  name.textContent = entry.name;
  const detail = document.createElement("small");
  detail.textContent = entry.is_directory ? "打开目录" : formatBytes(entry.size);
  copy.append(name, detail);

  const action = document.createElement("span");
  action.className = "file-browser-action";
  action.textContent = entry.is_directory ? "进入 →" : "选择";
  button.append(kind, copy, action);
  return button;
}

function showCreatedToken(item, token) {
  $("#created-url").value = `${location.origin}/api/${encodeURIComponent(item.url_path)}?token=${encodeURIComponent(token)}`;
  tokenBackdrop.hidden = false;
  document.body.style.overflow = "hidden";
}

function closeToken() {
  tokenBackdrop.hidden = true;
  document.body.style.overflow = "";
}

async function saveSubscription(event) {
  event.preventDefault();
  const id = $("#subscription-id").value;
  const payload = {
    name: $("#subscription-name").value,
    url_path: $("#subscription-url").value,
    file_path: $("#subscription-file").value,
    token: $("#subscription-token").value,
    note: $("#subscription-note").value,
    enabled: $("#subscription-enabled").checked,
  };
  setBusy($("#save-button"), true, "保存中…");
  $("#editor-error").textContent = "";
  try {
    const result = await api(id ? `/admin/api/subscriptions/${id}` : "/admin/api/subscriptions", {
      method: id ? "PUT" : "POST",
      body: JSON.stringify(payload),
    });
    closeEditor();
    await loadDashboard();
    if (result.token) showCreatedToken(result.subscription, result.token);
    else toast("订阅已保存");
  } catch (error) {
    if (error.status === 401) {
      window.location.replace("/");
      return;
    }
    $("#editor-error").textContent = error.message;
  } finally {
    setBusy($("#save-button"), false, "保存订阅");
  }
}

async function removeSubscription(id) {
  const item = state.dashboard.subscriptions.find((candidate) => candidate.id === id);
  if (!item || !confirm(`确定删除“${item.name}”吗？公开地址会立即失效。`)) return;
  try {
    await api(`/admin/api/subscriptions/${id}`, { method: "DELETE" });
    await loadDashboard();
    toast("订阅已删除");
  } catch (error) {
    toast(error.message, true);
  }
}

async function toggleAPI(event) {
  const enabled = event.target.checked;
  event.target.disabled = true;
  try {
    await api("/admin/api/switch", { method: "PUT", body: JSON.stringify({ enabled }) });
    state.dashboard.api_enabled = enabled;
    renderDashboard();
    toast(enabled ? "订阅服务已开启" : "订阅服务已暂停");
  } catch (error) {
    event.target.checked = !enabled;
    toast(error.message, true);
  } finally {
    event.target.disabled = false;
  }
}

async function logout() {
  try {
    await api("/admin/api/logout", { method: "POST", body: "{}" });
  } finally {
    window.location.replace("/");
  }
}

$("#logout-button").addEventListener("click", logout);
$("#add-button").addEventListener("click", () => openEditor());
$("#open-file-browser").addEventListener("click", openFileBrowser);
$("#file-browser-up").addEventListener("click", () => {
  if (fileBrowserParentPath !== null) loadFileDirectory(fileBrowserParentPath);
});
$("#api-switch").addEventListener("change", toggleAPI);
$("#search-input").addEventListener("input", (event) => { state.query = event.target.value; renderSubscriptions(); });
$("#editor-form").addEventListener("submit", saveSubscription);
$("#toggle-token").addEventListener("click", () => {
  const input = $("#subscription-token");
  input.type = input.type === "password" ? "text" : "password";
  $("#toggle-token").textContent = input.type === "password" ? "显示" : "隐藏";
});
$("#copy-created-url").addEventListener("click", () => copyText($("#created-url").value, "完整订阅地址已复制"));

document.addEventListener("click", (event) => {
  const add = event.target.closest("[data-action='add']");
  const closeEditorButton = event.target.closest("[data-action='close-editor']");
  const closeFileBrowserButton = event.target.closest("[data-action='close-file-browser']");
  const closeTokenButton = event.target.closest("[data-action='close-token']");
  const fileEntry = event.target.closest("[data-file-browser-path]");
  const edit = event.target.closest("[data-edit]");
  const remove = event.target.closest("[data-delete]");
  const copy = event.target.closest("[data-copy-url]");
  if (add) openEditor();
  if (closeEditorButton) closeEditor();
  if (closeFileBrowserButton) closeFileBrowser();
  if (closeTokenButton) closeToken();
  if (fileEntry) {
    if (fileEntry.dataset.directory === "true") loadFileDirectory(fileEntry.dataset.fileBrowserPath);
    else {
      $("#subscription-file").value = fileEntry.dataset.absolutePath;
      closeFileBrowser();
    }
  }
  if (edit) openEditor(state.dashboard.subscriptions.find((item) => item.id === Number(edit.dataset.edit)));
  if (remove) removeSubscription(Number(remove.dataset.delete));
  if (copy) copyText(`${copy.dataset.copyUrl}?token=YOUR_TOKEN`, "地址模板已复制");
});

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  if (!fileBrowserBackdrop.hidden) closeFileBrowser();
  else if (!tokenBackdrop.hidden) closeToken();
  else if (!editorBackdrop.hidden) closeEditor();
});

editorBackdrop.addEventListener("click", (event) => { if (event.target === editorBackdrop) closeEditor(); });
fileBrowserBackdrop.addEventListener("click", (event) => { if (event.target === fileBrowserBackdrop) closeFileBrowser(); });
tokenBackdrop.addEventListener("click", (event) => { if (event.target === tokenBackdrop) closeToken(); });

loadDashboard();
