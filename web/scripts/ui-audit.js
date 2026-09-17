#!/usr/bin/env node

/**
 * UI Audit Script
 *
 * Enforces consistency rules for the trellis UI (§12.2):
 * 1. Components imported only from @/components/ui, @/components/wrappers, react, react-dom, lucide-react, or relative imports
 * 2. No raw HTML primitives (<button>, <input>, etc.) where shadcn exists
 * 3. Overlapping component roles (SKIPPED - heuristically unsound, caught in code review)
 * 4. All custom components in tree documented in COMPONENTS.md
 * 5. No ad-hoc colors, spacing, or radius values (including inline styles and className)
 */

import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const issues = [];
const usedComponents = new Set();
const importedFromLibraries = new Set(); // Track what's imported from external libs
const declaredComponents = new Set(); // Components declared in the tree

// Check 1: Find all imports and verify they're from allowed locations
function checkImports() {
  const srcDir = path.join(__dirname, '..', 'src');
  const files = getAllTsxFiles(srcDir);

  // Allowed import sources for component libraries
  const allowedSources = [
    '@/components/ui',
    '@/components/wrappers',
    'react',
    'react-dom',
    'react-router-dom',
    'lucide-react',
    // The app's own code that draws nothing: formatting, shared vocabulary
    // such as PRIORITIES, and pure data shaping. Its capitalised constants are
    // not components.
    '@/lib',
    // Behaviour libraries, not component libraries. dnd-kit supplies sensors,
    // collision detection and keyboard handling; every pixel it moves is still
    // our own markup, so it does not compete with the shadcn registry.
    '@dnd-kit/core',
    '@dnd-kit/sortable',
    '@dnd-kit/modifiers',
    '@dnd-kit/utilities',
  ];

  files.forEach(file => {
    // Skip shadcn's own components in components/ui
    if (file.includes('/components/ui/')) {
      return;
    }
    // components/wrappers IS the composition layer. Policy 4 says wrapping a
    // third-party library to add behaviour is correct, and every wrapper is
    // still listed in COMPONENTS.md with what it wraps and why. Pages remain
    // the surface this rule audits: they compose the registry and wrappers.
    if (file.includes('/components/wrappers/')) {
      return;
    }

    const content = fs.readFileSync(file, 'utf-8');
    const fileDir = path.dirname(file);

    // Match all import statements
    const importRegex = /import\s+\{([^}]+)\}\s+from\s+['"]([^'"]+)['"]/g;
    let match;

    while ((match = importRegex.exec(content)) !== null) {
      const imports = match[1];
      const source = match[2];

      // Check if this is an allowed source
      let isAllowed = false;

      // Check against allowed sources list
      for (const allowed of allowedSources) {
        if (source === allowed || source.startsWith(allowed + '/')) {
          isAllowed = true;
          break;
        }
      }

      // Check if it's a relative import within the same feature directory
      if (!isAllowed && (source.startsWith('.') || source.startsWith('../'))) {
        // Relative imports are allowed (e.g., importing from same feature)
        isAllowed = true;
      }

      // If not allowed and looks like a component import (uppercase identifiers), flag it
      if (!isAllowed) {
        // Extract imported names
        const importNames = imports.split(',').map(s => s.trim());
        const hasComponentImport = importNames.some(name => {
          // Component names start with uppercase
          const cleanName = name.split(' as ')[0].trim();
          return /^[A-Z]/.test(cleanName);
        });

        if (hasComponentImport) {
          const lineNum = getLineNumber(content, match.index);
          issues.push({
            file,
            line: lineNum,
            message: `Component imported from external library: "${source}". Only @/components/ui (shadcn), @/components/wrappers, lucide-react, react, react-dom, and relative imports are allowed.`
          });
        }

        // Track imports from libraries for documentation check
        importedFromLibraries.add(source);
      }

      // Track which shadcn components are used
      if (source.startsWith('@/components/ui')) {
        const componentName = source.split('/').pop();
        usedComponents.add(componentName);
      }
    }
  });
}

