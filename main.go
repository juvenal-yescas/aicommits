package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/manifoldco/promptui"
	"gopkg.in/ini.v1"
)

// Config holds the parsed ~/.aicommits configuration
type Config struct {
	OpenAIAPIKey  string
	OpenAIBaseURL string
	OpenAIModel   string
	Locale        string
	CommitType    string
	Timeout       time.Duration
	MaxLength     int
	Generate      int
}

// ChatMessage for OpenAI-compatible API
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest for OpenAI-compatible API
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

// ChatResponse from OpenAI-compatible API
type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func loadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(home, ".aicommits")

	cfg := &Config{
		Locale:     "en",
		CommitType: "conventional",
		Timeout:    60 * time.Second,
		MaxLength:  72,
		Generate:   1,
	}

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		return cfg, nil
	}

	f, err := ini.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	sec := f.Section("")
	if k, err := sec.GetKey("OPENAI_API_KEY"); err == nil {
		cfg.OpenAIAPIKey = k.String()
	}
	if k, err := sec.GetKey("OPENAI_BASE_URL"); err == nil {
		cfg.OpenAIBaseURL = k.String()
	}
	if k, err := sec.GetKey("OPENAI_MODEL"); err == nil {
		cfg.OpenAIModel = k.String()
	}
	if k, err := sec.GetKey("locale"); err == nil {
		cfg.Locale = k.String()
	}
	if k, err := sec.GetKey("type"); err == nil {
		cfg.CommitType = k.String()
	}
	if k, err := sec.GetKey("timeout"); err == nil {
		ms, err := k.Int64()
		if err == nil && ms >= 500 {
			cfg.Timeout = time.Duration(ms) * time.Millisecond
		}
	}
	if k, err := sec.GetKey("max-length"); err == nil {
		n, err := k.Int()
		if err == nil && n >= 20 {
			cfg.MaxLength = n
		}
	}
	if k, err := sec.GetKey("generate"); err == nil {
		n, err := k.Int()
		if err == nil && n >= 1 {
			cfg.Generate = n
		}
	}

	return cfg, nil
}

func assertGitRepo() error {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the current directory must be a Git repository")
	}
	return nil
}

type StagedDiff struct {
	Files []string
	Diff  string
}

func isLockFile(file string) bool {
	lockPatterns := []string{"package-lock.json", "pnpm-lock.yaml"}
	for _, p := range lockPatterns {
		if filepath.Base(file) == p {
			return true
		}
	}
	return strings.HasSuffix(file, ".lock")
}

func getStagedDiff() (*StagedDiff, error) {
	// Get all staged files
	cmd := exec.Command("git", "diff", "--cached", "--diff-algorithm=minimal", "--name-only")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}

	allFiles := []string{}
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f != "" {
			allFiles = append(allFiles, f)
		}
	}

	hasNonLock := false
	for _, f := range allFiles {
		if !isLockFile(f) {
			hasNonLock = true
			break
		}
	}

	files := allFiles
	args := []string{"diff", "--cached", "--diff-algorithm=minimal"}
	if hasNonLock {
		files = []string{}
		for _, f := range allFiles {
			if !isLockFile(f) {
				files = append(files, f)
			}
		}
		args = append(args, ":(exclude)package-lock.json", ":(exclude)pnpm-lock.yaml", ":(exclude)*.lock")
	}

	diffCmd := exec.Command("git", args...)
	diffOut, err := diffCmd.Output()
	if err != nil {
		return nil, err
	}

	return &StagedDiff{
		Files: files,
		Diff:  string(diffOut),
	}, nil
}

