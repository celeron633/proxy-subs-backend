"use strict";

const state = {
  dashboard: null,
  query: "",
};

const editorBackdrop = $("#editor-backdrop");
const tokenBackdrop = $("#token-backdrop");
const editorForm = $("#editor-form");

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
  editorBackdrop.hidden = true;
  document.body.style.overflow = "";
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
  const closeTokenButton = event.target.closest("[data-action='close-token']");
  const edit = event.target.closest("[data-edit]");
  const remove = event.target.closest("[data-delete]");
  const copy = event.target.closest("[data-copy-url]");
  if (add) openEditor();
  if (closeEditorButton) closeEditor();
  if (closeTokenButton) closeToken();
  if (edit) openEditor(state.dashboard.subscriptions.find((item) => item.id === Number(edit.dataset.edit)));
  if (remove) removeSubscription(Number(remove.dataset.delete));
  if (copy) copyText(`${copy.dataset.copyUrl}?token=YOUR_TOKEN`, "地址模板已复制");
});

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  if (!tokenBackdrop.hidden) closeToken();
  else if (!editorBackdrop.hidden) closeEditor();
});

editorBackdrop.addEventListener("click", (event) => { if (event.target === editorBackdrop) closeEditor(); });
tokenBackdrop.addEventListener("click", (event) => { if (event.target === tokenBackdrop) closeToken(); });

loadDashboard();
