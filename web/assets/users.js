"use strict";

let users = [];
let currentUsername = "";
const userBackdrop = $("#user-backdrop");

async function loadUsers() {
  $("#users-loading").hidden = false;
  $("#users-error").hidden = true;
  $("#add-button").disabled = true;
  try {
    const result = await api("/admin/api/users");
    users = result.users;
    currentUsername = result.username;
    $("#account-name").textContent = currentUsername;
    $("#users-list").replaceChildren(...users.map((user) => {
      const row = document.createElement("article");
      row.className = "user-row";
      const current = user.username === currentUsername;
      row.innerHTML = `<div><div class="user-name"><h2>${escapeHTML(user.username)}</h2>${current ? '<span class="badge">当前账号</span>' : ''}</div><p class="user-meta">创建于 ${escapeHTML(new Date(user.created_at).toLocaleDateString())}</p></div><div class="user-actions"><button type="button" class="button button-quiet" data-password="${user.id}">重置密码</button><button type="button" class="button button-quiet button-danger" data-delete-user="${user.id}" ${current || users.length <= 1 ? 'disabled' : ''}>删除</button></div>`;
      return row;
    }));
    $("#add-button").disabled = false;
  } catch (error) {
    if (error.status === 401) { location.replace("/"); return; }
    $("#users-error-message").textContent = error.message;
    $("#users-error").hidden = false;
  } finally {
    $("#users-loading").hidden = true;
  }
}

function openUserEditor(user = null) {
  $("#user-form").reset();
  $("#user-id").value = user?.id || "";
  $("#user-name").value = user?.username || "";
  $("#user-name").readOnly = Boolean(user);
  $("#user-title").textContent = user ? "重置密码" : "新增用户";
  $("#user-password-note").hidden = !user;
  $("#user-error").textContent = "";
  showModal(userBackdrop, user ? "#user-password" : "#user-name");
}

function closeUserEditor() {
  if ($("#save-user").disabled) return;
  hideModal(userBackdrop);
  $("#user-form").reset();
}

$("#user-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  $("#user-error").textContent = "";
  if ($("#user-password").value !== $("#user-confirm-password").value) { $("#user-error").textContent = "两次输入的密码不一致"; return; }
  const id = $("#user-id").value;
  const username = $("#user-name").value;
  setBusy($("#save-user"), true, "保存中…");
  try {
    await api(id ? `/admin/api/users/${id}/password` : "/admin/api/users", { method: id ? "PUT" : "POST", body: JSON.stringify({ username, password: $("#user-password").value }) });
    hideModal(userBackdrop);
    $("#user-form").reset();
    if (id && username === currentUsername) { location.replace("/"); return; }
    toast(id ? "密码已重置，原会话已失效" : "用户已创建");
    await loadUsers();
  } catch (error) {
    if (error.status === 401) { location.replace("/"); return; }
    $("#user-error").textContent = error.message;
  } finally { setBusy($("#save-user"), false, "保存"); }
});

$("#users-list").addEventListener("click", async (event) => {
  const edit = event.target.closest("[data-password]");
  if (edit) openUserEditor(users.find((user) => user.id === Number(edit.dataset.password)));
  const remove = event.target.closest("[data-delete-user]");
  if (!remove || remove.disabled) return;
  const user = users.find((item) => item.id === Number(remove.dataset.deleteUser));
  if (!user || !confirm(`删除用户“${user.username}”？该账号的登录会话将失效。`)) return;
  remove.disabled = true;
  try {
    await api(`/admin/api/users/${user.id}`, { method: "DELETE" });
    await loadUsers();
    toast("用户已删除");
  } catch (error) {
    if (error.status === 401) { location.replace("/"); return; }
    remove.disabled = false;
    toast(error.message, true);
  }
});
$("#add-button").addEventListener("click", () => openUserEditor());
$("#retry-users").addEventListener("click", loadUsers);
$("#logout-button").addEventListener("click", async () => {
  try { await api("/admin/api/logout", { method: "POST", body: "{}" }); } finally { location.replace("/"); }
});
document.querySelectorAll("[data-close-user]").forEach((button) => button.addEventListener("click", closeUserEditor));
userBackdrop.addEventListener("click", (event) => { if (event.target === userBackdrop) closeUserEditor(); });
document.addEventListener("keydown", (event) => { if (event.key === "Escape" && !userBackdrop.hidden) closeUserEditor(); });
loadUsers();