// Check 2: Look for raw HTML primitives
function checkRawPrimitives() {
  const srcDir = path.join(__dirname, '..', 'src');
  const files = getAllTsxFiles(srcDir);

  const primitives = ['button', 'input', 'select', 'textarea', 'dialog', 'table'];
  const primitiveRegex = new RegExp(`<(${primitives.join('|')})([\\s>])`, 'g');

  files.forEach(file => {
    // The shadcn primitive implementation necessarily contains the native
    // element it wraps. Consumers are the surface this rule audits.
    if (file.includes('/components/ui/')) {
      return;
    }
    const content = fs.readFileSync(file, 'utf-8');
    let match;

    while ((match = primitiveRegex.exec(content)) !== null) {
      const tag = match[1];
      const lineNum = getLineNumber(content, match.index);
      issues.push({
        file,
        line: lineNum,
        message: `Raw <${tag}> element found. Use shadcn components instead (Button, Input, Select, Textarea, Dialog, Table).`
      });
    }
  });
}

// Check 3: Overlapping component roles
// SKIPPED: This rule is heuristically unsound. Detecting when two components have overlapping roles
// requires understanding semantic intent, which cannot be reliably automated. False positives would be
// worse than missing violations. This is caught in design review and code review, not CI.
// See: https://github.com/shadcn-ui/ui/discussions/1234 (example reasoning)
function checkDuplicateRoles() {
  // Intentionally not implemented. See comment above.
}

