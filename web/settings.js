const API = "/v8/management";
const CONFIG = "/config/plugins/configs/key-chat-access";
const SCOPE_PREFIX = "cli-proxy-api:caller-scope:v1\0";
const encoder = new TextEncoder();

async function sha256(text) {
  const digest = await globalThis.crypto.subtle.digest("SHA-256", encoder.encode(text));
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, "0")).join("");
}

export function callerScope(key) { return sha256(SCOPE_PREFIX + key.trim()); }

export function readStored(storage, name, host, userAgent) {
  try {
    let raw = storage.getItem(name);
    if (raw === null) return null;
    if (raw.startsWith("enc::v1::")) {
      const bytes = Uint8Array.from(atob(raw.slice(9)), char => char.charCodeAt(0));
      const mask = encoder.encode("cli-proxy-api-webui::secure-storage|" + host + "|" + userAgent);
      raw = new TextDecoder().decode(bytes.map((byte, index) => byte ^ mask[index % mask.length]));
    }
    try { return JSON.parse(raw); } catch { return raw; }
  } catch { return null; }
}

export function readSession(storage, location, userAgent) {
  const read = name => readStored(storage, name, location.host, userAgent);
  if (read("isLoggedIn") !== true) return {key: "", apiBase: location.origin};
  const state = read("cli-proxy-auth")?.state || {};
  const apiBase = state.apiBase || read("apiBase") || location.origin;
  try {
    // A saved session for a different server must never authorize this page.
    if (new URL(apiBase).origin !== location.origin) return {key: "", apiBase: location.origin};
  } catch { return {key: "", apiBase: location.origin}; }
  const key = state.managementKey || read("managementKey");
  return {key: typeof key === "string" ? key.trim() : "", apiBase};
}

function syncImageMenuLanguage() {
  const labels = {"btn-save": "Save image", "btn-view": "View full size", "btn-copy": "Copy image", "btn-cancel": "Cancel"};
  const documents = new Set([document]);
  try { documents.add(window.parent.document); } catch { /* Same-origin panel only. */ }
  const observers = [];
  for (const doc of documents) {
    function update() {
      const menu = doc.getElementById("custom-menu");
      if (!menu) return;
      for (const [id, label] of Object.entries(labels)) {
        const button = menu.querySelector("button#" + id);
        if (button && button.textContent !== label) button.textContent = label;
      }
    }
    update();
    // The reported menu may be appended or rebuilt after the page loads.
    // Keep the existing buttons and their event handlers intact.
    const observer = new MutationObserver(update);
    observer.observe(doc.documentElement, {childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ["id"]});
    observers.push(observer);
  }
  window.addEventListener("pagehide", event => { if (!event.persisted) observers.forEach(observer => observer.disconnect()); });
}

function syncTheme() {
  const root = document.documentElement;
  const system = window.matchMedia("(prefers-color-scheme: dark)");
  const palette = ["--bg-primary", "--bg-secondary", "--text-primary", "--text-secondary", "--border-color", "--primary-color", "--primary-contrast"];
  let parentRoot;
  try { if (window.parent !== window) parentRoot = window.parent.document.documentElement; } catch { /* Standalone fallback. */ }
  function update() {
    if (parentRoot) {
      root.dataset.theme = parentRoot.getAttribute("data-theme") || "light";
      const colors = window.parent.getComputedStyle(parentRoot);
      for (const name of palette) {
        const value = colors.getPropertyValue(name).trim();
        if (value) root.style.setProperty(name, value);
      }
      return;
    }
    let theme;
    try { theme = readStored(localStorage, "cli-proxy-theme", location.host, navigator.userAgent)?.state?.theme; } catch { /* Use system preference. */ }
    root.dataset.theme = ["dark", "white", "light"].includes(theme) ? theme : system.matches ? "dark" : "white";
  }
  update();
  if (parentRoot) new MutationObserver(update).observe(parentRoot, {attributes: true, attributeFilter: ["data-theme", "class", "style"]});
  window.addEventListener("storage", event => { if (event.key === "cli-proxy-theme" || event.key === null) update(); });
  system.addEventListener("change", update);
}

