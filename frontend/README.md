# Assistant frontend

The frontend uses the same-origin `/api/v1` prefix by default. In the Compose deployment, Nginx proxies those requests to the Go API; Next.js itself does not proxy `/api` requests.

## Development

```bash
cp .env.local.example .env.local
pnpm install
pnpm dev
```

Open [http://localhost:3000](http://localhost:3000). The copied `.env.local` points the development frontend at `http://localhost:8080/api/v1`.

## Deployment

Compose builds with the same-origin `/api/v1` prefix and publishes the application through Nginx. For a separate frontend deployment, set `NEXT_PUBLIC_API_BASE_URL` to the browser-visible API prefix before running `pnpm build`, for example:

```bash
NEXT_PUBLIC_API_BASE_URL=https://api.example.com/api/v1 pnpm build
```

This is a build-time public value. The Go API must set `WEB_ORIGIN` to the deployed frontend origin.
