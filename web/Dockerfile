# ── build ────────────────────────────────────────────────────────────────────
FROM node:20-alpine AS build
WORKDIR /src
COPY package.json package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund
COPY . .
# VITE_* is compile-time, so each environment gets its own build.
ARG VITE_API_BASE=https://api.braelaspin.co.ke
ENV VITE_API_BASE=$VITE_API_BASE
RUN npm run typecheck && npm run check:core && npx vite build

# ── serve ────────────────────────────────────────────────────────────────────
# Static files only. This image contains no application code and talks to
# nothing: the API is a separate deployable on its own origin.
FROM nginx:1.27-alpine
COPY --from=build /src/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://localhost/ >/dev/null || exit 1
