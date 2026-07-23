package aicommits

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

// chatMessage is a single message in an OpenAI-compatible chat request.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the request body for OpenAI-compatible chat completions.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

// chatResponse is the response body from an OpenAI-compatible chat completions endpoint.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// buildSystemPrompt builds the system prompt for the given configuration.
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
	typeInstr := commitTypeInstructions[cfg.CommitType]

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

// sanitizeMessage cleans up the raw model output: strips think-blocks, quotes,
// XML tags, trailing periods, and takes only the first line.
func sanitizeMessage(msg string) string {
	msg = regexp.MustCompile(`(?is)<think>.*?</think>`).ReplaceAllString(msg, "")
	msg = strings.TrimSpace(msg)
	if idx := strings.Index(msg, "\n"); idx >= 0 {
		msg = msg[:idx]
	}
	msg = regexp.MustCompile(`(\w)\.$`).ReplaceAllString(msg, "$1")
	msg = strings.Trim(msg, "\"'`")
	msg = regexp.MustCompile(`^<[^>]*>\s*`).ReplaceAllString(msg, "")
	return strings.TrimSpace(msg)
}

// GenerateCommitMessage calls the configured LLM API and returns a single commit message.
func GenerateCommitMessage(cfg *Config, diff string) (string, error) {
	const maxDiff = 30000
	if len(diff) > maxDiff {
		diff = diff[:maxDiff] + "\n\n[Diff truncated due to size]"
	}

	baseURL := strings.TrimSuffix(cfg.OpenAIBaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	body, err := json.Marshal(chatRequest{
		Model: cfg.OpenAIModel,
		Messages: []chatMessage{
			{Role: "system", Content: buildSystemPrompt(cfg)},
			{Role: "user", Content: diff},
		},
		Temperature: 0.4,
		MaxTokens:   2000,
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: cfg.Timeout}
	req, err := http.NewRequest("POST", baseURL+"/chat/completions", bytes.NewReader(body))
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
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, respBytes)
	}

	var cr chatResponse
	if err := json.Unmarshal(respBytes, &cr); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("no response from API")
	}

	return sanitizeMessage(cr.Choices[0].Message.Content), nil
}

// GenerateCommitMessages calls GenerateCommitMessage n times in parallel and
// returns all successful results. At least one must succeed.
func GenerateCommitMessages(cfg *Config, diff string, n int) ([]string, error) {
	results := make([]string, n)
	errs := make([]error, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = GenerateCommitMessage(cfg, diff)
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