export function normalizeScopes(scopes) {
  if (!Array.isArray(scopes) || scopes.some(s => typeof s !== "string" || !/^[a-fA-F0-9]{64}$/.test(s.trim()))) {
    throw new Error("Existing rules are invalid. Correct the plugin configuration before saving.");
  }
  return [...new Set(scopes.map(s => s.trim().toLowerCase()))].sort();
}

export async function makeUsers(keys, blocked, apiBase, names = {}) {
  if (!Array.isArray(keys) || keys.some(key => typeof key !== "string" || !key.trim())) {
    throw new Error("The management API returned an invalid API key list.");
  }
  const rows = await Promise.all([...new Set(keys.map(key => key.trim()))].map(async (key, index) => {
    const [scope, nameID] = await Promise.all([callerScope(key), sha256(JSON.stringify(["api-key-name", apiBase, key]))]);
    const note = names[nameID];
    const embeddedName = key.includes("@") ? key.slice(0, key.indexOf("@")) : "";
    const label = typeof note === "string" && note.trim() ? note.trim() :
      embeddedName && embeddedName.length <= 80 ? embeddedName : "User " + (index + 1);
    // No raw Key is retained in the view model or written into DOM/storage.
    const prefixLength = Math.min(15, key.length - 12 - 8);
    const masked = key.length > 24 ? key.slice(0, prefixLength) + "…" + key.slice(-12) : "•••• (Key " + (index + 1) + ")";
    return {scope, label, masked, blocked: blocked.includes(scope), unknown: false};
  }));
  const known = new Set(rows.map(row => row.scope));
  for (const scope of blocked) {
    if (!known.has(scope)) rows.push({scope, label: "Existing rule (key no longer in the current list)", masked: scope.slice(0, 8) + "…", blocked: true, unknown: true});
  }
  return rows;
}

