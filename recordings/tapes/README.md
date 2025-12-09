# VHS Demo Recordings

This directory contains VHS tape files for recording PromptKit demos.

## Quick Start (Docker - Recommended)

```bash
# Build the demo container (includes VHS)
make demo-build

# Record all tapes
make demo-vhs-docker

# Record a single tape
make demo-vhs-docker-single TAPE=01-install
```

## Local Recording (requires VHS installed)

```bash
# Install VHS
brew install vhs

# Record all tapes
make demo-vhs

# Record a single tape  
make demo-vhs-single TAPE=01-install
```

## Interactive Recording

```bash
# Start demo container interactively
make demo-run

# Inside container:
record-all                              # Record all tapes
vhs /recordings/tapes/01-install.tape   # Record single tape
asciinema rec /recordings/my-demo.cast  # Interactive recording
```

## Individual Recordings

| Tape | Description | Duration |
|------|-------------|----------|
| `02-init-project.tape` | Initialize project from template | ~20s |
| `03-config-overview.tape` | Explore full configuration | ~25s |
| `04-config-selfplay.tape` | Deep dive into self-play personas | ~20s |
| `05-run-scenario.tape` | Run a test scenario | ~30s |
| `06-redteam-test.tape` | Red-team security testing | ~35s |
| `07-view-conversation.tape` | View conversation details | ~25s |
| `08-sdk-demo.tape` | Compile with packc & run SDK demo | ~25s |
| `09-social-demo.tape` | **Full demo for social media (MP4)** | ~90s |

## Usage

Record a single tape:

```bash
vhs recordings/tapes/01-install.tape
```

## Output

- **GIFs** → `recordings/gifs/` - For README and docs
- **MP4** → `recordings/videos/` - For LinkedIn, Twitter, etc.

GIFs can be embedded in:

- **README.md** - Auto-plays inline on GitHub
- **Astro Docs** - Use `<img>` tag or MDX component

## Customization

Edit tape files to adjust:

- `Set FontSize` - Text size (14-18 recommended)
- `Set Width/Height` - Terminal dimensions
- `Set Theme` - Color scheme (Dracula, Nord, etc.)
- `Set TypingSpeed` - Typing animation speed
- `Sleep` - Pause duration between commands

## Tips

1. **Keep commands short** - Wrap long commands or use aliases
2. **Use echo for context** - Explain what's happening
3. **Generous sleep times** - Wait for TUI rendering
4. **Test locally first** - Run commands manually before recording
