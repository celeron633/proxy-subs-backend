"use strict";

let originalProtectionState = true;

async function loadSecuritySettings() {
  try {
    const [dashboard, settings] = await Promise.all([
      api("/admin/api/dashboard"),
      api("/admin/api/settings/security"),
    ]);
    $("#account-name").textContent = dashboard.username;
    originalProtectionState = settings.enabled;
    $("#protection-switch").checked = settings.enabled;
    updateSettingState();
  } catch (error) {
    if (error.status === 401) {
      window.location.replace("/");
      return;
    }
    toast(error.message, true);
  }
}

function updateSettingState() {
  const enabled = $("#protection-switch").checked;
  $("#setting-state").textContent = enabled
    ? "保护已开启，错误次数会按 IP 记录。"
    : "保护将关闭，已有的错误和封禁记录会被清空。";
  $("#save-settings").disabled = enabled === originalProtectionState;
}

async function saveSecuritySettings() {
  const button = $("#save-settings");
  const enabled = $("#protection-switch").checked;
  setBusy(button, true, "保存中…");
  try {
    await api("/admin/api/settings/security", {
      method: "PUT",
      body: JSON.stringify({ enabled }),
    });
    originalProtectionState = enabled;
    updateSettingState();
    toast(enabled ? "错误访问保护已开启" : "错误访问保护已关闭");
  } catch (error) {
    if (error.status === 401) {
      window.location.replace("/");
      return;
    }
    toast(error.message, true);
  } finally {
    button.textContent = "保存设置";
    button.disabled = $("#protection-switch").checked === originalProtectionState;
  }
}

async function logout() {
  try {
    await api("/admin/api/logout", { method: "POST", body: "{}" });
  } finally {
    window.location.replace("/");
  }
}

$("#protection-switch").addEventListener("change", updateSettingState);
$("#save-settings").addEventListener("click", saveSecuritySettings);
$("#logout-button").addEventListener("click", logout);
loadSecuritySettings();
