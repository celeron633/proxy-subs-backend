"use strict";

const $ = (selector) => document.querySelector(selector);
let toastTimer;

async function api(path, options = {}) {
  const response = await fetch(path, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  let payload = null;
  if (response.status !== 204) {
    const contentType = response.headers.get("content-type") || "";
    payload = contentType.includes("application/json") ? await response.json() : null;
  }
  if (!response.ok) {
    const error = new Error(payload?.error || `请求失败（${response.status}）`);
    error.status = response.status;
    throw error;
  }
  return payload;
}

function setBusy(button, busy, label) {
  button.disabled = busy;
  button.textContent = label;
}

function toast(message, isError = false) {
  const element = $("#toast");
  if (!element) return;
  clearTimeout(toastTimer);
  element.textContent = message;
  element.classList.toggle("error", isError);
  element.classList.add("show");
  toastTimer = setTimeout(() => element.classList.remove("show"), 2600);
}

async function copyText(value, message = "已复制") {
  try {
    await navigator.clipboard.writeText(value);
  } catch (_) {
    const input = document.createElement("textarea");
    input.value = value;
    document.body.appendChild(input);
    input.select();
    document.execCommand("copy");
    input.remove();
  }
  toast(message);
}

function escapeHTML(value) {
  return String(value).replace(/[&<>'"]/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[character]);
}

function formatBytes(bytes) {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}

function icon(name) {
  return `<svg class="icon" aria-hidden="true"><use href="/assets/icons.svg#${name}"></use></svg>`;
}

// Keep keyboard focus inside the top dialog, including the nested file picker.
const modalStack = [];
const modalTriggers = new WeakMap();
const focusableSelector = "button:not(:disabled), input:not(:disabled):not([type='hidden']), textarea:not(:disabled), a[href], [tabindex='0']";

function syncModalState() {
  const current = modalStack.at(-1);
  const shell = $(".app-shell");
  if (shell) shell.inert = Boolean(current);
  modalStack.forEach((element) => { element.inert = element !== current; });
  document.body.style.overflow = current ? "hidden" : "";
}

function showModal(element, initialFocus) {
  modalTriggers.set(element, document.activeElement);
  if (!modalStack.includes(element)) modalStack.push(element);
  element.hidden = false;
  syncModalState();
  requestAnimationFrame(() => {
    if (modalStack.at(-1) === element) element.querySelector(initialFocus || focusableSelector)?.focus();
  });
}

function hideModal(element) {
  const index = modalStack.indexOf(element);
  if (index !== -1) modalStack.splice(index, 1);
  element.hidden = true;
  element.inert = false;
  syncModalState();
  const trigger = modalTriggers.get(element);
  if (trigger?.isConnected && !trigger.closest("[inert], [hidden]")) trigger.focus();
  else (modalStack.at(-1)?.querySelector(focusableSelector) || $("#add-button"))?.focus();
}

document.addEventListener("keydown", (event) => {
  if (event.key !== "Tab" || !modalStack.length) return;
  const controls = [...modalStack.at(-1).querySelectorAll(focusableSelector)].filter((element) => element.getClientRects().length);
  const first = controls[0];
  const last = controls.at(-1);
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
});
