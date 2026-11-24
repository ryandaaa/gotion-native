# gotion OAuth Worker

Notion's OAuth needs a client secret, which a desktop app can't keep. This
Cloudflare Worker keeps it, exchanges the login code for tokens and posts
them to gotion on the user's own machine (`127.0.0.1` only).

## Setup (once)

1. **Notion integration.** At <https://www.notion.so/profile/integrations>
   make a new integration of type **Public**. Fill in the required fields
   (name, website, privacy policy and terms URLs can be your own pages).
   Under *OAuth Domain & URIs* add the redirect URI
   `https://gotion.ryanda.my.id/callback` (the Worker's custom domain,
   set in `wrangler.toml`; a zone of the same Cloudflare account).
   Copy the **OAuth client ID** and **client secret**.
2. **Deploy.**
   ```sh
   cd worker
   npx wrangler login
   npx wrangler deploy
   npx wrangler secret put NOTION_CLIENT_ID
   npx wrangler secret put NOTION_CLIENT_SECRET
   ```
3. **Point gotion at it.**
   ```sh
   # The app uses https://gotion.ryanda.my.id by default; another Worker:
   GOTION_OAUTH_URL=https://auth.example.com go run .
   go build -ldflags "-X gotion/internal/app.OAuthURL=https://auth.example.com" .
   ```

## Endpoints

| | |
|---|---|
| `GET /login?port&state` | redirects to Notion's consent page |
| `GET /callback` | Notion returns here; tokens are posted to `http://127.0.0.1:<port>/callback` |
| `POST /refresh` | `{refresh_token}` → `{access_token, refresh_token}` |