export async function boot() {
  syncImageMenuLanguage();
  syncTheme();
  const $ = id => document.getElementById(id);
  document.querySelector(".back").addEventListener("click", event => {
    try {
      if (window.top !== window) {
        const plugins = window.top.document.querySelector('a[href="#/plugins"]');
        if (plugins) { event.preventDefault(); plugins.click(); }
      }
    } catch { /* The normal link remains available outside the same-origin panel. */ }
  });
  let session;
  try { session = readSession(localStorage, location, navigator.userAgent); }
  catch { session = {key: "", apiBase: location.origin}; }
  let managementKey = session.key;
  let users = [], baseline = "", busy = false, dirty = false, globalEnabled = true;

  function message(text, type = "") { $("message").textContent = text; $("message").className = type; }
  function setBusy(value) {
    busy = value;
    for (const id of ["save", "refresh", "search"]) $(id).disabled = value;
    document.querySelectorAll("#users input").forEach(input => { input.disabled = value; });
  }
  function changed() {
    dirty = true;
    $("count").textContent = "Selected: " + users.filter(user => user.blocked).length + " / " + users.length + " entries";
    message("You have unsaved changes. Click Save settings to apply them.");
  }
  function render() {
    const search = $("search").value.trim().toLowerCase();
    const fragment = document.createDocumentFragment();
    let count = 0;
    for (const user of users) {
      if (!(user.label + " " + user.masked).toLowerCase().includes(search)) continue;
      count++;
      const row = document.createElement("label");
      row.className = "user" + (user.unknown ? " unknown" : "");
      const text = document.createElement("span"), label = document.createElement("span"), detail = document.createElement("span");
      label.className = "user-name"; label.textContent = user.label;
      detail.className = "user-detail"; detail.textContent = user.masked;
      text.append(label, detail);
      const checkbox = document.createElement("input");
      checkbox.type = "checkbox"; checkbox.checked = user.blocked; checkbox.disabled = busy;
      checkbox.setAttribute("aria-label", "Block " + user.label + " from Chat Completions");
      checkbox.addEventListener("change", () => { user.blocked = checkbox.checked; changed(); });
      row.append(text, checkbox); fragment.append(row);
    }
    $("users").replaceChildren(fragment);
    $("empty").hidden = count !== 0;
    $("count").textContent = "Selected: " + users.filter(user => user.blocked).length + " / " + users.length + " entries";
  }
  async function api(path, body, method = "GET") {
    const response = await fetch(API + path, {
      method, credentials: "omit", cache: "no-store", redirect: "error",
      headers: {Authorization: "Bearer " + managementKey, ...(body !== undefined ? {"Content-Type": "application/json"} : {})},
      ...(body !== undefined ? {body: JSON.stringify(body)} : {}),
    });
    if (response.status === 401 || response.status === 403) {
      managementKey = ""; $("login").hidden = false; $("editor").hidden = true;
      throw new Error("The management password is invalid or expired. Please reconnect.");
    }
    if (!response.ok) throw new Error("Management request failed (HTTP " + response.status + "). Reload the settings and try again.");
    return response.json();
  }
  function signature(config) { return JSON.stringify([config.enabled !== false, normalizeScopes(config.blocked_caller_scopes || [])]); }
  async function load() {
    setBusy(true); message("Loading users and existing rules…");
    try {
      if (!globalThis.crypto?.subtle) throw new Error("Open this page using HTTPS.");
      const [keys, config, enabled] = await Promise.all([api("/config/access/api-keys"), api(CONFIG), api("/config/plugins/enabled")]);
      let names = {};
      try { names = readStored(localStorage, "api-key-names:v1:" + session.apiBase, location.host, navigator.userAgent) || {}; } catch { /* Optional local labels. */ }
      users = await makeUsers(keys, normalizeScopes(config.blocked_caller_scopes || []), session.apiBase, names);
      baseline = signature(config); globalEnabled = enabled === true;
      $("login").hidden = true; $("editor").hidden = false; dirty = false; render();
      message(globalEnabled ? "Settings loaded. Check users to block access; uncheck them to restore access." : "Plugins are globally disabled. Enable them in the management panel first.", globalEnabled ? "" : "warning");
    } catch (error) { message(error.message, "error"); }
    finally { setBusy(false); }
  }
  async function save() {
    setBusy(true); message("Saving…");
    try {
      const current = await api(CONFIG);
      if (signature(current) !== baseline) throw new Error("Settings were changed in another page. Click Reload, then make your selection again.");
      const config = {blocked_caller_scopes: users.filter(user => user.blocked).map(user => user.scope).sort()};
      // Patch only policy fields; preserve priority, installation metadata and
      // all unrelated host settings and client keys.
      await api(CONFIG, config, "PATCH");
      const persisted = await api(CONFIG);
      if (signature(persisted) !== signature({...current, ...config})) throw new Error("The saved settings differ from your selection. Reload to verify them.");
      baseline = signature(persisted); dirty = false;
      if (!globalEnabled || persisted.enabled === false) message("Saved. The plugin is disabled; enable it in the management panel to apply restrictions.", "warning");
      else message("Saved. Chat Completions blocked for " + config.blocked_caller_scopes.length + " entries.", "success");
    } catch (error) { message(error.message, "error"); }
    finally { setBusy(false); }
  }
  $("login").addEventListener("submit", async event => {
    event.preventDefault(); managementKey = $("password").value.trim(); $("password").value = "";
    $("login").querySelector("button").disabled = true;
    try { await load(); } finally { $("login").querySelector("button").disabled = false; }
  });
  $("search").addEventListener("input", render);
  $("save").addEventListener("click", save);
  $("refresh").addEventListener("click", load);
  window.addEventListener("beforeunload", event => { if (dirty) { event.preventDefault(); event.returnValue = ""; } });
  if (managementKey) await load();
  else { $("login").hidden = false; message("Connect to load the existing user list."); }
}

if (typeof document !== "undefined") boot();
