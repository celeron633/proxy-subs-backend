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
    $("#protection-switch").disabled = false;
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
    ? (enabled === originalProtectionState ? "已开启" : "未保存：开启保护")
    : (enabled === originalProtectionState ? "已关闭" : "未保存：关闭后将清空错误及封禁记录");
  $("#save-settings").disabled = enabled === originalProtectionState;
}

async function saveSecuritySettings() {
  const button = $("#save-settings");
  const enabled = $("#protection-switch").checked;
  $("#protection-switch").disabled = true;
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
    $("#protection-switch").disabled = false;
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
