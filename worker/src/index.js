// gotion's OAuth helper, a Cloudflare Worker. Notion's token exchange
// needs the integration's client secret, which a desktop app cannot keep;
// this Worker keeps it, and hands the tokens to the app on the user's own
// machine (127.0.0.1) and nowhere else.
//
//   GET  /login?port=P&state=S  -> Notion's consent page
//   GET  /callback              <- Notion, with a code: exchanged for tokens,
//                                  then posted to http://127.0.0.1:P/callback
//   POST /refresh               {refresh_token} -> new tokens, as JSON
//
// Secrets: NOTION_CLIENT_ID, NOTION_CLIENT_SECRET (wrangler secret put).

const NOTION = "https://api.notion.com/v1";
const VERSION = "2026-03-11";

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    try {
      if (url.pathname === "/login" && request.method === "GET") return login(url, env);
      if (url.pathname === "/callback" && request.method === "GET") return await callback(url, env);
      if (url.pathname === "/refresh" && request.method === "POST") return await refresh(request, env);
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
