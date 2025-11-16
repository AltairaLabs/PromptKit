#!/bin/bash
# Script to dynamically prepare example READMEs for Astro documentation
# This runs during docs-build and copies example READMEs to the appropriate content collections
#
# Examples are integrated into their respective product collections:
# - /examples -> arena/examples/
# - /sdk/examples -> sdk/examples/

ARENA_OUTPUT="docs/src/content/arena/examples"
SDK_OUTPUT="docs/src/content/sdk/examples"

# Clean up existing example directories
rm -rf "$ARENA_OUTPUT"
rm -rf "$SDK_OUTPUT"
mkdir -p "$ARENA_OUTPUT"
mkdir -p "$SDK_OUTPUT"

# Function to process examples from a directory (Astro version)
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
            
            # Create output file with Astro frontmatter
            output_file="$output_path/${dirname}.md"
            cat > "$output_file" << EOF
---
title: ${title}
description: Example demonstrating ${dirname}
docType: example
order: 100
---

EOF
            # Append original content (without the first heading since it's in title)
            tail -n +2 "$readme_path" >> "$output_file"
            
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
docType: example
order: 99
---

EOF
        tail -n +2 "$source_dir/README.md" >> "$output_file"
        echo "  Processed: ${title} index"
    fi
}

# Process PromptArena examples (top-level /examples) into arena/examples/
echo "Processing PromptArena examples..."
process_examples "examples" "$ARENA_OUTPUT"

# Process SDK examples into sdk/examples/
echo "Processing SDK examples..."
process_examples "sdk/examples" "$SDK_OUTPUT"

echo "✅ Example READMEs prepared for Astro in arena/examples/ and sdk/examples/"