func buildSystemPrompt(cfg *Config) string {
	commitTypeFormats := map[string]string{
		"plain":             "<commit message>",
		"conventional":      "<type>[optional (<scope>)]: <commit message>\nThe commit message subject must start with a lowercase letter",
		"conventional+body": "<type>[optional (<scope>)]: <commit message subject>\nThe commit message subject must start with a lowercase letter",
		"gitmoji":           ":emoji: <commit message>",
		"subject+body":      "<commit message subject>",
	}

	conventionalTypePrompt := `Choose a type from the type-to-description JSON below that best describes the git diff. IMPORTANT: The type MUST be lowercase (e.g., "feat", not "Feat" or "FEAT"):
{
  "docs": "Documentation only changes",
  "style": "Changes that do not affect the meaning of the code (white-space, formatting, missing semi-colons, etc)",
  "refactor": "A code change that improves code structure without changing functionality (renaming, restructuring classes/methods, extracting functions, etc)",
  "perf": "A code change that improves performance",
  "test": "Adding missing tests or correcting existing tests",
  "build": "Changes that affect the build system or external dependencies",
  "ci": "Changes to our CI configuration files and scripts",
  "chore": "Other changes that don't modify src or test files",
  "revert": "Reverts a previous commit",
  "feat": "A new feature",
  "fix": "A bug fix"
}`

	commitTypeInstructions := map[string]string{
		"plain":             "",
		"conventional":      conventionalTypePrompt,
		"conventional+body": conventionalTypePrompt + "\nOutput only the conventional commit subject line; the body is generated separately.",
		"gitmoji":           "Choose an appropriate gitmoji emoji from https://gitmoji.dev/ that best describes the git diff.",
		"subject+body":      "Output only the subject line; the body is generated separately.",
	}

	format, ok := commitTypeFormats[cfg.CommitType]
	if !ok {
		format = commitTypeFormats["plain"]
	}
	typeInstr, ok := commitTypeInstructions[cfg.CommitType]
	if !ok {
		typeInstr = ""
	}

	parts := []string{
		"Generate a concise git commit message title in present tense that precisely describes the key changes in the following code diff. Focus on what was changed, not just file names. Provide only the title, no description or body.",
		fmt.Sprintf("Message language: %s", cfg.Locale),
		fmt.Sprintf("Commit message must be a maximum of %d characters.", cfg.MaxLength),
		"Exclude anything unnecessary such as translation. Your entire response will be passed directly into git commit.",
		fmt.Sprintf("IMPORTANT: Do not include any explanations, introductions, or additional text. Do not wrap the commit message in quotes or any other formatting. The commit message must not exceed %d characters. Respond with ONLY the commit message text.", cfg.MaxLength),
		"Be specific: include concrete details (package names, versions, functionality) rather than generic statements.",
	}
	if typeInstr != "" {
		parts = append(parts, typeInstr)
	}
	parts = append(parts, fmt.Sprintf("The output response must be in format:\n%s", format))

	return strings.Join(parts, "\n")
}

func sanitizeMessage(msg string) string {
	// Remove <think>...</think> blocks
	re := regexp.MustCompile(`(?is)<think>.*?</think>`)
	msg = re.ReplaceAllString(msg, "")
	msg = strings.TrimSpace(msg)
	// Take first line only
	if idx := strings.Index(msg, "\n"); idx >= 0 {
		msg = msg[:idx]
	}
	// Remove trailing period from last word
	re2 := regexp.MustCompile(`(\w)\.$`)
	msg = re2.ReplaceAllString(msg, "$1")
	// Remove surrounding quotes/backticks
	msg = strings.Trim(msg, `"'\`+"`")
	// Remove leading XML tags
	re3 := regexp.MustCompile(`^<[^>]*>\s*`)
	msg = re3.ReplaceAllString(msg, "")
	return strings.TrimSpace(msg)
}

func generateCommitMessage(cfg *Config, diff string) (string, error) {
	maxDiff := 30000
	if len(diff) > maxDiff {
		diff = diff[:maxDiff] + "\n\n[Diff truncated due to size]"
	}

	baseURL := cfg.OpenAIBaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	reqBody := ChatRequest{
		Model: cfg.OpenAIModel,
		Messages: []ChatMessage{
			{Role: "system", Content: buildSystemPrompt(cfg)},
			{Role: "user", Content: diff},
		},
		Temperature: 0.4,
		MaxTokens:   2000,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: cfg.Timeout}
	req, err := http.NewRequest("POST", baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAIAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBytes))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response from API")
	}

	return sanitizeMessage(chatResp.Choices[0].Message.Content), nil
}

