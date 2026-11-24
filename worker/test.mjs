// node test.mjs — checks the Worker against a fake Notion.
import assert from "node:assert/strict";
import worker from "./src/index.js";

const env = { NOTION_CLIENT_ID: "cid", NOTION_CLIENT_SECRET: "secret" };
let lastToken;
globalThis.fetch = async (url, init) => {
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

console.log("worker: all checks passed");
