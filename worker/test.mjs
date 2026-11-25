// node test.mjs — checks the Worker against a fake Notion.
import assert from "node:assert/strict";
import worker, { Workspace } from "./src/index.js";

const kv = new Map();
const env = {
  NOTION_CLIENT_ID: "cid",
  NOTION_CLIENT_SECRET: "secret",
  GITHUB_REPO: "me/gotion",
  EVENTS: { get: async (k) => kv.get(k) ?? null, put: async (k, v) => void kv.set(k, v) },
};
let lastToken;
let meCalls = 0;
let releases = 0;
let release = null; // none published yet
let limited = false; // GitHub refusing: its rate limit
globalThis.fetch = async (url, init) => {
  if (url === "https://api.github.com/repos/me/gotion/releases/latest") {
    releases++;
    if (limited) return Response.json({ message: "API rate limit exceeded" }, { status: 403 });
    return release ? Response.json(release) : Response.json({ message: "Not Found" }, { status: 404 });
  }
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

// downloads: none before the first release, then the newest file of each kind.
r = await call("/download/windows");
assert.equal(r.status, 404);
assert.match(await r.text(), /Coming soon/);
assert.equal((await call("/download/nonsense")).status, 404);
release = {
  assets: [
    { name: "gotion-setup-0.2.0-amd64.exe", browser_download_url: "https://dl/win64" },
    { name: "gotion-setup-0.2.0-arm64.exe", browser_download_url: "https://dl/winarm" },
    { name: "gotion-0.2.0-arm64.dmg", browser_download_url: "https://dl/mac" },
    { name: "gotion-0.2.0-amd64.dmg", browser_download_url: "https://dl/macintel" },
    { name: "gotion_0.2.0_amd64.deb", browser_download_url: "https://dl/deb" },
    { name: "gotion-0.2.0-1.x86_64.rpm", browser_download_url: "https://dl/rpm" },
    { name: "gotion-0.2.0-linux-amd64.tar.gz", browser_download_url: "https://dl/tar" },
    { name: "install.sh", browser_download_url: "https://dl/install" },
  ],
};
r = await call("/download/windows");
assert.equal(r.status, 404, "the answer of a few seconds ago still holds");
const realNow = Date.now;
Date.now = () => realNow() + 31 * 1000; // half a minute later: asked again
const urls = {
  windows: "https://dl/win64", "windows-arm": "https://dl/winarm", mac: "https://dl/mac", "mac-intel": "https://dl/macintel",
  "linux-deb": "https://dl/deb", "linux-rpm": "https://dl/rpm", "linux-tar": "https://dl/tar", "install.sh": "https://dl/install",
};
for (const [kind, where] of Object.entries(urls)) {
  r = await call("/download/" + kind);
  assert.equal(r.status, 302, kind);
  assert.equal(r.headers.get("Location"), where, kind);
}
r = await call("/download/releases");
assert.equal(r.headers.get("Location"), "https://github.com/me/gotion/releases");
assert.equal(releases, 2, "GitHub is asked once per five minutes");

// GitHub's rate limit: the release known before keeps the links working.
limited = true;
Date.now = () => realNow() + 7 * 60 * 1000;
r = await call("/download/mac");
assert.equal(r.headers.get("Location"), "https://dl/mac", "a refusal keeps the known release");
assert.equal(releases, 3);
await call("/download/mac");
assert.equal(releases, 3, "a refusal is asked again after half a minute, not at once");

// A Worker that just started, while GitHub refuses: the release kept in KV.
const fresh = (await import("./src/index.js?cold")).default;
r = await fresh.fetch(new Request("https://w.example/download/linux-deb"), env);
assert.equal(r.headers.get("Location"), "https://dl/deb", "kept in KV");
// ...and with nothing kept either: GitHub's releases page, not "coming soon".
const bare = (await import("./src/index.js?bare")).default;
r = await bare.fetch(new Request("https://w.example/download/windows"), { ...env, EVENTS: { get: async () => null, put: async () => {} } });
assert.equal(r.status, 302);
assert.equal(r.headers.get("Location"), "https://github.com/me/gotion/releases/latest");
Date.now = realNow;

// live: the webhook pushes each event to the workspace's Durable Object, and
// /live hands a token's app a socket of its own workspace only.
const published = [];
const connects = [];
let liveDown = false;
const liveEnv = {
  ...env,
  LIVE: {
    idFromName: (n) => "id:" + n,
    get: (id) => ({
      fetch: async (req, init) => {
        if (liveDown) throw new Error("object unreachable");
        const u = typeof req === "string" ? req : req.url;
        if (u.endsWith("/publish")) published.push({ id, event: JSON.parse(init.body) });
        else connects.push(id);
        return new Response("ok");
      },
    }),
  },
};
const lcall = (path, init) => worker.fetch(new Request("https://w.example" + path, init), liveEnv);
body = JSON.stringify({ id: "9", workspace_id: "ws-A", type: "page.content_updated", timestamp: "2026-11-24T10:05:00Z",
  entity: { id: "pg", type: "page" }, data: { parent: { id: "root", type: "page" }, updated_blocks: [{ id: "b1", type: "block" }] } });
r = await lcall("/webhook", { method: "POST", body, headers: { "X-Notion-Signature": await sign(body) } });
assert.equal(r.status, 200);
assert.equal(published.length, 1);
assert.equal(published[0].id, "id:ws-A", "pushed to its own workspace's object");
assert.deepEqual(published[0].event.data.updated_blocks, [{ id: "b1", type: "block" }], "the changed blocks travel");
got = await (await lcall("/events?since=2026-11-24T10:04:00Z", { headers: { Authorization: "Bearer tok-A" } })).json();
assert.deepEqual(got.events.map((e) => e.id), ["9"], "and kept in KV for the apps that poll");

// The object unreachable: the event is still kept, the webhook still 200.
liveDown = true;
body = JSON.stringify({ id: "10", workspace_id: "ws-A", type: "page.created", timestamp: "2026-11-24T10:06:00Z", entity: { id: "p10", type: "page" } });
r = await lcall("/webhook", { method: "POST", body, headers: { "X-Notion-Signature": await sign(body) } });
assert.equal(r.status, 200, "a failed push never fails the webhook");
got = await (await lcall("/events?since=2026-11-24T10:05:30Z", { headers: { Authorization: "Bearer tok-A" } })).json();
assert.deepEqual(got.events.map((e) => e.id), ["10"]);
liveDown = false;

const up = { Upgrade: "websocket" };
assert.equal((await lcall("/live", { headers: { ...up } })).status, 401, "no token, no socket");
assert.equal((await lcall("/live", { headers: { ...up, Authorization: "Bearer stolen" } })).status, 401);
assert.equal((await lcall("/live", { headers: { Authorization: "Bearer tok-A" } })).status, 426, "a WebSocket upgrade only");
r = await lcall("/live", { headers: { ...up, Authorization: "Bearer tok-A" } });
assert.deepEqual(connects, ["id:ws-A"], "the token's own workspace");
assert.equal((await call("/live", { headers: { ...up, Authorization: "Bearer tok-A" } })).status, 404, "no LIVE: the app polls");

// The object: it pushes to every socket, skips a broken one, and refuses
// sockets past its limit.
const sockets = [];
const sock = (broken) => ({ got: [], send(m) { if (broken) throw new Error("gone"); this.got.push(m); } });
const ws = new Workspace({ getWebSockets: () => sockets, acceptWebSocket: () => {}, setWebSocketAutoResponse: () => {} }, {});
sockets.push(sock(false), sock(true), sock(false));
r = await ws.fetch(new Request("https://live/publish", { method: "POST", body: '{"id":"x"}' }));
assert.deepEqual(await r.json(), { sent: 2 });
assert.deepEqual(sockets[0].got, ['{"id":"x"}']);
while (sockets.length < 32) sockets.push(sock(false));
assert.equal((await ws.fetch(new Request("https://live/connect"))).status, 429, "too many sockets");
const pinged = sock(false);
ws.webSocketMessage(pinged, "ping");
assert.deepEqual(pinged.got, ["pong"]);

console.log("worker: all checks passed");
