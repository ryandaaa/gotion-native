// node test.mjs — checks the Worker against a fake Notion.
import assert from "node:assert/strict";
import worker from "./src/index.js";

const kv = new Map();
const env = {
  NOTION_CLIENT_ID: "cid",
  NOTION_CLIENT_SECRET: "secret",
  EVENTS: { get: async (k) => kv.get(k) ?? null, put: async (k, v) => void kv.set(k, v) },
};
let lastToken;
let meCalls = 0;
globalThis.fetch = async (url, init) => {
  if (url === "https://api.notion.com/v1/users/me") {
    meCalls++;
    const ok = init.headers.Authorization === "Bearer tok-A";
    return ok ? Response.json({ bot: { workspace_id: "ws-A" } }) : Response.json({}, { status: 401 });
  }
  assert.equal(url, "https://api.notion.com/v1/oauth/token");
  assert.equal(init.headers.Authorization, "Basic " + btoa("cid:secret"));
  lastToken = JSON.parse(init.body);
  if (lastToken.code === "bad" || lastToken.refresh_token === "bad") {
    return Response.json({ object: "error", message: "invalid_grant" }, { status: 400 });
  }
  return Response.json({ access_token: "acc<>\"", refresh_token: "ref", workspace_name: "Ruang & Co" });
};
const call = (path, init) => worker.fetch(new Request("https://w.example" + path, init), env);
const state = "abcdefghijklmnopqrstuvwx";

// login: to Notion's page, with the port and state carried in state
let r = await call(`/login?port=43123&state=${state}`);
assert.equal(r.status, 302);
const to = new URL(r.headers.get("Location"));
assert.equal(to.origin + to.pathname, "https://api.notion.com/v1/oauth/authorize");
assert.equal(to.searchParams.get("redirect_uri"), "https://w.example/callback");
assert.equal(to.searchParams.get("state"), "43123." + state);
assert.equal(to.searchParams.get("owner"), "user");

// a port out of range or a short state is refused
assert.equal((await call(`/login?port=80&state=${state}`)).status, 400);
assert.equal((await call(`/login?port=43123&state=short`)).status, 400);

// callback: tokens go to 127.0.0.1 only, escaped, in a form
r = await call(`/callback?code=c1&state=43123.${state}`);
let html = await r.text();
assert.equal(lastToken.grant_type, "authorization_code");
assert.equal(lastToken.redirect_uri, "https://w.example/callback");
assert.match(html, /action="http:\/\/127\.0\.0\.1:43123\/callback"/);
assert.match(html, /name="access_token" value="acc&lt;&gt;&quot;"/);
assert.match(html, /name="state" value="abcdefghijklmnopqrstuvwx"/);
assert.match(html, /value="Ruang &amp; Co"/);
assert.equal(r.headers.get("Cache-Control"), "no-store");

// a denied consent reaches the app as an error
html = await (await call(`/callback?error=access_denied&state=43123.${state}`)).text();
assert.match(html, /name="error" value="Login dibatalkan."/);
assert.doesNotMatch(html, /access_token/);

// a forged state with a port elsewhere is refused
assert.equal((await call(`/callback?code=c1&state=80.${state}`)).status, 400);

// refresh
r = await call("/refresh", { method: "POST", body: JSON.stringify({ refresh_token: "old" }) });
assert.deepEqual(await r.json(), { access_token: "acc<>\"", refresh_token: "ref" });
assert.equal(lastToken.grant_type, "refresh_token");
r = await call("/refresh", { method: "POST", body: JSON.stringify({ refresh_token: "bad" }) });
assert.equal(r.status, 401);

// webhook: the verification token is kept, then events must be signed.
r = await call("/webhook", { method: "POST", body: JSON.stringify({ verification_token: "secret_v" }) });
assert.equal(r.status, 200);
assert.equal(kv.get("verification_token"), "secret_v");
r = await call("/webhook", { method: "POST", body: JSON.stringify({ verification_token: "attacker" }) });
assert.equal(r.status, 409, "a later token does not replace the first");
assert.equal(kv.get("verification_token"), "secret_v");
const ev = (id, ws, t) => JSON.stringify({ id, workspace_id: ws, type: "page.created", timestamp: t, entity: { id: "p" + id, type: "page" }, data: { parent: { id: "root", type: "page" } } });
const sign = async (body) => {
  const k = await crypto.subtle.importKey("raw", new TextEncoder().encode("secret_v"), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const s = await crypto.subtle.sign("HMAC", k, new TextEncoder().encode(body));
  return "sha256=" + [...new Uint8Array(s)].map((b) => b.toString(16).padStart(2, "0")).join("");
};
let body = ev("1", "ws-A", "2026-11-24T10:00:00Z");
r = await call("/webhook", { method: "POST", body, headers: { "X-Notion-Signature": "sha256=00" } });
assert.equal(r.status, 401, "an unsigned event is refused");
r = await call("/webhook", { method: "POST", body, headers: { "X-Notion-Signature": await sign(body) } });
assert.equal(r.status, 200);
body = ev("2", "ws-B", "2026-11-24T10:01:00Z"); // another workspace
await call("/webhook", { method: "POST", body, headers: { "X-Notion-Signature": await sign(body) } });
body = ev("3", "ws-A", "2026-11-24T10:02:00Z");
await call("/webhook", { method: "POST", body, headers: { "X-Notion-Signature": await sign(body) } });
await call("/webhook", { method: "POST", body, headers: { "X-Notion-Signature": await sign(body) } }); // retried: kept once

// events: a token reads its own workspace's events only, after since.
r = await call("/events?since=2026-11-24T10:00:30Z", { headers: { Authorization: "Bearer tok-A" } });
let got = await r.json();
assert.deepEqual(got.events.map((e) => e.id), ["3"]);
r = await call("/events", { headers: { Authorization: "Bearer tok-A" } });
got = await r.json();
assert.deepEqual(got.events.map((e) => e.id), ["1", "3"], "no event of ws-B, none twice");
assert.equal(meCalls, 1, "the token's workspace is cached");
assert.equal((await call("/events", { headers: { Authorization: "Bearer stolen" } })).status, 401);
assert.equal((await call("/events")).status, 401);
for (const k of kv.keys()) assert.ok(!k.includes("tok-A"), "tokens are never kept as such");

console.log("worker: all checks passed");
