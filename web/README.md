# braelaspin — web client

TypeScript + Vite, **zero runtime dependencies**. Deployed as static files,
separately from the Go API.

## Layering — read this before adding code

```
src/core/   PURE business logic. No DOM. No imports from ui/.
            ↑ this is what ports to Dart for the Flutter client
src/ui/     the ONLY code that touches the DOM
src/main.ts composition root
```

`npm run check:core` fails the build if anything in `core/` touches
`document`, `window`, `localStorage`, or imports from `ui/`. That rule is what
keeps the logic portable and testable without a browser — please don't work
around it, move the browser code into `ui/` instead.

## Commands

```bash
npm install
npm run dev        # vite dev server on :5173
npm run verify     # typecheck + core purity + tests + build
npm run build      # production bundle into dist/
npm test           # core unit tests
```

Needs the API running: `cd .. && make up && make run`.

## Configuration

`VITE_API_BASE` is baked in **at build time**, so each environment needs its
own build. See `.env.example`.

## Size budget

~16 KB gzipped (JS + CSS + HTML). If a dependency would push that past ~40 KB,
it needs a better justification than convenience.
