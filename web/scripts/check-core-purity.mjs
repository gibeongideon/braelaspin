#!/usr/bin/env node
/**
 * Enforces the architectural rule this project is built on:
 *
 *   src/core/ IS PURE BUSINESS LOGIC. It must never touch the DOM, and it must
 *   never import from src/ui/.
 *
 * That rule is what makes core/ portable to Dart for the Flutter client and
 * testable without a browser. It is easy to break by accident with a single
 * `document.` or a stray import, so it is checked mechanically rather than by
 * code review.
 *
 * Run: npm run check:core
 */

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const CORE = new URL('../src/core', import.meta.url).pathname;

/**
 * Browser globals that would tie core/ to a DOM.
 * `fetch`, `crypto`, `AbortController`, `setTimeout` and `performance` are
 * deliberately allowed: they are web-standard, exist in Node 18+, and have
 * direct Dart equivalents (`http`, `Random.secure`, `Timer`).
 */
const FORBIDDEN = [
  { re: /\bdocument\b/,            why: 'DOM access' },
  { re: /\bwindow\b/,              why: 'browser window' },
  { re: /\blocalStorage\b/,        why: 'browser storage (inject a TokenStore instead)' },
  { re: /\bsessionStorage\b/,      why: 'browser storage' },
  { re: /\bnavigator\b/,           why: 'browser navigator' },
  { re: /\balert\s*\(/,            why: 'browser dialog' },
  { re: /\bHTMLElement\b/,         why: 'DOM type' },
  { re: /\bfrom\s+['"]\.\.\/ui/,   why: "import from ui/ (core must not depend on the view layer)" },
  { re: /\bfrom\s+['"]\.\.\/main/, why: 'import from the composition root' },
];

function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (p.endsWith('.ts') && !p.endsWith('.test.ts')) out.push(p);
  }
  return out;
}

let failures = 0;
for (const file of walk(CORE)) {
  const src = readFileSync(file, 'utf8');
  const lines = src.split('\n');

  lines.forEach((line, i) => {
    // Skip comments — the rules are discussed in prose in these files.
    const trimmed = line.trim();
    if (trimmed.startsWith('*') || trimmed.startsWith('//') || trimmed.startsWith('/*')) return;

    for (const { re, why } of FORBIDDEN) {
      if (re.test(line)) {
        console.error(
          `✗ ${relative(process.cwd(), file)}:${i + 1}  ${why}\n    ${trimmed}`,
        );
        failures++;
      }
    }
  });
}

if (failures > 0) {
  console.error(
    `\n${failures} purity violation(s) in src/core/.\n` +
    `core/ must stay free of the DOM so it can be unit-tested without a browser\n` +
    `and ported to Dart for the Flutter client. Move browser code into src/ui/.`,
  );
  process.exit(1);
}

console.log('✓ src/core/ is pure — no DOM, no imports from ui/');
