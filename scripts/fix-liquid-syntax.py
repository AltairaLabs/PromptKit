#!/usr/bin/env python3
"""
Fix Liquid syntax errors in Jekyll markdown files by wrapping Go template
and GitHub Actions syntax in {% raw %} tags.
"""

import re
import sys
from pathlib import Path

def fix_file(filepath):
    """Fix Liquid syntax errors in a single file."""
    with open(filepath, 'r', encoding='utf-8') as f:
        content = f.read()
    
    original = content
    changes = []
    
    # Pattern 1: Fix code blocks containing {{.xxx}} Go template syntax
    # Match code blocks and wrap them if they contain {{.
    def wrap_go_templates(match):
        block = match.group(0)
        if '{{.' in block or '{{if' in block or '{{range' in block or '{{end}}' in block:
            # Check if already wrapped
            if '{% raw %}' not in block:
                lang = match.group(1) if match.group(1) else ''
                code = match.group(2)
                return f'{% raw %}\n```{lang}\n{code}```\n{% endraw %}'
        return block
    
    # Pattern for code blocks
    content = re.sub(r'```(\w*)\n(.*?)```', wrap_go_templates, content, flags=re.DOTALL)
    
    # Pattern 2: Fix inline {{.xxx}} that aren't in code blocks
    # This catches things like: Output: "Hello, {{.name}}!"
    lines = content.split('\n')
    for i, line in enumerate(lines):
        # Skip if line is already in a raw block or is a code fence
        if '{% raw %}' in line or '{% endraw %}' in line or line.strip().startswith('```'):
            continue
        # If line contains {{. and isn't in a code block
        if '{{.' in line and '```' not in line:
            # Wrap just the {{.xxx}} part
            lines[i] = re.sub(r'(\{\{\.[\w-]+\}\})', r'{% raw %}\1{% endraw %}', line)
            if lines[i] != line:
                changes.append(f"Line {i+1}: Wrapped Go template syntax")
    
    content = '\n'.join(lines)
    
    # Pattern 3: Fix GitHub Actions {{xxx}} syntax (not starting with a dot)
    # Match ${{ ... }} patterns
    content = re.sub(
        r'(\$\{\{[^}]+\}\})',
        r'{% raw %}\1{% endraw %}',
        content
    )
    
    # Pattern 4: Fix Go struct literals {{Key: value}}
    content = re.sub(
        r'(\{\{[A-Z]\w*:\s*[^}]+\}\})',
        r'{% raw %}\1{% endraw %}',
        content
    )
    
    if content != original:
        with open(filepath, 'w', encoding='utf-8') as f:
            f.write(content)
        return True
    return False

def main():
    docs_dir = Path(__file__).parent.parent / 'docs'
    
    # Files to fix based on error output
    files_to_fix = [
        'concepts/templates.md',
        'concepts/prompts.md',
        'packc/explanation/compilation.md',
        'packc/explanation/pack-format.md',
        'packc/how-to/compile-packs.md',
        'packc/reference/compile-prompt.md',
        'runtime/how-to/manage-state.md',
        'runtime/explanation/middleware-design.md',
        'runtime/explanation/state-management.md',
        'workflows/deployment-workflow.md',
        'workflows/development-workflow.md',
    ]
    
    fixed_count = 0
    for file_path in files_to_fix:
        full_path = docs_dir / file_path
        if full_path.exists():
            print(f"Processing {file_path}...")
            if fix_file(full_path):
                fixed_count += 1
                print(f"  ✓ Fixed")
            else:
                print(f"  - No changes needed")
        else:
            print(f"  ✗ File not found: {full_path}")
    
    print(f"\nFixed {fixed_count} files")

if __name__ == '__main__':
    main()
