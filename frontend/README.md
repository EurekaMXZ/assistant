# Assistant frontend

The frontend calls the public Go API directly. Next.js does not proxy `/api` requests.

## Development

```bash
cp .env.local.example .env.local
pnpm install
pnpm dev
```

Open [http://localhost:3000](http://localhost:3000). The default API prefix is `http://localhost:8080/api/v1`.

## Deployment

Set `NEXT_PUBLIC_API_BASE_URL` to the browser-visible API prefix before running `pnpm build`, for example:

```bash
NEXT_PUBLIC_API_BASE_URL=https://api.example.com/api/v1 pnpm build
```

This is a build-time public value. The Go API must set `WEB_ORIGIN` to the deployed frontend origin.
