// Called by integration_test.py with local fixture credentials only.
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {pathToFileURL} from "node:url";
const [base, modulePath] = process.argv.slice(2);
const {chromium} = await import(modulePath ? pathToFileURL(modulePath).href : "playwright");
const browser = await chromium.launch({headless: true});
const keyA = "local-test-blocked-client", keyB = "local-test-allowed-client";
const secret = "local-test-management";
const hash = text => createHash("sha256").update(text).digest("hex");
const pageURL = base + "/v0/resource/plugins/key-chat-access/settings";
const configURL = base + "/v8/management/config/plugins/configs/key-chat-access";
try {
  const page = await browser.newPage();
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.addInitScript(({base, keyA, keyB}) => {
    function encode(value) {
      const bytes = new TextEncoder().encode(JSON.stringify(value));
      const mask = new TextEncoder().encode("cli-proxy-api-webui::secure-storage|" + location.host + "|" + navigator.userAgent);
      return "enc::v1::" + btoa(String.fromCharCode(...bytes.map((b, i) => b ^ mask[i % mask.length])));
    }
    localStorage.setItem("api-key-names:v1:" + base, encode({[keyA]: "Shayan", [keyB]: "Alice <script>bad()</script>"}));
  }, {base, keyA: hash(JSON.stringify(["api-key-name", base, keyA])), keyB: hash(JSON.stringify(["api-key-name", base, keyB]))});
  await page.goto(pageURL);
  await page.locator("#login").waitFor({state: "visible"});
  await page.locator("#password").fill(secret);
  await page.locator("#login button").click();
  await page.locator("#editor").waitFor({state: "visible"});
  const blocked = page.getByRole("checkbox", {name: "Block Shayan from Chat Completions", exact: true});
  const allowed = page.getByRole("checkbox", {name: "Block Alice <script>bad()</script> from Chat Completions", exact: true});
  assert.equal(await blocked.isChecked(), true);
  assert.equal(await allowed.isChecked(), false);
  assert.equal(await page.locator("#password").inputValue(), "");
  assert.ok(!(await page.content()).includes(keyA));
  assert.ok(!(await page.content()).includes(keyB));
  assert.ok(!(await page.content()).includes(secret));
  const writes = [];
  page.on("request", request => { if (request.method() === "PATCH") writes.push(JSON.parse(request.postData())); });
  await allowed.check();
  await page.locator("#search").fill("Alice");
  assert.equal(await page.locator("#users .user").count(), 1);
  await page.locator("#save").click();
  await page.locator("#message.success").waitFor();
  assert.deepEqual(Object.keys(writes[0]), ["blocked_caller_scopes"]);
  const management = {Authorization: "Bearer " + secret};
  let config = await (await page.request.get(configURL, {headers: management})).json();
  assert.equal(config.blocked_caller_scopes.length, 2);
  assert.equal(config.priority, 1000);
  assert.ok(config.store);
  const chat = {model: "test-chat", messages: [{role: "user", content: "hi"}]};
  await page.waitForTimeout(300);
  assert.equal((await page.request.post(base + "/v1/chat/completions", {headers: {Authorization: "Bearer " + keyB}, data: chat})).status(), 403);
  // Search must not drop rules hidden from the list. Unchecking restores Alice
  // while preserving Shayan and the host's store metadata.
  await allowed.uncheck();
  await page.locator("#save").click();
  await page.locator("#message.success").waitFor();
  config = await (await page.request.get(configURL, {headers: management})).json();
  assert.deepEqual(config.blocked_caller_scopes, [hash("cli-proxy-api:caller-scope:v1\0" + keyA)]);
  await page.waitForTimeout(300);
  assert.equal((await page.request.post(base + "/v1/chat/completions", {headers: {Authorization: "Bearer " + keyB}, data: chat})).status(), 200);
  assert.equal((await page.request.post(base + "/v1/chat/completions", {headers: {Authorization: "Bearer " + keyA}, data: chat})).status(), 403);
  // Detect another administrator's edits before saving stale data.
  await page.request.patch(configURL, {headers: management, data: {blocked_caller_scopes: [...config.blocked_caller_scopes, "a".repeat(64)]}});
  await page.locator("#save").click();
  await page.locator("#message.error").waitFor();
  assert.ok((await page.locator("#message").textContent()).includes("changed in another page"));
  await page.request.patch(configURL, {headers: management, data: {blocked_caller_scopes: config.blocked_caller_scopes}});
  // Saved CPAMC sessions should connect automatically, including obfuscation.
  await page.evaluate(({base, secret}) => {
    const bytes = new TextEncoder().encode(JSON.stringify({state: {apiBase: base, managementKey: secret}}));
    const mask = new TextEncoder().encode("cli-proxy-api-webui::secure-storage|" + location.host + "|" + navigator.userAgent);
    localStorage.setItem("cli-proxy-auth", "enc::v1::" + btoa(String.fromCharCode(...bytes.map((b, i) => b ^ mask[i % mask.length]))));
    localStorage.setItem("isLoggedIn", "true");
  }, {base, secret});
  await page.reload();
  await page.locator("#editor").waitFor({state: "visible"});
  assert.equal(await blocked.isChecked(), true);
  assert.equal(await page.locator("#login").isVisible(), false);
  assert.deepEqual(errors, []);
  await page.setViewportSize({width: 390, height: 844});
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
  await page.screenshot({path: "/tmp/key-chat-access-settings.png", fullPage: true});
  console.log("PASS: browser login/session reuse, names/search, add/remove enforcement, preserved settings, conflict detection, escaped labels, mobile layout");
} finally { await browser.close(); }
