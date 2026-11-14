package chroma

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultCollection is the fallback ChromaDB collection name.
	DefaultCollection = "default"
)

// ProjectConfig stores per-project codegen-memory settings in .codegen/config.json.
type ProjectConfig struct {
	ProjectName string `json:"project_name,omitempty"`
	PersistDir  string `json:"persist_dir"`
}

// GlobalConfig stores machine-wide codegen-memory settings in ~/.codegen/config.json.
type GlobalConfig struct {
	GlobalPersistDir string `json:"global_persist_dir"`
}

// ExpandPath expands a leading "~/" to the user's home directory and cleans the path.
func ExpandPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if trimmed == "~" || strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if trimmed == "~" {
				return home
			}
			return filepath.Clean(filepath.Join(home, trimmed[2:]))
		}
	}
	return filepath.Clean(trimmed)
}

// DefaultGlobalPersistDir returns ~/.codegen/chroma_data.
func DefaultGlobalPersistDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Clean("./.codegen/chroma_data")
	}
	return filepath.Join(home, ".codegen", "chroma_data")
}

// GlobalConfigPath returns ~/.codegen/config.json.
func GlobalConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Clean("./.codegen/config.json")
	}
	return filepath.Join(home, ".codegen", "config.json")
}

// ResolveGlobalPersistDir resolves the global ChromaDB storage directory.
func ResolveGlobalPersistDir() string {
	if env := strings.TrimSpace(os.Getenv("CODEGEN_GLOBAL_CHROMA_DIR")); env != "" {
		return absOrExpand(env, "")
	}
	if data, err := os.ReadFile(GlobalConfigPath()); err == nil {
		var cfg GlobalConfig
		if json.Unmarshal(data, &cfg) == nil && strings.TrimSpace(cfg.GlobalPersistDir) != "" {
			return absOrExpand(cfg.GlobalPersistDir, filepath.Dir(GlobalConfigPath()))
		}
	}
	return DefaultGlobalPersistDir()
}

// ResolvePersistDir resolves the ChromaDB directory for CLI/TUI usage.
// When isGlobal is true, it always uses the global database.
// When isGlobal is false, it strictly resolves the current workspace/project database.
func ResolvePersistDir(isGlobal bool) string {
	if isGlobal {
		return ResolveGlobalPersistDir()
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	if env := strings.TrimSpace(os.Getenv("CHROMA_PERSIST_DIR")); env != "" {
		return absOrExpand(env, cwd)
	}

	if dir, ok := FindProjectPersistDir(cwd); ok {
		return dir
	}

	return filepath.Join(cwd, "chroma_data")
}

// ResolveMCPPersistDir resolves the ChromaDB directory for the MCP server.
// Priority:
//  1. Explicit CHROMA_PERSIST_DIR environment variable
//  2. Project-local config found from workspaceRoot (if provided)
//  3. Project-local config found from current working directory
//  4. Global ChromaDB directory (~/.codegen/chroma_data)
func ResolveMCPPersistDir(workspaceRoot string) string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	if env := strings.TrimSpace(os.Getenv("CHROMA_PERSIST_DIR")); env != "" {
		base := cwd
		if workspaceRoot != "" {
			base = workspaceRoot
		}
		return absOrExpand(env, base)
	}

	if workspaceRoot != "" {
		if dir, ok := FindProjectPersistDir(workspaceRoot); ok {
			return dir
		}
	}

	if dir, ok := FindProjectPersistDir(cwd); ok {
		return dir
	}

	return ResolveGlobalPersistDir()
}

