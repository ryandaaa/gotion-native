// gotion's OAuth helper, a Cloudflare Worker. Notion's token exchange
// needs the integration's client secret, which a desktop app cannot keep;
// this Worker keeps it, and hands the tokens to the app on the user's own
// machine (127.0.0.1) and nowhere else.
//
//   GET  /login?port=P&state=S  -> Notion's consent page
//   GET  /callback              <- Notion, with a code: exchanged for tokens,
//                                  then posted to http://127.0.0.1:P/callback
//   POST /refresh               {refresh_token} -> new tokens, as JSON
//   POST /webhook               <- Notion's events, signed; kept a while
//   GET  /events?since=T        -> a workspace's events since T, for the
//                                  app holding one of its tokens
//   GET  /download/<kind>       -> the newest release's file of that kind,
//                                  on GitHub (GITHUB_REPO)
//
// Secrets: NOTION_CLIENT_ID, NOTION_CLIENT_SECRET (wrangler secret put).
// KV: EVENTS, for the webhook's verification token, the events and which
// workspace a token belongs to.

const NOTION = "https://api.notion.com/v1";
const VERSION = "2026-03-11";

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    try {
      if (url.pathname === "/login" && request.method === "GET") return login(url, env);
      if (url.pathname === "/callback" && request.method === "GET") return await callback(url, env);
      if (url.pathname === "/refresh" && request.method === "POST") return await refresh(request, env);
      if (url.pathname === "/webhook" && request.method === "POST") return await webhook(request, env);
      if (url.pathname === "/events" && request.method === "GET") return await events(request, url, env);
      if (url.pathname.startsWith("/download/") && request.method === "GET") return await download(url, env);
      return new Response("gotion OAuth", { status: 404 });
    } catch (e) {
      return page("Login gagal", "Terjadi kesalahan: " + e.message, 500);
    }
  },
};

// The app listens on a port of its own machine: only one in the range a
// listener gets, and only on 127.0.0.1, so tokens never go elsewhere.
function validPort(p) {
  const n = Number(p);
  return Number.isInteger(n) && n >= 1024 && n <= 65535 ? n : 0;
}

function redirectURI(url) {
  return url.origin + "/callback";
}

function login(url, env) {
  const port = validPort(url.searchParams.get("port"));
  const state = url.searchParams.get("state") || "";
  if (!port || !/^[A-Za-z0-9_-]{16,128}$/.test(state)) {
    return page("Login gagal", "Permintaan login tidak valid. Mulai lagi dari gotion.", 400);
  }
  const auth = new URL(NOTION + "/oauth/authorize");
  auth.searchParams.set("client_id", env.NOTION_CLIENT_ID);
  auth.searchParams.set("response_type", "code");
  auth.searchParams.set("owner", "user");
  auth.searchParams.set("redirect_uri", redirectURI(url));
  // The app's state and its port travel through Notion and come back.
  auth.searchParams.set("state", port + "." + state);
  return Response.redirect(auth.toString(), 302);
}

