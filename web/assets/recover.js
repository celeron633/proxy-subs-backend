"use strict";

let recoveryMode = "request";
let recoveryCaptchaID = "";
let recoveryBusy = false;

async function loadRecoveryCaptcha() {
  recoveryCaptchaID = "";
  $("#refresh-recovery-captcha").disabled = true;
  $("#recovery-captcha").value = "";
  try {
    const captcha = await api("/admin/api/captcha");
    recoveryCaptchaID = captcha.id;
    $("#recovery-captcha-image").src = captcha.image;
  } catch (error) { $("#recovery-error").textContent = error.message; }
  finally { $("#refresh-recovery-captcha").disabled = false; }
}

function setRecoveryMode(mode) {
  recoveryMode = mode;
  const reset = mode === "reset";
  $("#request-fields").hidden = reset;
  $("#reset-fields").hidden = !reset;
  $("#recovery-captcha").disabled = reset;
  $("#reset-fields").querySelectorAll("input, textarea").forEach((input) => { input.disabled = !reset; });
  $("#recovery-submit").textContent = reset ? "重置密码" : "生成恢复文件";
  $("#recovery-mode").textContent = reset ? "重新申请" : "已有恢复码";
  $("#recovery-description").textContent = reset
    ? "如账号存在，请读取程序目录中的 recovery.txt。恢复码 10 分钟内有效，仅可使用一次；已有请求不会被覆盖。"
    : "验证后，在服务可执行文件同目录生成 recovery.txt。";
  $("#recovery-error").textContent = "";
}

$("#recovery-mode").addEventListener("click", () => {
  if (recoveryBusy) return;
  setRecoveryMode(recoveryMode === "request" ? "reset" : "request");
  if (recoveryMode === "request") loadRecoveryCaptcha();
});
$("#refresh-recovery-captcha").addEventListener("click", loadRecoveryCaptcha);
$("#recovery-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (recoveryBusy) return;
  $("#recovery-error").textContent = "";
  const reset = recoveryMode === "reset";
  if (reset && $("#recovery-password").value !== $("#recovery-confirm-password").value) { $("#recovery-error").textContent = "两次输入的密码不一致"; return; }
  if (!reset && !recoveryCaptchaID) { $("#recovery-error").textContent = "请刷新验证码后重试"; return; }
  recoveryBusy = true;
  $("#recovery-mode").disabled = true;
  setBusy($("#recovery-submit"), true, "提交中…");
  try {
    const payload = { username: $("#recovery-username").value };
    if (reset) { payload.recovery_code = $("#recovery-code").value; payload.password = $("#recovery-password").value; }
    else { payload.captcha_id = recoveryCaptchaID; payload.captcha_answer = $("#recovery-captcha").value; }
    await api(reset ? "/admin/api/recovery/reset" : "/admin/api/recovery/request", { method: "POST", body: JSON.stringify(payload) });
    if (reset) {
      $("#recovery-form").reset();
      $("#recovery-form").hidden = true;
      $("#recovery-mode").hidden = true;
      $("#recovery-description").hidden = true;
      $("#recovery-success").hidden = false;
    } else { setRecoveryMode("reset"); $("#recovery-code").focus(); }
  } catch (error) {
    $("#recovery-error").textContent = error.message;
    if (!reset) await loadRecoveryCaptcha();
  } finally {
    recoveryBusy = false;
    $("#recovery-mode").disabled = false;
    setBusy($("#recovery-submit"), false, recoveryMode === "reset" ? "重置密码" : "生成恢复文件");
  }
});
loadRecoveryCaptcha();