func generateCommitMessages(cfg *Config, diff string, n int) ([]string, error) {
	results := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = generateCommitMessage(cfg, diff)
		}(i)
	}
	wg.Wait()

	var messages []string
	var firstErr error
	for i, m := range results {
		if errs[i] != nil {
			if firstErr == nil {
				firstErr = errs[i]
			}
		} else if m != "" {
			messages = append(messages, m)
		}
	}
	if len(messages) == 0 {
		return nil, firstErr
	}
	return messages, nil
}

func runGitCommit(message string, noVerify bool) error {
	args := []string{"commit", "-m", message}
	if noVerify {
		args = append(args, "--no-verify")
	}
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg.OpenAIAPIKey == "" {
		fmt.Fprintln(os.Stderr, "No OPENAI_API_KEY found in ~/.aicommits. Please configure it.")
		os.Exit(1)
	}

	if err := assertGitRepo(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	staged, err := getStagedDiff()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting staged diff: %v\n", err)
		os.Exit(1)
	}
	if staged == nil || len(staged.Files) == 0 {
		fmt.Fprintln(os.Stderr, "No staged changes found. Stage your changes manually, or automatically stage all changes with `git add`.")
		os.Exit(1)
	}

	fmt.Printf("📁 Detected %d staged file(s):\n", len(staged.Files))
	for _, f := range staged.Files {
		fmt.Printf("     %s\n", f)
	}
	fmt.Println()

	n := cfg.Generate
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var g int
		// handle -g=3 or --generate=3
		if _, err := fmt.Sscanf(arg, "-g=%d", &g); err == nil && g >= 1 {
			n = g
		} else if _, err := fmt.Sscanf(arg, "--generate=%d", &g); err == nil && g >= 1 {
			n = g
			// handle -g 3 or --generate 3
		} else if (arg == "-g" || arg == "--generate") && i+1 < len(args) {
			if _, err := fmt.Sscanf(args[i+1], "%d", &g); err == nil && g >= 1 {
				n = g
				i++
			}
		}
	}

	if n > 1 {
		fmt.Printf("🔍 Generating %d commit messages...\n", n)
	} else {
		fmt.Println("🔍 Generating commit message...")
	}

	messages, err := generateCommitMessages(cfg, staged.Diff, n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating commit message: %v\n", err)
		os.Exit(1)
	}

	// Build select items: all messages + edit + cancel
	items := make([]string, 0, len(messages)+2)
	for i, m := range messages {
		items = append(items, fmt.Sprintf("%d: %s", i+1, m))
	}
	items = append(items, "✏️  Enter custom message")
	items = append(items, "❌ Cancel")

	selPr := promptui.Select{
		Label: "Select a commit message",
		Items: items,
		Size:  len(items),
	}
	idx, _, err := selPr.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cancelled.")
		os.Exit(1)
	}

	var finalMessage string
	switch {
	case idx < len(messages): // picked a generated message
		finalMessage = messages[idx]
	case idx == len(messages): // Edit
		defaultMsg := ""
		if len(messages) > 0 {
			defaultMsg = messages[0]
		}
		editPrompt := promptui.Prompt{
			Label:   "Commit message",
			Default: defaultMsg,
		}
		edited, err := editPrompt.Run()
		if err != nil || strings.TrimSpace(edited) == "" {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			os.Exit(1)
		}
		finalMessage = strings.TrimSpace(edited)
	default: // Cancel
		fmt.Println("Cancelled.")
		os.Exit(0)
	}

	if err := runGitCommit(finalMessage, false); err != nil {
		fmt.Fprintf(os.Stderr, "git commit failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Committed!")
}
