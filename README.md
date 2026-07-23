# aicommits

AI-powered Git commit message generator using an OpenAI-compatible API.

![Go 1.26](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)

## Features

- 🔍 Analyzes your staged `git diff` and generates meaningful commit messages
- 🤖 Uses an OpenAI-compatible API (OpenAI, Ollama, LM Studio, etc.)
- 🎨 Supports multiple commit formats: plain, [conventional commits](https://www.conventionalcommits.org/), [gitmoji](https://gitmoji.dev/), and more
- 📝 Interactive selection from multiple generated messages
- ✏️ Option to edit or write your own message before committing

## Installation

```bash
git clone https://github.com/user/aicommits.git
cd aicommits
go build -o aicommits ./cmd/
sudo mv aicommits /usr/local/bin/
```

## Configuration

Create `~/.aicommits` in your home directory:

```ini
OPENAI_API_KEY=sk-your-api-key-here
OPENAI_MODEL=gpt-4o
```

### All Options

| Key               | Default                     | Description                                                        |
| ----------------- | --------------------------- | ------------------------------------------------------------------ |
| `OPENAI_API_KEY`  | _(required)_                | Your OpenAI-compatible API key                                     |
| `OPENAI_MODEL`    | _(required)_                | Model name (e.g., `gpt-4o`, `gpt-4o-mini`, `llama3`)               |
| `OPENAI_BASE_URL` | `https://api.openai.com/v1` | Custom API base URL (e.g., `http://localhost:11434/v1` for Ollama) |
| `locale`          | `en`                        | Language for commit messages                                       |
| `type`            | `conventional`              | Commit message format                                              |
| `timeout`         | `60000`                     | API request timeout in milliseconds (min: 500)                     |
| `max-length`      | `72`                        | Maximum commit message length in characters (min: 20)              |
| `generate`        | `1`                         | How many messages to generate per run                              |

### Commit Types

| Value                      | Format Example                                        |
| -------------------------- | ----------------------------------------------------- |
| `plain`                    | `add user authentication endpoint`                    |
| `conventional` _(default)_ | `feat: add user authentication endpoint`              |
| `conventional+body`        | `feat: add user authentication endpoint` + body (TBD) |
| `gitmoji`                  | `:sparkles: add user authentication endpoint`         |
| `subject+body`             | `add user authentication endpoint` + body (TBD)       |

### Example: Ollama (local)

```ini
OPENAI_API_KEY=ollama
OPENAI_BASE_URL=http://localhost:11434/v1
OPENAI_MODEL=llama3
locale=en
type=conventional
timeout=60000
```

## Usage

Stage your changes as usual, then run `aicommits`:

```bash
git add .
aicommits
```

You can override the number of messages to generate via flag:

```bash
aicommits -g 3           # generate 3 messages
aicommits --generate 3   # generate 3 messages
```

The tool will:

1. Detect staged files
2. Generate one or more commit messages
3. Present an interactive picker
4. Run `git commit` with your chosen message

```
📁 Detected 2 staged file(s):
     src/auth.go
     src/auth_test.go

🔍 Generating 3 commit messages...

Select a commit message:
  1: feat: implement user authentication with JWT tokens
  2: feat: add login and registration handlers
  3: feat: create auth middleware with token validation
  ✏️  Enter custom message
  ❌ Cancel
```

## License

MIT
