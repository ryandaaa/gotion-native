# gotion OAuth Worker

Notion's OAuth needs a client secret, which a desktop app can't keep. This
Cloudflare Worker keeps it, exchanges the login code for tokens and posts
them to gotion on the user's own machine (`127.0.0.1` only).

## Setup (once)

1. **Notion integration.** At <https://www.notion.so/profile/integrations>
   make a new integration of type **Public**. Fill in the required fields
   (name, website, privacy policy and terms URLs can be your own pages).
   Under *OAuth Domain & URIs* add the redirect URI
   `https://gotion-oauth.<your-subdomain>.workers.dev/callback`
   (you get the exact host after the first deploy; add it then).
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
   GOTION_OAUTH_URL=https://gotion-oauth.<your-subdomain>.workers.dev go run .
   # or bake it in:
   go build -ldflags "-X gotion/internal/app.OAuthURL=https://gotion-oauth.<…>.workers.dev" .
   ```

## Endpoints

| | |
|---|---|
| `GET /login?port&state` | redirects to Notion's consent page |
| `GET /callback` | Notion returns here; tokens are posted to `http://127.0.0.1:<port>/callback` |
| `POST /refresh` | `{refresh_token}` → `{access_token, refresh_token}` |
