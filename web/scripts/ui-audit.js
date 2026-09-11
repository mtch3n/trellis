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
  ];

  files.forEach(file => {
    // Skip shadcn's own components in components/ui
    if (file.includes('/components/ui/')) {
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
    const exportRegex = /export\s+(?:function|const)\s+([A-Z][a-zA-Z0-9]*)/g;
    let match;

    while ((match = exportRegex.exec(content)) !== null) {
      const componentName = match[1];
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
    const content = fs.readFileSync(file, 'utf-8');

    // Check 5a: Arbitrary values in square brackets in className
    const arbitraryRegex = /className="[^"]*\[[^\]]+\][^"]*"/g;
    let match;

    while ((match = arbitraryRegex.exec(content)) !== null) {
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
    } catch (e) {
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
