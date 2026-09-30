# Heimdall web app: static SPA build served by nginx.
#   docker build --build-arg PUBLIC_API_URL=http://127.0.0.1:8080 -t heimdall-web web/
# PUBLIC_API_URL is baked in at build time ($env/static/public).
FROM node:22-alpine AS build
WORKDIR /app
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
ARG PUBLIC_API_URL=http://127.0.0.1:8080
ENV PUBLIC_API_URL=$PUBLIC_API_URL
RUN pnpm build

FROM nginx:1.27-alpine
COPY --from=build /app/build /usr/share/nginx/html
# SPA fallback: unknown paths render index.html (client-side routing).
RUN printf 'server {\n  listen 80;\n  root /usr/share/nginx/html;\n  location / { try_files $uri $uri/ /index.html; }\n}\n' > /etc/nginx/conf.d/default.conf
EXPOSE 80
