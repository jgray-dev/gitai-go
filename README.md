# gitai

AI-powered Git commit message generator using Claude Haiku 4.5

## Features

- Automatically generates commit messages using Claude Haiku 4.5
- Parallel processing of multiple modified files
- Enforces git commit message length limits (72 characters)
- Commits each file individually with tailored messages
- Cost tracking for API usage

## Installation

1. Clone or download this repository
2. **Edit `main.go` and add your API key** (around line 26):
   ```go
   var ANTHROPIC_API_KEY = "your-api-key-here"
   ```
   Get your API key from: https://console.anthropic.com/

3. Run the installation script:
   ```bash
   ./run
   ```

The `run` script will:
- Build the binary with your API key embedded
- Install it to `~/.local/bin/gitai`

Make sure `~/.local/bin` is in your PATH. If not, add this to your `~/.bashrc` or `~/.zshrc`:
```bash
export PATH="$HOME/.local/bin:$PATH"
```

## Usage

Navigate to any git repository with modified files and run:

```bash
gitai
```

The tool will:
1. Detect all modified files
2. Generate commit messages in parallel using Claude Haiku 4.5
3. Stage and commit each file individually
4. Display progress and results

## Configuration

Your Anthropic API key is embedded directly in the compiled binary during the build process. To change your API key:

1. Edit `main.go` (line ~26)
2. Update the `ANTHROPIC_API_KEY` variable
3. Run `./run` again to rebuild and reinstall

## Requirements

- Go 1.21 or higher
- Git
- Anthropic API key

## How It Works

1. **Detection**: Runs `git diff --name-only HEAD` to find modified files
2. **Parallel Processing**: Uses goroutines to process multiple files simultaneously
3. **AI Generation**: Sends each file's diff to Claude Haiku 4.5 with strict formatting instructions
4. **Length Enforcement**: Ensures commit messages stay within 72 character limit
5. **Individual Commits**: Stages and commits each file separately with its AI-generated message

## Example Output

```
gitai AI-powered commits

⠹ Crafting messages... [3 remaining]

▸ main.go
  │ Add parallel processing for git diff analysis

▸ README.md
  │ Update documentation with installation instructions

▸ run
  │ Create build and install script

3/3 committed | Cost: $0.0023
```

## License

MIT
