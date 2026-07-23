package aicommits

import (
	"fmt"
	"os"
	"strings"

	"github.com/manifoldco/promptui"
)

// RunAicommits is the entry point for the aicommits command.
// It loads config, gets the staged diff, generates commit messages,
// prompts the user to select one, and runs git commit.
func RunAicommits(generate int) error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}
	if cfg.OpenAIAPIKey == "" {
		return fmt.Errorf("no OPENAI_API_KEY found in ~/.aicommits. Please configure it")
	}

	if err := AssertGitRepo(); err != nil {
		return err
	}

	staged, err := GetStagedDiff()
	if err != nil {
		return fmt.Errorf("error getting staged diff: %w", err)
	}
	if staged == nil || len(staged.Files) == 0 {
		return fmt.Errorf("no staged changes found. Stage your changes manually, or automatically stage all changes with `git add`")
	}

	fmt.Printf("📁 Detected %d staged file(s):\n", len(staged.Files))
	for _, f := range staged.Files {
		fmt.Printf("     %s\n", f)
	}
	fmt.Println()

	n := cfg.Generate
	if generate > 0 {
		n = generate
	}

	if n > 1 {
		fmt.Printf("🔍 Generating %d commit messages...\n", n)
	} else {
		fmt.Println("🔍 Generating commit message...")
	}

	messages, err := GenerateCommitMessages(cfg, staged.Diff, n)
	if err != nil {
		return fmt.Errorf("error generating commit message: %w", err)
	}

	var finalMessage string
	for {
		// Build the selection list: generated messages + regenerate + edit + cancel.
		items := make([]string, 0, len(messages)+3)
		for i, m := range messages {
			items = append(items, fmt.Sprintf("%d: %s", i+1, m))
		}
		items = append(items, "🔄 Regenerate")
		items = append(items, "✏️  Enter custom message")
		items = append(items, "❌ Cancel")

		sel := promptui.Select{
			Label: "Select a commit message",
			Items: items,
			Size:  len(items),
		}
		idx, _, err := sel.Run()
		if err != nil {
			return fmt.Errorf("cancelled")
		}

		switch {
		case idx < len(messages):
			finalMessage = messages[idx]
		case idx == len(messages): // Regenerate
			fmt.Println()
			if n > 1 {
				fmt.Printf("🔍 Regenerating %d commit messages...\n", n)
			} else {
				fmt.Println("🔍 Regenerating commit message...")
			}
			messages, err = GenerateCommitMessages(cfg, staged.Diff, n)
			if err != nil {
				return fmt.Errorf("error regenerating commit message: %w", err)
			}
			continue
		case idx == len(messages)+1: // Edit
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
				return fmt.Errorf("cancelled")
			}
			finalMessage = strings.TrimSpace(edited)
		default: // Cancel
			fmt.Println("Cancelled.")
			os.Exit(0)
		}
		break
	}

	if err := RunGitCommit(finalMessage, false); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	fmt.Println("✅ Committed!")
	return nil
}