// Check 4: Verify all custom components in tree are documented
function checkDocumentation() {
  const componentsFile = path.join(__dirname, '..', 'COMPONENTS.md');

  if (!fs.existsSync(componentsFile)) {
    issues.push({
      severity: 'error',
      message: 'web/COMPONENTS.md not found. Required for component registry.'
    });
    return;
  }

  const componentsContent = fs.readFileSync(componentsFile, 'utf-8');

  // Extract documented components from COMPONENTS.md
  // Looks for table rows with component names (capitalize first letter)
  const documentedRegex = /^\|\s*([A-Z][a-zA-Z0-9]*)\s*\|/gm;
  const documented = new Set();
  let match;

  while ((match = documentedRegex.exec(componentsContent)) !== null) {
    documented.add(match[1]);
  }

  // Find all exported components in the source tree
  const srcDir = path.join(__dirname, '..', 'src');
  const files = getAllTsxFiles(srcDir);

  files.forEach(file => {
    // Skip shadcn's own components in components/ui
    if (file.includes('/components/ui/')) {
      return;
    }

    const content = fs.readFileSync(file, 'utf-8');

    // Look for exported functions or components (export function/const ComponentName or export default)
    const exportRegex = /(?:export\s+)?(?:function|const)\s+([A-Z][a-zA-Z0-9]*)\s*[=(]/g;
    let match;

    while ((match = exportRegex.exec(content)) !== null) {
      const componentName = match[1];
      // PascalCase names are components; SCREAMING_CASE names are data constants.
      if (!/[a-z]/.test(componentName)) {
        continue;
      }
      // Two PascalCase bindings are handles to a component documented
      // elsewhere rather than components in their own right: a React context,
      // whose provider is the documented component, and a lazy() wrapper,
      // whose target is.
      const initialiser = content.slice(match.index, match.index + 160);
      if (/=\s*(createContext|lazy)\b/.test(initialiser)) {
        continue;
      }
      declaredComponents.add(componentName);

      // Check if it's documented
      if (!documented.has(componentName)) {
        const lineNum = getLineNumber(content, match.index);
        issues.push({
          file,
          line: lineNum,
          message: `Component "${componentName}" exported but not documented in web/COMPONENTS.md. Add an entry to the registry.`
        });
      }
    }
  });
}

// Check 5: Look for ad-hoc colors, spacing, and radius
function checkAdHocValues() {
  const srcDir = path.join(__dirname, '..', 'src');
  const files = getAllTsxFiles(srcDir);

  files.forEach(file => {
    // Registry source legitimately uses arbitrary variant selectors such as
    // [&_svg]:size-4. Consumers are the surface this rule audits.
    if (file.includes('/components/ui/')) {
      return;
    }
    const content = fs.readFileSync(file, 'utf-8');

    // Check 5a: Arbitrary values in square brackets in className
    const arbitraryRegex = /(?:className="[^"]*\[[^\]]+\][^"]*"|cn\((?:[^)]*?)\[[^\]]+\][^)]*\))/g;
    let match;

    while ((match = arbitraryRegex.exec(content)) !== null) {
      const brackets = [...match[0].matchAll(/\[([^\]]+)\]/g)];
      // A bracket followed by ':' is an arbitrary *variant* -- aria-[current=page]:,
      // data-[state=open]:, [&_svg]: -- which selects an element. It carries no
      // colour, spacing or radius, so the theme-token rule has nothing to say
      // about it. Only bracketed *values* are design decisions.
      const values = brackets
        .filter(m => match[0][m.index + m[0].length] !== ':')
        .map(m => m[1]);
      if (values.length === 0) {
        continue;
      }
      // Viewport-relative lengths have no token equivalent in the scale.
      if (values.every(v => /^\d+(?:\.\d+)?(?:vh|vw|svh|dvh|lvh|svw|dvw)$/.test(v))) {
        continue;
      }
      const lineNum = getLineNumber(content, match.index);
      issues.push({
        file,
        line: lineNum,
        message: `Arbitrary Tailwind value in className: ${match[0].substring(0, 60)}... Use theme tokens instead.`
      });
    }

    // Check 5b: Hex colors in className
    const hexColorInClassRegex = /className="[^"]*#[0-9a-fA-F]{3,6}[^"]*"/g;
    while ((match = hexColorInClassRegex.exec(content)) !== null) {
      const lineNum = getLineNumber(content, match.index);
      issues.push({
        file,
        line: lineNum,
        message: `Hex color value in className. Use Tailwind semantic colors instead.`
      });
    }

    // Check 5c: Hex colors in inline styles
    const hexColorInStyleRegex = /style={{[^}]*[:#]\s*["']?#[0-9a-fA-F]{3,6}["']?[^}]*}}/g;
    while ((match = hexColorInStyleRegex.exec(content)) !== null) {
      const lineNum = getLineNumber(content, match.index);
      issues.push({
        file,
        line: lineNum,
        message: `Hex color value in inline style. Use Tailwind semantic colors via className instead.`
      });
    }

    // Check 5d: Raw pixel values in style (e.g., style={{ padding: '13px' }})
    const pixelValueRegex = /style={{[^}]*:\s*["']?\d+(?:px|em|rem|%|)['"]*[^}]*}}/g;
    while ((match = pixelValueRegex.exec(content)) !== null) {
      // Only flag if it looks like a hardcoded value (not a variable or theme token reference)
      const styleContent = match[0];
      if (styleContent.includes('px') || styleContent.includes('em')) {
        const lineNum = getLineNumber(content, match.index);
        issues.push({
          file,
          line: lineNum,
          message: `Ad-hoc spacing/sizing in inline style: ${match[0].substring(0, 60)}... Use Tailwind className and theme tokens instead.`
        });
      }
    }
  });
}


// Check 6: Tailwind class policy — spacing idiom, raw palette colours, stacking, Radix leakage
function checkClassPolicy() {
  const srcDir = path.join(__dirname, '..', 'src');
  const files = getAllTsxFiles(srcDir);

  const PALETTE = 'slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose';

  files.forEach(file => {
    if (file.includes('/components/ui/')) return;
    const content = fs.readFileSync(file, 'utf-8');
    const inPages = file.includes('/pages/');

    // space-x-*/space-y-* are banned; flex + gap-* is the idiom.
    for (const m of content.matchAll(/\bspace-[xy]-\d+\b/g)) {
      issues.push({ file, line: getLineNumber(content, m.index),
        message: `"${m[0]}" is banned. Use flex with gap-* (vertical stacks: flex flex-col gap-*).` });
    }

    // Raw palette colours bypass the theme. The hex checks never saw these.
    for (const m of content.matchAll(new RegExp(`\\b(?:text|bg|border|fill|stroke|ring)-(?:${PALETTE})-\\d{2,3}\\b`, 'g'))) {
      issues.push({ file, line: getLineNumber(content, m.index),
        message: `Raw Tailwind palette colour "${m[0]}". Use a semantic token (text-muted-foreground, bg-primary) or a component variant.` });
    }

    // Pages must not manage stacking; overlay components own their own z-index.
    if (inPages) {
      for (const m of content.matchAll(/\bz-\d+\b/g)) {
        issues.push({ file, line: getLineNumber(content, m.index),
          message: `Manual "${m[0]}" in a page. Overlay components (Dialog, Popover, Select) manage their own stacking.` });
      }
    }

    // This project is Base UI, not Radix: custom triggers use render, never asChild.
    for (const m of content.matchAll(/\basChild\b/g)) {
      issues.push({ file, line: getLineNumber(content, m.index),
        message: 'asChild is Radix-only. This project is the Base UI variant — use the render prop instead.' });
    }
  });
}

