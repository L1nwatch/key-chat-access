import {test} from "node:test";
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {callerScope, makeUsers, normalizeScopes, readSession} from "./web/settings.js";

const origin = "https://example.test";
const loc = {origin, host: "example.test"};
const ua = "test-user-agent";
const hash = text => createHash("sha256").update(text).digest("hex");
const storage = data => ({getItem: name => Object.hasOwn(data, name) ? data[name] : null});
function encode(value) {
  const raw = new TextEncoder().encode(JSON.stringify(value));
  const mask = new TextEncoder().encode("cli-proxy-api-webui::secure-storage|" + loc.host + "|" + ua);
  return "enc::v1::" + Buffer.from(raw.map((byte, index) => byte ^ mask[index % mask.length])).toString("base64");
}

test("scope uses the host identity domain and trims the exact client key", async () => {
  const key = "local-tést-user";
  assert.equal(await callerScope("  " + key + "\n"), hash("cli-proxy-api:caller-scope:v1\0" + key));
  assert.notEqual(await callerScope(key), hash(key));
});

test("reuses CPAMC encrypted session and refuses another server's password", () => {
  const data = {isLoggedIn: "true", "cli-proxy-auth": encode({state: {apiBase: origin, managementKey: "test-password"}})};
  assert.equal(readSession(storage(data), loc, ua).key, "test-password");
  data["cli-proxy-auth"] = encode({state: {apiBase: "https://another.test", managementKey: "test-password"}});
  assert.equal(readSession(storage(data), loc, ua).key, "");
  assert.equal(readSession(storage({}), loc, ua).key, "");
  assert.equal(readSession(storage({isLoggedIn: "true", "cli-proxy-auth": "enc::v1::invalid"}), loc, ua).key, "");
});

test("list resolves panel names without retaining raw keys and preserves unknown rules", async () => {
  const key = "local-very-long-client-secret-123456";
  const scope = await callerScope(key), unknown = "a".repeat(64);
  const names = {[hash(JSON.stringify(["api-key-name", origin, key]))]: "Shayan"};
  const rows = await makeUsers([key, " " + key + " "], [scope, unknown], origin, names);
  assert.equal(rows.length, 2);
  assert.equal(rows[0].label, "Shayan");
  assert.equal(rows[0].blocked, true);
  assert.equal(rows[1].scope, unknown);
  assert.equal(rows[1].blocked, true);
  assert.equal(rows[1].unknown, true);
  assert.ok(!JSON.stringify(rows).includes(key));
});

test("scope validation prevents malformed or silently dropped policies", () => {
  const scope = "a".repeat(64);
  assert.deepEqual(normalizeScopes([scope, scope.toUpperCase()]), [scope]);
  assert.throws(() => normalizeScopes(["username"]));
  assert.throws(() => normalizeScopes("not-an-array"));
});

test("unnamed keys show a searchable suffix while concealing the secret", async () => {
  const key = "sk-" + "AbCdEf0123".repeat(3) + "shayan1234";
  const [user] = await makeUsers([key], [], origin);
  assert.ok(user.masked.includes("shayan"));
  assert.ok(!JSON.stringify(user).includes(key));
});
