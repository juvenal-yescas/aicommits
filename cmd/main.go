package main

import (
	"aicommits"
	"fmt"
	"os"
)

func main() {
	generate := parseGenerateFlag(os.Args[1:])

	if err := aicommits.RunAicommits(generate); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseGenerateFlag parses -g / --generate from the given argument list.
// Returns 0 if the flag is not present (caller falls back to config value).
func parseGenerateFlag(args []string) int {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		var g int
		if _, err := fmt.Sscanf(arg, "-g=%d", &g); err == nil && g >= 1 {
			return g
		}
		if _, err := fmt.Sscanf(arg, "--generate=%d", &g); err == nil && g >= 1 {
			return g
		}
		if (arg == "-g" || arg == "--generate") && i+1 < len(args) {
			if _, err := fmt.Sscanf(args[i+1], "%d", &g); err == nil && g >= 1 {
				return g
			}
		}
	}
	return 0
}