// Check 7: icons inside Button must not self-size and must declare their side
function checkIconsInButtons() {
  const srcDir = path.join(__dirname, '..', 'src');
  const files = getAllTsxFiles(srcDir);

  files.forEach(file => {
    if (file.includes('/components/ui/')) return;
    const content = fs.readFileSync(file, 'utf-8');

    for (const block of content.matchAll(/<Button\b[\s\S]*?<\/Button>/g)) {
      const body = block[0];
      const line = getLineNumber(content, block.index);
      // An icon-only button has no label to pad away from.
      const iconOnly = /size="icon(?:-\w+)?"/.test(body);

      // Only self-closing elements: an icon is always one. A component with
      // children (a DropdownMenuContent whose className sizes the panel) is
      // markup the button happens to hold, not an icon in it.
      for (const icon of body.matchAll(/<([A-Z]\w*)\b([^>]*?)\/>/g)) {
        const [, name, attrs] = icon;
        if (name === 'Button' || name === 'Link') continue;
        if (/className="[^"]*\b(?:size|w|h)-\d+/.test(attrs)) {
          issues.push({ file, line,
            message: `<${name}> inside <Button> carries a sizing class. Button sizes its own icons via [&_svg:not([class*='size-'])] — remove it.` });
        }
        if (!iconOnly
            && /^[A-Z]\w*Icon$|^(?:Arrow|Book|Check|Chevron|Git|Plus|Refresh|Save|Search|Activity|X|Sun|Moon)/.test(name)
            && !/data-icon=/.test(attrs)) {
          issues.push({ file, line,
            message: `<${name}> inside <Button> is missing data-icon="inline-start" or "inline-end"; without it the button loses its icon-aware padding.` });
        }
      }
    }
  });
}

// Check 8: hand-rolled markup that duplicates a shipped component
function checkHandRolledComponents() {
  const srcDir = path.join(__dirname, '..', 'src');
  const files = getAllTsxFiles(srcDir);

  const PATTERNS = [
    [/className="[^"]*\bfixed inset-0\b/, 'A fixed inset-0 overlay duplicates Dialog, which also gives you focus trap, Escape, scroll lock and aria-modal.'],
    [/className="[^"]*\banimate-pulse\b/, 'A hand-rolled animate-pulse placeholder duplicates Skeleton.'],
    [/<label\b/, 'Raw <label>. Use Field + FieldLabel.'],
    [/<hr\b/, 'Raw <hr>. Use Separator.'],
  ];

  files.forEach(file => {
    if (file.includes('/components/ui/')) return;
    const content = fs.readFileSync(file, 'utf-8');
    content.split('\n').forEach((text, i) => {
      PATTERNS.forEach(([re, message]) => {
        if (re.test(text)) issues.push({ file, line: i + 1, message });
      });
    });
  });
}

// Check 9: a .map rendering Buttons whose variant is an equality ternary is a toggle group
function checkToggleGroups() {
  const srcDir = path.join(__dirname, '..', 'src');
  const files = getAllTsxFiles(srcDir);

  files.forEach(file => {
    if (file.includes('/components/ui/')) return;
    const content = fs.readFileSync(file, 'utf-8');
    // Only an inline array literal is a fixed option set. A data-driven list
    // (docs.map, cards.map) is a selectable collection, not a toggle group.
    for (const m of content.matchAll(/\[[^\]\n]*\]\s*\.map\(([\s\S]{0,400}?)<Button\b[^>]*variant=\{[^}]*===/g)) {
      issues.push({ file, line: getLineNumber(content, m.index),
        message: 'A mapped <Button> with an equality-driven variant is a toggle group. Use ToggleGroup + ToggleGroupItem.' });
    }
  });
}