// FindProjectPersistDir walks upward from startDir looking for project-scoped codegen configuration
// or an existing chroma_data directory. It stops before the user's home directory so global configs
// in ~/.gemini are never mistaken for project configs.
func FindProjectPersistDir(startDir string) (string, bool) {
	if strings.TrimSpace(startDir) == "" {
		return "", false
	}
	absStart, err := filepath.Abs(ExpandPath(startDir))
	if err != nil {
		return "", false
	}

	home, _ := os.UserHomeDir()
	dir := absStart
	for {
		if home != "" && filepath.Clean(dir) == filepath.Clean(home) {
			break
		}

		if found, ok := readProjectDirConfig(dir); ok {
			return found, true
		}

		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", false
}

func readProjectDirConfig(projectRoot string) (string, bool) {
	// 1. .codegen/config.json (canonical Go CLI config)
	codegenCfgPath := filepath.Join(projectRoot, ".codegen", "config.json")
	if data, err := os.ReadFile(codegenCfgPath); err == nil {
		var cfg ProjectConfig
		if json.Unmarshal(data, &cfg) == nil && strings.TrimSpace(cfg.PersistDir) != "" {
			return absOrExpand(cfg.PersistDir, projectRoot), true
		}
	}

	// 2. .agents/plugins/codegen-memory/mcp_config.json (Antigravity workspace plugin)
	pluginMCP := filepath.Join(projectRoot, ".agents", "plugins", "codegen-memory", "mcp_config.json")
	if dir, ok := readMCPConfigPersistDir(pluginMCP, projectRoot); ok {
		return dir, true
	}

	// 3. .gemini/mcp_config.json (legacy / Gemini / compatibility)
	geminiMCP := filepath.Join(projectRoot, ".gemini", "mcp_config.json")
	if dir, ok := readMCPConfigPersistDir(geminiMCP, projectRoot); ok {
		return dir, true
	}

	// 4. .mcp.json (Claude Code project MCP config)
	claudeMCP := filepath.Join(projectRoot, ".mcp.json")
	if dir, ok := readMCPConfigPersistDir(claudeMCP, projectRoot); ok {
		return dir, true
	}

	// 5. .env file containing CHROMA_PERSIST_DIR
	if val, ok := readDotEnvValue(filepath.Join(projectRoot, ".env"), "CHROMA_PERSIST_DIR"); ok && val != "" {
		return absOrExpand(val, projectRoot), true
	}

	// 6. Existing chroma_data/ directory in projectRoot
	chromaDir := filepath.Join(projectRoot, "chroma_data")
	if info, err := os.Stat(chromaDir); err == nil && info.IsDir() {
		return chromaDir, true
	}

	return "", false
}

func readMCPConfigPersistDir(configPath, projectRoot string) (string, bool) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", false
	}
	var parsed struct {
		MCPServers map[string]struct {
			Env map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", false
	}
	srv, ok := parsed.MCPServers["codegen-memory"]
	if !ok {
		return "", false
	}
	raw := strings.TrimSpace(srv.Env["CHROMA_PERSIST_DIR"])
	if raw == "" {
		return "", false
	}
	return absOrExpand(raw, projectRoot), true
}

func readDotEnvValue(envPath, key string) (string, bool) {
	f, err := os.Open(envPath)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(k) != key {
			continue
		}
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		return v, true
	}
	return "", false
}

func absOrExpand(raw, baseDir string) string {
	expanded := ExpandPath(raw)
	if filepath.IsAbs(expanded) || baseDir == "" {
		abs, err := filepath.Abs(expanded)
		if err == nil {
			return abs
		}
		return expanded
	}
	return filepath.Clean(filepath.Join(baseDir, expanded))
}

// DefaultCollectionName returns CHROMA_COLLECTION_NAME or "default".
func DefaultCollectionName() string {
	if env := strings.TrimSpace(os.Getenv("CHROMA_COLLECTION_NAME")); env != "" {
		return env
	}
	if cwd, err := os.Getwd(); err == nil {
		if val, ok := readDotEnvValue(filepath.Join(cwd, ".env"), "CHROMA_COLLECTION_NAME"); ok && val != "" {
			return val
		}
	}
	return DefaultCollection
}

// WriteProjectConfig saves <projectDir>/.codegen/config.json.
func WriteProjectConfig(projectDir, projectName, persistDir string) error {
	cfgPath := filepath.Join(projectDir, ".codegen", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}
	absPersist := absOrExpand(persistDir, projectDir)
	data, err := json.MarshalIndent(ProjectConfig{
		ProjectName: projectName,
		PersistDir:  absPersist,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, append(data, '\n'), 0o644)
}

// WriteGlobalConfig saves ~/.codegen/config.json.
func WriteGlobalConfig(persistDir string) error {
	cfgPath := GlobalConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}
	absPersist := absOrExpand(persistDir, "")
	data, err := json.MarshalIndent(GlobalConfig{
		GlobalPersistDir: absPersist,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, append(data, '\n'), 0o644)
}
