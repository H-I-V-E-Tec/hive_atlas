package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/H-I-V-E-Tec/hive_atlas/internal/transport"
)

func setup(client, path string) error {
	command := os.Getenv("HIVE_LAUNCHER")
	args := []string{"atlas"}
	if command == "" {
		return fmt.Errorf("run setup through the installed hive launcher (HIVE_LAUNCHER required)")
	}
	if !filepath.IsAbs(command) {
		return fmt.Errorf("launcher path must be absolute")
	}
	center, err := centerURL()
	if err != nil {
		return err
	}
	endpoint := os.Getenv("HIVE_ATLAS_URL")
	if endpoint == "" {
		endpoint = center + "/atlas"
	}
	endpoint, err = transport.ValidateURL(endpoint)
	if err != nil {
		return err
	}
	settings := map[string]string{"HIVE_CENTER_URL": center, "HIVE_ATLAS_URL": endpoint, "HIVE_HOME": hiveHome()}
	if path := os.Getenv("HIVE_TOKEN_FILE"); path != "" {
		settings["HIVE_TOKEN_FILE"] = path
	}
	home, _ := os.UserHomeDir()
	if path == "" {
		switch client {
		case "codex":
			path = filepath.Join(home, ".codex", "config.toml")
		case "claude-code":
			path = filepath.Join(home, ".claude.json")
		case "claude-desktop":
			return fmt.Errorf("provide --config with Claude Desktop's config path")
		default:
			return fmt.Errorf("unknown agent")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if client == "codex" {
		text := string(data)
		section := "[mcp_servers.hive_atlas]"
		// Preserve all other tables and replace only the Atlas table.
		lines := strings.Split(text, "\n")
		out := []string{}
		inside := false
		for _, line := range lines {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "[") {
				if trim == section || strings.HasPrefix(trim, "[mcp_servers.hive_atlas.") {
					inside = true
					continue
				}
				inside = false
			}
			if !inside {
				out = append(out, line)
			}
		}
		text = strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n\n" + section + "\ncommand = " + strconv.Quote(command) + "\nargs = [\"atlas\"]\n"
		text += "[mcp_servers.hive_atlas.env]\n"
		keys := []string{}
		for key := range settings {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			text += key + " = " + strconv.Quote(settings[key]) + "\n"
		}
		return privateWrite(path, []byte(text))
	}
	if client != "claude-code" && client != "claude-desktop" {
		return fmt.Errorf("unknown agent")
	}
	config := map[string]any{}
	if len(data) > 0 && json.Unmarshal(data, &config) != nil {
		return fmt.Errorf("invalid agent JSON config")
	}
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		if config["mcpServers"] != nil {
			return fmt.Errorf("invalid mcpServers config")
		}
		servers = map[string]any{}
		config["mcpServers"] = servers
	}
	servers["hive_atlas"] = map[string]any{"command": command, "args": args, "env": settings}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return privateWrite(path, append(encoded, '\n'))
}
