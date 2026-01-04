#!/bin/bash
# Script to dynamically prepare example READMEs for Astro Starlight documentation
# This runs during docs-build and copies example READMEs to the appropriate content collections
#
# Examples are integrated into their respective product collections:
# - /examples -> docs/arena/examples/
# - /sdk/examples -> docs/sdk/examples/

ARENA_OUTPUT="docs/src/content/docs/arena/examples"
SDK_OUTPUT="docs/src/content/docs/sdk/examples"

# Clean up existing example directories
rm -rf "$ARENA_OUTPUT"
rm -rf "$SDK_OUTPUT"
mkdir -p "$ARENA_OUTPUT"
mkdir -p "$SDK_OUTPUT"

# Function to process examples from a directory (Starlight version)
# Args: source_dir, output_path
process_examples() {
    local source_dir=$1
    local output_path=$2

    mkdir -p "$output_path"

    # Process each example README
    for example_dir in "$source_dir"/*/; do
        if [ -f "${example_dir}README.md" ]; then
            dirname=$(basename "$example_dir")
            readme_path="${example_dir}README.md"

            # Extract title from first heading
            title=$(grep -m 1 "^# " "$readme_path" | sed 's/^# //' || echo "$dirname")
            # Remove the "# " from title
            title=$(echo "$title" | sed 's/^# //')

            # Create output file with Starlight frontmatter
            output_file="$output_path/${dirname}.md"
            cat > "$output_file" << EOF
---
title: ${title}
description: Example demonstrating ${dirname}
sidebar:
  order: 100
---

EOF
            # Append original content (skip first H1 heading since Starlight uses frontmatter title)
            sed '1{/^# /d}' "$readme_path" | sed '1{/^$/d}' >> "$output_file"

            echo "  Processed: $dirname"
        fi
    done

    # Process main examples README if needed
    if [ -f "$source_dir/README.md" ]; then
        output_file="$output_path/index.md"
        # Extract title
        title=$(grep -m 1 "^# " "$source_dir/README.md" | sed 's/^# //' || echo "Examples")

        cat > "$output_file" << EOF
---
title: ${title}
description: Code examples and tutorials
sidebar:
  order: 99
---

EOF
        sed '1{/^# /d}' "$source_dir/README.md" | sed '1{/^$/d}' >> "$output_file"
        echo "  Processed: ${title} index"
    fi
}

# Process PromptArena examples (top-level /examples) into arena/examples/
echo "Processing PromptArena examples..."
process_examples "examples" "$ARENA_OUTPUT"

# Process SDK examples into sdk/examples/
echo "Processing SDK examples..."
process_examples "sdk/examples" "$SDK_OUTPUT"

echo "✅ Example READMEs prepared for Starlight in arena/examples/ and sdk/examples/"
