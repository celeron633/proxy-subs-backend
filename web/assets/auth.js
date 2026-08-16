"use strict";

let setupMode = false;

async function initializeAuthPage() {
  try {
    const status = await api("/admin/api/status");
    if (status.authenticated) {
      window.location.replace("/dashboard");
      return;
    }
    configureAuthForm(!status.initialized);
  } catch (error) {
    $("#auth-error").textContent = error.message;
  }
}

function configureAuthForm(isFirstRun) {
  setupMode = isFirstRun;
  $("#auth-title").textContent = isFirstRun ? "初始化设置" : "登录";
  $("#auth-description").textContent = isFirstRun
    ? "首次使用，请设置管理员账号。"
    : "请输入管理员账号以继续。";
  $("#auth-submit").textContent = isFirstRun ? "完成设置" : "登录";
  $("#password").autocomplete = isFirstRun ? "new-password" : "current-password";
}

async function submitCredentials(event) {
  event.preventDefault();
  const button = $("#auth-submit");
  const idleLabel = setupMode ? "完成设置" : "登录";
  $("#auth-error").textContent = "";
  setBusy(button, true, setupMode ? "正在设置…" : "正在登录…");
  try {
    await api(setupMode ? "/admin/api/setup" : "/admin/api/login", {
      method: "POST",
      body: JSON.stringify({ username: $("#username").value, password: $("#password").value }),
    });
    window.location.replace("/dashboard");
  } catch (error) {
    $("#auth-error").textContent = error.message;
    setBusy(button, false, idleLabel);
  }
}

$("#auth-form").addEventListener("submit", submitCredentials);
initializeAuthPage();
