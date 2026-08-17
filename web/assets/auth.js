"use strict";

let setupMode = false;
let captchaID = "";

async function initializeAuthPage() {
  try {
    const status = await api("/admin/api/status");
    if (status.authenticated) {
      window.location.replace("/dashboard");
      return;
    }
    configureAuthForm(!status.initialized);
    if (status.initialized) await loadCaptcha();
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
  $("#captcha-field").hidden = isFirstRun;
  $("#captcha-answer").disabled = isFirstRun;
  $("#captcha-answer").required = !isFirstRun;
}

async function loadCaptcha() {
  const refreshButton = $("#refresh-captcha");
  refreshButton.disabled = true;
  captchaID = "";
  $("#captcha-answer").value = "";
  try {
    const result = await api("/admin/api/captcha");
    captchaID = result.id;
    $("#captcha-image").src = result.image;
  } catch (error) {
    $("#auth-error").textContent = error.message;
  } finally {
    refreshButton.disabled = false;
  }
}

async function submitCredentials(event) {
  event.preventDefault();
  const button = $("#auth-submit");
  const idleLabel = setupMode ? "完成设置" : "登录";
  $("#auth-error").textContent = "";
  setBusy(button, true, setupMode ? "正在设置…" : "正在登录…");
  try {
    if (!setupMode && !captchaID) throw new Error("验证码尚未加载，请刷新后重试");
    await api(setupMode ? "/admin/api/setup" : "/admin/api/login", {
      method: "POST",
      body: JSON.stringify({
        username: $("#username").value,
        password: $("#password").value,
        captcha_id: captchaID,
        captcha_answer: $("#captcha-answer").value,
      }),
    });
    window.location.replace("/dashboard");
  } catch (error) {
    $("#auth-error").textContent = error.message;
    setBusy(button, false, idleLabel);
    if (!setupMode) await loadCaptcha();
  }
}

$("#auth-form").addEventListener("submit", submitCredentials);
$("#refresh-captcha").addEventListener("click", loadCaptcha);
initializeAuthPage();
