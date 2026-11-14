package initwizard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/term"

	"github.com/zerochirou/codegen/internal/chroma"
)

const (
	ansiCyan  = "\033[36m"
	ansiGreen = "\033[32m"
	ansiDim   = "\033[2m"
	ansiBold  = "\033[1m"
	ansiReset = "\033[0m"
)

// Options controls interactive or non-interactive execution of `codegen init`.
type Options struct {
	Scope         string // "", "project", or "global"
	ProjectName   string
	StoragePath   string
	InstallSkills *bool
	Yes           bool
	SkillsFS      fs.FS
	In            io.Reader
	Out           io.Writer
}

// Result reports what `codegen init` wrote.
type Result struct {
	Scope           string
	ProjectName     string
	ChromaDir       string
	ConfigPaths     []string
	SkillsDir       string
	SkillsInstalled int
}

// Run executes the Clack-style interactive setup wizard (or non-interactive if Yes/flags are set).
func Run(opts Options) (Result, error) {
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	reader := bufio.NewReader(in)

	fmt.Fprintf(out, "\n%s┌%s  %sLet's configure codegen%s\n", ansiCyan, ansiReset, ansiBold, ansiReset)
	fmt.Fprintf(out, "%s│%s\n", ansiDim, ansiReset)

	isProject := true
	switch strings.ToLower(strings.TrimSpace(opts.Scope)) {
	case "project":
		isProject = true
	case "global":
		isProject = false
	default:
		if opts.Yes {
			isProject = true
		} else {
			choice := promptSelect(in, out, "Install scope:", []string{
				"Per-project (.codegen/ + .agents/ + .gemini/)",
				"Global (~/.codegen/ + ~/.gemini/config/)",
			})
			isProject = strings.HasPrefix(choice, "Per-project")
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	home, _ := os.UserHomeDir()

	var projectName string
	var defaultBase string
	if isProject {
		defaultName := filepath.Base(cwd)
		if opts.ProjectName != "" {
			projectName = opts.ProjectName
		} else if opts.Yes {
			projectName = defaultName
		} else {
			projectName = promptText(reader, out, "Project name:", defaultName)
		}
		defaultBase = cwd
	} else {
		projectName = "global"
		defaultBase = filepath.Join(home, ".codegen")
	}

	rawStorage := strings.TrimSpace(opts.StoragePath)
	if rawStorage == "" {
		if opts.Yes {
			rawStorage = defaultBase
		} else {
			rawStorage = promptText(reader, out, "ChromaDB storage path:", defaultBase)
		}
	}
	chromaDir := NormalizeChromaDir(rawStorage, cwd)

	installSkills := true
	if opts.InstallSkills != nil {
		installSkills = *opts.InstallSkills
	} else if !opts.Yes {
		installSkills = promptSelect(in, out, "Install agent skills?", []string{"Yes", "No"}) == "Yes"
	}

	var skillsDir string
	if isProject {
		skillsDir = filepath.Join(cwd, ".agents", "skills")
	} else {
		skillsDir = filepath.Join(home, ".gemini", "config", "skills")
	}

	scopeLabel := "Per-project"
	if !isProject {
		scopeLabel = "Global"
	}

	fmt.Fprintf(out, "%s│%s\n", ansiDim, ansiReset)
	fmt.Fprintf(out, "%s●%s  %sAbout to configure:%s\n", ansiGreen, ansiReset, ansiBold, ansiReset)
	fmt.Fprintf(out, "%s│%s    Scope:        %s\n", ansiDim, ansiReset, scopeLabel)
	fmt.Fprintf(out, "%s│%s    Project:      %s\n", ansiDim, ansiReset, projectName)
	fmt.Fprintf(out, "%s│%s    ChromaDB:     %s\n", ansiDim, ansiReset, chromaDir)
	if installSkills {
		fmt.Fprintf(out, "%s│%s    Agent skills: yes (%s)\n", ansiDim, ansiReset, skillsDir)
	} else {
		fmt.Fprintf(out, "%s│%s    Agent skills: no\n", ansiDim, ansiReset)
	}
	fmt.Fprintf(out, "%s│%s\n", ansiDim, ansiReset)

	if !opts.Yes {
		if promptSelect(in, out, "Continue with these settings?", []string{"Yes", "No"}) != "Yes" {
			fmt.Fprintf(out, "%s└%s  Cancelled.\n\n", ansiDim, ansiReset)
			return Result{}, nil
		}
	}

	res, err := Apply(ApplyConfig{
		IsProject:     isProject,
		ProjectDir:    cwd,
		HomeDir:       home,
		ProjectName:   projectName,
		ChromaDir:     chromaDir,
		InstallSkills: installSkills,
		SkillsFS:      opts.SkillsFS,
	})
	if err != nil {
		return Result{}, err
	}

	fmt.Fprintf(
		out,
		"%s└%s  Done! ChromaDB ready at %s (%d skills ready).\n",
		ansiGreen,
		ansiReset,
		res.ChromaDir,
		res.SkillsInstalled,
	)
	for _, p := range res.ConfigPaths {
		fmt.Fprintf(out, "   Config: %s\n", p)
	}
	fmt.Fprintln(out)
	return res, nil
}

// ApplyConfig defines the resolved targets for `codegen init`.
type ApplyConfig struct {
	IsProject     bool
	ProjectDir    string
	HomeDir       string
	ProjectName   string
	ChromaDir     string
	InstallSkills bool
	SkillsFS      fs.FS
}

// Apply creates the ChromaDB folder, writes MCP & project/global configs, and installs skills.
func Apply(cfg ApplyConfig) (Result, error) {
	chromaDir := NormalizeChromaDir(cfg.ChromaDir, cfg.ProjectDir)
	if err := os.MkdirAll(chromaDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create chroma directory %s: %w", chromaDir, err)
	}

	res := Result{
		ProjectName: cfg.ProjectName,
		ChromaDir:   chromaDir,
	}

	if cfg.IsProject {
		res.Scope = "project"
		if err := chroma.WriteProjectConfig(cfg.ProjectDir, cfg.ProjectName, chromaDir); err != nil {
			return Result{}, err
		}
		res.ConfigPaths = append(res.ConfigPaths, filepath.Join(cfg.ProjectDir, ".codegen", "config.json"))

		geminiMCP := filepath.Join(cfg.ProjectDir, ".gemini", "mcp_config.json")
		if err := writeMCPConfig(geminiMCP, map[string]any{
			"command": "codegen",
			"args":    []string{"run"},
			"env": map[string]string{
				"CHROMA_PERSIST_DIR": chromaDir,
			},
		}); err != nil {
			return Result{}, err
		}
		res.ConfigPaths = append(res.ConfigPaths, geminiMCP)

		// Also create an Antigravity workspace plugin when global dynamic MCP isn't already set.
		globalMCPPath := filepath.Join(cfg.HomeDir, ".gemini", "config", "mcp_config.json")
		if !hasDynamicGlobalMCP(globalMCPPath) {
			pluginDir := filepath.Join(cfg.ProjectDir, ".agents", "plugins", "codegen-memory")
			if err := writeJSONFile(filepath.Join(pluginDir, "plugin.json"), map[string]any{
				"name": "codegen-memory",
			}); err != nil {
				return Result{}, err
			}
			pluginMCP := filepath.Join(pluginDir, "mcp_config.json")
			if err := writeMCPConfig(pluginMCP, map[string]any{
				"command": "codegen",
				"args":    []string{"run"},
				"env": map[string]string{
					"CHROMA_PERSIST_DIR": chromaDir,
				},
			}); err != nil {
				return Result{}, err
			}
			res.ConfigPaths = append(res.ConfigPaths, pluginMCP)
		}

		if cfg.InstallSkills {
			skillsDir := filepath.Join(cfg.ProjectDir, ".agents", "skills")
			count, err := installBundledSkills(cfg.SkillsFS, skillsDir)
			if err != nil {
				return Result{}, err
			}
			res.SkillsDir = skillsDir
			res.SkillsInstalled = count
		}
		return res, nil
	}

	// Global scope
	res.Scope = "global"
	if err := chroma.WriteGlobalConfig(chromaDir); err != nil {
		return Result{}, err
	}
	res.ConfigPaths = append(res.ConfigPaths, chroma.GlobalConfigPath())

	// Register global MCP without hardcoding CHROMA_PERSIST_DIR so `codegen run`
	// dynamically routes to per-project DB when run inside initialized workspaces,
	// falling back to global_persist_dir (~/.codegen/chroma_data) otherwise.
	globalEntry := map[string]any{
		"command":  "codegen",
		"args":     []string{"run"},
		"disabled": false,
	}

	globalMCP := filepath.Join(cfg.HomeDir, ".gemini", "config", "mcp_config.json")
	if err := writeMCPConfig(globalMCP, globalEntry); err != nil {
		return Result{}, err
	}
	res.ConfigPaths = append(res.ConfigPaths, globalMCP)

	agyIDEDir := filepath.Join(cfg.HomeDir, ".gemini", "antigravity")
	if info, err := os.Stat(agyIDEDir); err == nil && info.IsDir() {
		ideMCP := filepath.Join(agyIDEDir, "mcp_config.json")
		if err := writeMCPConfig(ideMCP, globalEntry); err == nil {
			res.ConfigPaths = append(res.ConfigPaths, ideMCP)
		}
	}

	geminiSettings := filepath.Join(cfg.HomeDir, ".gemini", "settings.json")
	if _, err := os.Stat(geminiSettings); err == nil {
		_ = updateExistingMCPConfig(geminiSettings, map[string]any{
			"command": "codegen",
			"args":    []string{"run"},
		})
	}

	if cfg.InstallSkills {
		skillsDir := filepath.Join(cfg.HomeDir, ".gemini", "config", "skills")
		count, err := installBundledSkills(cfg.SkillsFS, skillsDir)
		if err != nil {
			return Result{}, err
		}
		res.SkillsDir = skillsDir
		res.SkillsInstalled = count
	}
	return res, nil
}

// NormalizeChromaDir resolves rawInput relative to baseDir and appends "chroma_data"
// if the final path segment is not already "chroma_data".
func NormalizeChromaDir(rawInput, baseDir string) string {
	expanded := chroma.ExpandPath(rawInput)
	if expanded == "" {
		expanded = baseDir
	}
	if !filepath.IsAbs(expanded) && baseDir != "" {
		expanded = filepath.Join(baseDir, expanded)
	}
	abs, err := filepath.Abs(expanded)
	if err == nil {
		expanded = abs
	}
	if filepath.Base(expanded) != "chroma_data" {
		expanded = filepath.Join(expanded, "chroma_data")
	}
	return filepath.Clean(expanded)
}

func hasDynamicGlobalMCP(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	var parsed struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return false
	}
	srv, ok := parsed.MCPServers["codegen-memory"]
	if !ok {
		return false
	}
	return filepath.Base(srv.Command) == "codegen" && slices.Contains(srv.Args, "run") && strings.TrimSpace(srv.Env["CHROMA_PERSIST_DIR"]) == ""
}

func writeMCPConfig(configPath string, entry map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	config := map[string]any{}
	if data, err := os.ReadFile(configPath); err == nil {
		_ = json.Unmarshal(data, &config)
	}
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok || servers == nil {
		servers = map[string]any{}
	}
	servers["codegen-memory"] = entry
	config["mcpServers"] = servers
	return writeJSONFile(configPath, config)
}

func updateExistingMCPConfig(configPath string, entry map[string]any) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	config := map[string]any{}
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	servers, ok := config["mcpServers"].(map[string]any)
	if !ok || servers == nil {
		return nil
	}
	if _, exists := servers["codegen-memory"]; !exists {
		return nil
	}
	servers["codegen-memory"] = entry
	config["mcpServers"] = servers
	return writeJSONFile(configPath, config)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func installBundledSkills(skillsFS fs.FS, destDir string) (int, error) {
	if skillsFS == nil {
		return 0, nil
	}
	const root = ".agents/skills"
	entries, err := fs.ReadDir(skillsFS, root)
	if err != nil {
		return 0, fmt.Errorf("read bundled skills: %w", err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, err
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillRoot := root + "/" + entry.Name()
		if _, err := fs.Stat(skillsFS, skillRoot+"/SKILL.md"); err != nil {
			continue
		}
		targetRoot := filepath.Join(destDir, entry.Name())
		err := fs.WalkDir(skillsFS, skillRoot, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel := strings.TrimPrefix(p, skillRoot)
			rel = strings.TrimPrefix(rel, "/")
			dst := targetRoot
			if rel != "" {
				dst = filepath.Join(targetRoot, filepath.FromSlash(rel))
			}
			if d.IsDir() {
				return os.MkdirAll(dst, 0o755)
			}
			data, err := fs.ReadFile(skillsFS, p)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, 0o644)
		})
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func promptText(reader *bufio.Reader, out io.Writer, label, defaultVal string) string {
	fmt.Fprintf(out, "%s◇%s  %s%s%s\n", ansiCyan, ansiReset, ansiBold, label, ansiReset)
	if defaultVal != "" {
		fmt.Fprintf(out, "%s│%s   %s(%s)%s ", ansiDim, ansiReset, ansiDim, defaultVal, ansiReset)
	} else {
		fmt.Fprintf(out, "%s│%s   ", ansiDim, ansiReset)
	}
	line, _ := reader.ReadString('\n')
	val := strings.TrimSpace(line)
	if val == "" {
		return defaultVal
	}
	return val
}

func promptSelect(in io.Reader, out io.Writer, label string, options []string) string {
	f, isFile := in.(*os.File)
	if !isFile || !term.IsTerminal(int(f.Fd())) {
		return options[0]
	}

	fd := int(f.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return options[0]
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	idx := 0
	fmt.Fprintf(out, "%s◇%s  %s%s%s %s(↑/↓, Enter)%s\r\n", ansiCyan, ansiReset, ansiBold, label, ansiReset, ansiDim, ansiReset)
	renderOptions := func() {
		for i, opt := range options {
			marker := fmt.Sprintf("%s○%s", ansiDim, ansiReset)
			text := fmt.Sprintf("%s%s%s", ansiDim, opt, ansiReset)
			if i == idx {
				marker = fmt.Sprintf("%s●%s", ansiGreen, ansiReset)
				text = fmt.Sprintf("%s%s%s", ansiBold, opt, ansiReset)
			}
			fmt.Fprintf(out, "\033[2K%s│%s  %s %s\r\n", ansiDim, ansiReset, marker, text)
		}
	}
	renderOptions()

	buf := make([]byte, 3)
	for {
		n, err := f.Read(buf[:1])
		if err != nil || n == 0 {
			break
		}
		b := buf[0]
		switch {
		case b == 3: // Ctrl+C
			_ = term.Restore(fd, oldState)
			fmt.Fprintln(out)
			os.Exit(130)
		case b == '\r' || b == '\n' || b == ' ':
			return options[idx]
		case b == 'k' || b == 'K':
			idx = (idx - 1 + len(options)) % len(options)
		case b == 'j' || b == 'J':
			idx = (idx + 1) % len(options)
		case b == 0x1b:
			n2, _ := f.Read(buf[1:3])
			if n2 == 2 && buf[1] == '[' {
				if buf[2] == 'A' {
					idx = (idx - 1 + len(options)) % len(options)
				} else if buf[2] == 'B' {
					idx = (idx + 1) % len(options)
				}
			}
		default:
			continue
		}
		fmt.Fprintf(out, "\033[%dA", len(options))
		renderOptions()
	}
	return options[idx]
}