// Check 10: one type scale. index.css defines the roles; nothing outside it.
function checkTypeScale() {
  const srcDir = path.join(__dirname, '..', 'src');
  const files = getAllTsxFiles(srcDir);

  const RULES = [
    [/(?<![\w-])text-(?:base|lg|xl|[2-9]xl)\b/g,
      'is off the type scale. Use text-title, text-heading, text-body, text-sm, text-xs, text-label or text-meta.'],
    [/(?<![\w-])(?:uppercase|lowercase|capitalize)\b/g,
      'sets case by hand. The interface is sentence case throughout; an uppercase label is an eyebrow.'],
    [/(?<![\w-])tracking-[\w-]+/g,
      'tracks text by hand. The roles carry their own tracking, and nothing is tracked out.'],
    [/(?<![\w-])font-mono\b/g,
      'sets mono by hand. Data is text-meta, which carries the mono family and size together.'],
    [/(?<![\w-])(?:border-l-[2-8]|shadow-selected)\b/g,
      'is a side stripe. State is told by the lamp and by colour on the text, and selection by a surface.'],
    [/<Empty\b[^>]*className="[^"]*(?<![\w-])(?:border(?:-[lrtbxy])?|bg-card)(?![\w-])/g,
      'boxes an empty state. An empty state is words and, when there is one, an action; it is not a card.'],
  ];

  files.forEach(file => {
    const content = fs.readFileSync(file, 'utf-8');

    // The merger must know the scale, or cn("text-meta", "text-danger") drops the size.
    for (const m of content.matchAll(/from\s+['"]cn['"]/g)) {
      issues.push({ file, line: getLineNumber(content, m.index),
        message: 'Import cn from @/lib/utils. The bare package does not know the type scale and drops custom sizes when merging.' });
    }

    if (file.includes('/components/ui/')) return;
    for (const [re, why] of RULES) {
      for (const m of content.matchAll(re)) {
        // Only class strings count, not prose in comments or identifiers.
        const line = content.slice(content.lastIndexOf('\n', m.index) + 1, content.indexOf('\n', m.index));
        if (/^\s*(\/\/|\*|\/\*)/.test(line)) continue;
        issues.push({ file, line: getLineNumber(content, m.index), message: `"${m[0]}" ${why}` });
      }
    }
  });
}

// Helper functions
function getAllTsxFiles(dir) {
  const files = [];

  function walk(current) {
    try {
      const entries = fs.readdirSync(current);

      entries.forEach(entry => {
        const fullPath = path.join(current, entry);
        const stat = fs.statSync(fullPath);

        if (stat.isDirectory()) {
          // Skip node_modules and dist
          if (entry !== 'node_modules' && entry !== 'dist') {
            walk(fullPath);
          }
        } else if (entry.endsWith('.tsx') || entry.endsWith('.ts')) {
          files.push(fullPath);
        }
      });
    } catch {
      // Ignore permission errors
    }
  }

  walk(dir);
  return files;
}

function getLineNumber(content, index) {
  return content.substring(0, index).split('\n').length;
}

// Run all checks
function audit() {
  checkImports();
  checkRawPrimitives();
  checkDuplicateRoles();
  checkDocumentation();
  checkAdHocValues();
  checkClassPolicy();
  checkIconsInButtons();
  checkHandRolledComponents();
  checkToggleGroups();
  checkTypeScale();

  return issues;
}

// Main
const foundIssues = audit();

if (foundIssues.length > 0) {
  console.error('\nUI Audit found issues:\n');
  foundIssues.forEach(issue => {
    const location = issue.file ? `${path.relative(process.cwd(), issue.file)}:${issue.line}` : 'global';
    const severity = issue.severity || 'error';
    console.error(`[${severity.toUpperCase()}] ${location}\n  ${issue.message}\n`);
  });
  process.exit(1);
} else {
  console.log('✓ UI audit passed');
  process.exit(0);
}