async function token(env, body) {
  const res = await fetch(NOTION + "/oauth/token", {
    method: "POST",
    headers: {
      Authorization: "Basic " + btoa(env.NOTION_CLIENT_ID + ":" + env.NOTION_CLIENT_SECRET),
      "Content-Type": "application/json",
      "Notion-Version": VERSION,
    },
    body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.message || data.error || "HTTP " + res.status);
  return data;
}

async function callback(url, env) {
  const [portText, state] = (url.searchParams.get("state") || "").split(".", 2);
  const port = validPort(portText);
  if (!port || !state) return page("Login gagal", "Balasan dari Notion tidak dikenali. Mulai lagi dari gotion.", 400);
  const error = url.searchParams.get("error");
  const fields = { state };
  if (error) {
    fields.error = error === "access_denied" ? "Login dibatalkan." : error;
  } else {
    const data = await token(env, {
      grant_type: "authorization_code",
      code: url.searchParams.get("code"),
      redirect_uri: redirectURI(url),
    });
    fields.access_token = data.access_token;
    fields.refresh_token = data.refresh_token || "";
    fields.workspace_name = data.workspace_name || "";
  }
  // Posted, not put in a URL: the tokens stay out of the browser's history.
  const inputs = Object.entries(fields)
    .map(([k, v]) => `<input type="hidden" name="${k}" value="${escape(v)}">`)
    .join("");
  return new Response(
    `<!doctype html><meta charset="utf-8"><title>gotion</title>
<body style="font:15px system-ui;display:grid;place-items:center;height:90vh;color:#444">
<form id="f" method="post" action="http://127.0.0.1:${port}/callback">${inputs}
<p>Menghubungkan ke gotion…</p><noscript><button>Lanjutkan</button></noscript></form>
<script>document.getElementById("f").submit()</script></body>`,
    { headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store", "Referrer-Policy": "no-referrer" } },
  );
}

async function refresh(request, env) {
  const { refresh_token } = await request.json().catch(() => ({}));
  if (!refresh_token) return Response.json({ error: "refresh_token missing" }, { status: 400 });
  try {
    const data = await token(env, { grant_type: "refresh_token", refresh_token });
    return Response.json({ access_token: data.access_token, refresh_token: data.refresh_token || "" });
  } catch (e) {
    return Response.json({ error: e.message }, { status: 401 });
  }
}

function escape(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function page(title, text, status) {
  return new Response(
    `<!doctype html><meta charset="utf-8"><title>${escape(title)}</title>
<body style="font:15px system-ui;display:grid;place-items:center;height:90vh;color:#444"><div><h2>${escape(title)}</h2><p>${escape(text)}</p></div></body>`,
    { status, headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" } },
  );
}

const KEEP = 100; // events kept per workspace
const EVENT_TTL = 2 * 24 * 3600; // seconds

async function hmacHex(key, body) {
  const k = await crypto.subtle.importKey("raw", new TextEncoder().encode(key), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const sig = await crypto.subtle.sign("HMAC", k, new TextEncoder().encode(body));
  return [...new Uint8Array(sig)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

async function sha256Hex(s) {
  const d = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
  return [...new Uint8Array(d)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

// Equal in constant time, as a signature check must be.
function same(a, b) {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return d === 0;
}

// webhook takes Notion's events. The first post of a subscription carries
// its verification token: kept (read it with wrangler kv key get, and
// paste it in Notion), and from then on every event must be signed with
// it, an HMAC of the raw body.
async function webhook(request, env) {
  const raw = await request.text();
  let body;
  try {
    body = JSON.parse(raw);
  } catch {
    return new Response("bad json", { status: 400 });
  }
  if (body.verification_token) {
    // The first token is kept and no later one replaces it: anyone may
    // post here, and a token of theirs would let them sign events. Set
    // NOTION_WEBHOOK_SECRET to the real one to override it.
    if (env.NOTION_WEBHOOK_SECRET || (await env.EVENTS.get("verification_token"))) {
      return new Response("already verified", { status: 409 });
    }
    await env.EVENTS.put("verification_token", body.verification_token);
    return new Response("ok");
  }
  const secret = env.NOTION_WEBHOOK_SECRET || (await env.EVENTS.get("verification_token"));
  const sig = request.headers.get("X-Notion-Signature") || "";
  if (!secret || !same(sig, "sha256=" + (await hmacHex(secret, raw)))) {
    return new Response("bad signature", { status: 401 });
  }
  if (!body.workspace_id || !body.type) return new Response("ok");
  const key = "ev:" + body.workspace_id;
  const list = JSON.parse((await env.EVENTS.get(key)) || "[]");
  if (!list.some((e) => e.id === body.id)) {
    list.push({
      id: body.id,
      type: body.type,
      timestamp: body.timestamp,
      entity: body.entity,
      parent: body.data && body.data.parent,
      authors: body.authors,
    });
  }
  list.sort((a, b) => (a.timestamp < b.timestamp ? -1 : 1));
  await env.EVENTS.put(key, JSON.stringify(list.slice(-KEEP)), { expirationTtl: EVENT_TTL });
  return new Response("ok");
}

// events gives the app its workspace's events after since: the workspace is
// the one its Notion token belongs to, as Notion says (GET /users/me), so
// no token reads another workspace's events.
async function events(request, url, env) {
  const auth = request.headers.get("Authorization") || "";
  const token = auth.startsWith("Bearer ") ? auth.slice(7) : "";
  if (!token) return Response.json({ error: "token missing" }, { status: 401 });
  const cacheKey = "auth:" + (await sha256Hex(token));
  let workspace = await env.EVENTS.get(cacheKey);
  if (!workspace) {
    const res = await fetch(NOTION + "/users/me", { headers: { Authorization: "Bearer " + token, "Notion-Version": VERSION } });
    if (!res.ok) return Response.json({ error: "token refused" }, { status: 401 });
    const me = await res.json();
    workspace = me.bot && me.bot.workspace_id;
    if (!workspace) return Response.json({ error: "no workspace" }, { status: 401 });
    await env.EVENTS.put(cacheKey, workspace, { expirationTtl: 600 });
  }
  const since = url.searchParams.get("since") || "";
  const list = JSON.parse((await env.EVENTS.get("ev:" + workspace)) || "[]");
  return Response.json({ events: list.filter((e) => !since || e.timestamp > since), now: new Date().toISOString() });
}

// The files of a release, by what the website calls them. Their names are
// those mygo build gives: gotion-setup-0.1.0-amd64.exe, gotion-0.1.0-arm64.dmg,
// gotion_0.1.0_amd64.deb, gotion-0.1.0-1.x86_64.rpm,
// gotion-0.1.0-linux-amd64.tar.gz, install.sh.
const ASSETS = {
  windows: /-setup-.*-amd64\.exe$/,
  "windows-arm": /-setup-.*-arm64\.exe$/,
  mac: /-arm64\.dmg$/,
  "mac-intel": /-amd64\.dmg$/,
  "linux-deb": /_amd64\.deb$/,
  "linux-rpm": /\.x86_64\.rpm$/,
  "linux-tar": /-linux-amd64\.tar\.gz$/,
  "install.sh": /^install\.sh$/,
};

// The newest release, asked of GitHub at most every 5 minutes per Worker.
let latest = { at: 0, release: null };

async function newestRelease(repo) {
  if (Date.now() - latest.at < 5 * 60 * 1000) return latest.release;
  const res = await fetch("https://api.github.com/repos/" + repo + "/releases/latest", {
    headers: { "User-Agent": "gotion-website", Accept: "application/vnd.github+json" },
  });
  const release = res.ok ? await res.json() : null;
  latest = { at: Date.now(), release };
  return release;
}

// download sends the visitor to the newest release's file, or tells them
// there is none yet.
async function download(url, env) {
  const repo = env.GITHUB_REPO || "";
  const kind = url.pathname.slice("/download/".length);
  if (kind === "releases") {
    const release = repo && (await newestRelease(repo));
    if (release) return Response.redirect("https://github.com/" + repo + "/releases", 302);
    return notYet();
  }
  const pattern = ASSETS[kind];
  if (!pattern) return page("Not found · Tidak ditemukan", "No such download. · Unduhan itu tidak ada.", 404);
  const release = repo && (await newestRelease(repo));
  const asset = release && (release.assets || []).find((a) => pattern.test(a.name));
  if (!asset) return notYet();
  return Response.redirect(asset.browser_download_url, 302);
}

function notYet() {
  return page(
    "Coming soon · Segera hadir",
    "gotion has no release for this system yet. Check back soon. · Belum ada rilis gotion untuk sistem ini. Coba lagi nanti.",
    404,
  );
}
