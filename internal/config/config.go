package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Config models application level configuration.
type Config struct {
	Jira JiraConfig
}

// JiraConfig contains connection details for the Jira instance.
type JiraConfig struct {
	URL      string
	Email    string
	APIToken string
}

// Load reads configuration from the provided path and applies environment overrides.
func Load(path string) (*Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	file, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("open config file: %w", err)
	}
	defer file.Close()

	cfg := Config{}
	if err := parseYAMLSubset(bufio.NewScanner(file), &cfg); err != nil {
		return nil, err
	}

	applyJiraEnvOverrides(&cfg.Jira)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func parseYAMLSubset(scanner *bufio.Scanner, cfg *Config) error {
	const jiraSection = "jira"
	currentSection := ""

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !isIndented(line) {
			// Root level key
			if strings.HasSuffix(trimmed, ":") {
				currentSection = strings.TrimSuffix(trimmed, ":")
				continue
			}
			return fmt.Errorf("unrecognized config line: %q", line)
		}

		if currentSection != jiraSection {
			continue
		}

		key, value, err := splitKeyValue(trimmed)
		if err != nil {
			return fmt.Errorf("invalid jira config line: %q: %w", line, err)
		}
		value = stripQuotes(value)

		switch strings.ToLower(key) {
		case "url":
			cfg.Jira.URL = value
		case "email":
			cfg.Jira.Email = value
		case "api_token":
			cfg.Jira.APIToken = value
		case "token":
			cfg.Jira.APIToken = value
		default:
			return fmt.Errorf("unknown jira config key %q", key)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	return nil
}

func applyJiraEnvOverrides(jira *JiraConfig) {
	if v := strings.TrimSpace(os.Getenv("JIRA_URL")); v != "" {
		jira.URL = v
	}
	if v := strings.TrimSpace(os.Getenv("JIRA_EMAIL")); v != "" {
		jira.Email = v
	}
	if v := strings.TrimSpace(os.Getenv("JIRA_API_TOKEN")); v != "" {
		jira.APIToken = v
	}
	if v := strings.TrimSpace(os.Getenv("JIRA_TOKEN")); v != "" {
		jira.APIToken = v
	}
}

func validate(cfg *Config) error {
	if cfg.Jira.URL == "" {
		return errors.New("jira url is required (cfg/config.yaml or JIRA_URL)")
	}
	if cfg.Jira.Email == "" {
		return errors.New("jira email is required (cfg/config.yaml or JIRA_EMAIL)")
	}
	if cfg.Jira.APIToken == "" {
		return errors.New("jira api token is required (cfg/config.yaml or JIRA_API_TOKEN)")
	}
	return nil
}

func splitKeyValue(line string) (key, value string, err error) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", errors.New("expected key: value pair")
	}
	key = strings.TrimSpace(parts[0])
	value = strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", errors.New("missing key")
	}
	return key, value, nil
}

func stripQuotes(value string) string {
	if len(value) >= 2 {
		first := rune(value[0])
		last := rune(value[len(value)-1])
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return strings.TrimRightFunc(value, unicode.IsSpace)
}

func isIndented(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}
