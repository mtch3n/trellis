#!/usr/bin/env python3
"""
Rewrite 'type' to 'template' in knowledge entry frontmatter.

For each *.md file under a vault directory (including hidden revision directories),
rewrites the leading YAML frontmatter:
  - type: note → removed (no template)
  - type: <x> → template: <x>
  - files with template: keep it and drop type:

Dry run by default (prints changes); use --apply to write changes.
Preserves all other content byte-for-byte.
"""

import argparse
import os
import re
import sys
from pathlib import Path
from typing import Tuple, Optional


def find_markdown_files(vault_dir: str) -> list[Path]:
    """Find all *.md files in vault directory, including hidden revision directories."""
    vault_path = Path(vault_dir)
    md_files = []

    for root, dirs, files in os.walk(vault_path):
        for file in files:
            if file.endswith('.md'):
                md_files.append(Path(root) / file)

    return sorted(md_files)


def extract_frontmatter(content: str) -> Tuple[str, str, str]:
    """
    Extract frontmatter from file content.
    Returns (frontmatter_text, body, full_text_with_separators)
    If no frontmatter, returns empty frontmatter and full content as body.
    """
    if not content.startswith('---\n'):
        return '', content, content

    # Find the closing ---
    lines = content.split('\n')
    if len(lines) < 2:
        return '', content, content

    # Find closing delimiter
    fm_end = -1
    for i in range(1, len(lines)):
        if lines[i] == '---':
            fm_end = i
            break

    if fm_end == -1:
        # No closing delimiter found
        return '', content, content

    fm_text = '\n'.join(lines[1:fm_end])
    body_start = fm_end + 1
    body = '\n'.join(lines[body_start:])

    return fm_text, body, content


def rewrite_frontmatter(fm_text: str) -> Tuple[str, bool]:
    """
    Rewrite frontmatter from 'type' to 'template'.
    Returns (new_frontmatter, was_changed)
    """
    if not fm_text.strip():
        return fm_text, False

    lines = fm_text.split('\n')
    new_lines = []
    changed = False
    has_template = False
    has_type = False

    # First pass: check what we have
    for line in lines:
        if line.strip().startswith('template:'):
            has_template = True
        if line.strip().startswith('type:'):
            has_type = True

    # Second pass: rewrite
    for line in lines:
        # Check if this line defines template
        if line.strip().startswith('template:'):
            new_lines.append(line)
            continue

        # Check if this line defines type
        if line.strip().startswith('type:'):
            # Extract the value
            match = re.match(r'^(\s*)type:\s*(.*)$', line)
            if match:
                indent = match.group(1)
                value = match.group(2).strip()
                changed = True  # Any removal/rewrite of type: is a change

                # Skip if value is 'note' (no template)
                if value == 'note':
                    # Drop this line entirely
                    continue
                else:
                    # Rewrite as template only if no template exists
                    if not has_template:
                        new_lines.append(f'{indent}template: {value}')
                    # else: keep existing template, just drop type
                continue

        new_lines.append(line)

    new_fm = '\n'.join(new_lines)
    return new_fm, changed


def rewrite_file_content(content: str) -> Tuple[str, bool]:
    """
    Rewrite file content from 'type' to 'template'.
    Returns (new_content, was_changed)
    """
    fm_text, body, _ = extract_frontmatter(content)

    if not content.startswith('---\n'):
        # No frontmatter
        return content, False

    new_fm, changed = rewrite_frontmatter(fm_text)

    if not changed:
        return content, False

    # Reconstruct file
    if new_fm.strip():
        new_content = f'---\n{new_fm}\n---\n\n{body}'
    else:
        # If frontmatter is now empty, just body (with leading newlines stripped)
        new_content = body.lstrip('\n')

    return new_content, changed


def process_file(filepath: Path, apply: bool = False) -> bool:
    """
    Process a single markdown file.
    Returns True if file was changed, False otherwise.
    """
    try:
        with open(filepath, 'rb') as f:
            original_bytes = f.read()

        original_content = original_bytes.decode('utf-8')
        new_content, changed = rewrite_file_content(original_content)

        if changed:
            # Show what would change
            print(f'would rewrite {filepath}: type -> template')

            if apply:
                # Write the new content, preserving exact bytes
                with open(filepath, 'w', encoding='utf-8', newline='') as f:
                    f.write(new_content)

        return changed
    except Exception as e:
        print(f'error processing {filepath}: {e}', file=sys.stderr)
        return False


def main():
    parser = argparse.ArgumentParser(
        description='Rewrite type to template in knowledge entry frontmatter'
    )
    parser.add_argument('vault_dir', help='Path to vault directory')
    parser.add_argument('--apply', action='store_true', help='Write changes (default: dry run)')

    args = parser.parse_args()

    if not os.path.isdir(args.vault_dir):
        print(f'error: {args.vault_dir} is not a directory', file=sys.stderr)
        sys.exit(1)

    files = find_markdown_files(args.vault_dir)

    if not files:
        print(f'no markdown files found in {args.vault_dir}')
        return

    changed_count = 0
    for filepath in files:
        if process_file(filepath, args.apply):
            changed_count += 1

    if changed_count > 0:
        if args.apply:
            print(f'\nrewrote {changed_count} file(s)')
        else:
            print(f'\nwould rewrite {changed_count} file(s) (use --apply to write)')
    else:
        print('no changes needed')


if __name__ == '__main__':
    main()
