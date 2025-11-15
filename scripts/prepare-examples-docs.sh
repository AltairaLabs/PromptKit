#!/bin/bash
# Script to dynamically add front matter to example READMEs for Jekyll processing
# This runs during docs-build and doesn't modify the source files
#
# To add a new example section:
# 1. Call process_examples with: source_dir, output_subdir, parent_title, grand_parent_title, nav_order
# 2. Add the new output directory to the copy section at the end
# 3. Update Makefile docs-clean to remove the new output directory
# 4. Update docs/_config.yml to include the new output directory

TEMP_DIR="docs/_examples_temp"

# Clean up any existing temp directory
rm -rf "$TEMP_DIR"
mkdir -p "$TEMP_DIR"

# Function to process examples from a directory
# Args: source_dir, output_subdir, parent_title, grand_parent_title, nav_order
process_examples() {
    local source_dir=$1
    local output_subdir=$2
    local parent_title=$3
    local grand_parent_title=$4
    local nav_order=$5
    
    local temp_subdir="$TEMP_DIR/$output_subdir"
    mkdir -p "$temp_subdir"
    
    # Process each example README
    for example_dir in "$source_dir"/*/; do
        if [ -f "${example_dir}README.md" ]; then
            dirname=$(basename "$example_dir")
            readme_path="${example_dir}README.md"
            
            # Skip if already has front matter
            if head -1 "$readme_path" | grep -q "^---"; then
                continue
            fi
            
            # Extract title from first heading
            title=$(grep -m 1 "^# " "$readme_path" | sed 's/^# //' || echo "$dirname")
            
            # Create temp file with front matter
            temp_file="$temp_subdir/${dirname}.md"
            if [ -n "$grand_parent_title" ]; then
                cat > "$temp_file" << EOF
---
layout: default
title: ${dirname}
parent: ${parent_title}
grand_parent: ${grand_parent_title}
---

EOF
            else
                cat > "$temp_file" << EOF
---
layout: default
title: ${dirname}
parent: ${parent_title}
---

EOF
            fi
            # Append original content
            cat "$readme_path" >> "$temp_file"
            
            echo "  Processed: $dirname"
        fi
    done
    
    # Process main examples README if needed
    if [ -f "$source_dir/README.md" ]; then
        if ! head -1 "$source_dir/README.md" | grep -q "^---"; then
            temp_file="$temp_subdir/_index.md"
            if [ -n "$grand_parent_title" ]; then
                cat > "$temp_file" << EOF
---
layout: default
title: ${parent_title}
parent: ${grand_parent_title}
nav_order: ${nav_order}
has_children: true
---

EOF
            else
                cat > "$temp_file" << EOF
---
layout: default
title: ${parent_title}
nav_order: ${nav_order}
has_children: true
---

EOF
            fi
            cat "$source_dir/README.md" >> "$temp_file"
            echo "  Processed: ${parent_title} index"
        fi
    fi
}

# Process PromptArena examples (top-level /examples)
echo "Processing PromptArena examples..."
process_examples "examples" "promptarena-examples" "PromptArena Examples" "PromptArena" "10"

# Process SDK examples
echo "Processing SDK examples..."
process_examples "sdk/examples" "sdk-examples" "SDK Examples" "Guides" "10"

# Copy temp files to docs directory for Jekyll processing
if [ -d "$TEMP_DIR" ] && [ "$(ls -A $TEMP_DIR)" ]; then
    # Copy PromptArena examples
    if [ -d "$TEMP_DIR/promptarena-examples" ]; then
        mkdir -p docs/examples
        cp -r "$TEMP_DIR/promptarena-examples"/* docs/examples/
        # Rename _index.md to index.md
        if [ -f "docs/examples/_index.md" ]; then
            mv docs/examples/_index.md docs/examples/index.md
        fi
    fi
    
    # Copy SDK examples
    if [ -d "$TEMP_DIR/sdk-examples" ]; then
        mkdir -p docs/sdk-examples
        cp -r "$TEMP_DIR/sdk-examples"/* docs/sdk-examples/
        # Rename _index.md to index.md
        if [ -f "docs/sdk-examples/_index.md" ]; then
            mv docs/sdk-examples/_index.md docs/sdk-examples/index.md
        fi
    fi
fi

echo "✅ Example READMEs prepared for Jekyll"
