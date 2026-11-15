package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zerochirou/codegen/internal/chroma"
)

// Status describes the sync state of a skill or rule relative to the source bundle.
type Status string

const (
	StatusNew      Status = "new"
	StatusAdded    Status = "added"
	StatusOutdated Status = "outdated"
	StatusUpdated  Status = "updated"
	StatusUpToDate Status = "up-to-date"
	StatusCustom   Status = "custom"
)

// TargetOptions configures where skills and rules are resolved.
type TargetOptions struct {
	Global      bool
	ProjectPath string
	Cwd         string
	HomeDir     string
}

// Target holds the resolved filesystem destinations for skills and rules.
type Target struct {
	Scope     string
	RootDir   string
	SkillsDir string
	RulesDir  string
}

// SyncOptions configures Inspect and Sync operations.
type SyncOptions struct {
	Target       Target
	SkillsFS     fs.FS
	FilterNames  []string
	IncludeRules bool
	DryRun       bool
}

// ItemChange records the state of a single skill or rule.
type ItemChange struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
}

// SyncResult reports all skill and rule states after Inspect or Sync.
type SyncResult struct {
	Scope          string       `json:"scope"`
	RootDir        string       `json:"root_dir"`
	SkillsDir      string       `json:"skills_dir"`
	RulesDir       string       `json:"rules_dir,omitempty"`
	DryRun         bool         `json:"dry_run,omitempty"`
	Skills         []ItemChange `json:"skills"`
	Rules          []ItemChange `json:"rules,omitempty"`
	RulePaths      []string     `json:"-"`
	CustomSkills   []string     `json:"custom_skills,omitempty"`
	AddedCount     int          `json:"added"`
	UpdatedCount   int          `json:"updated"`
	UnchangedCount int          `json:"up_to_date"`
	TotalBundled   int          `json:"total_bundled"`
}

// BundledRule holds a bundled markdown rule file from .agents/rules.
type BundledRule struct {
	Name string
	Data []byte
}

type bundledFile struct {
	relPath string
	data    []byte
}

type bundledSkill struct {
	name  string
	files []bundledFile
}

// ResolveTarget resolves the project or global skills and rules directories.
func ResolveTarget(opts TargetOptions) (Target, error) {
	if opts.Global && strings.TrimSpace(opts.ProjectPath) != "" {
		return Target{}, errors.New("--global and --path cannot be used together")
	}

	homeDir := strings.TrimSpace(opts.HomeDir)
	if homeDir == "" {
		homeDir, _ = os.UserHomeDir()
	}

	if opts.Global {
		if homeDir == "" {
			return Target{}, errors.New("cannot resolve user home directory for --global")
		}
		absHome, err := filepath.Abs(chroma.ExpandPath(homeDir))
		if err == nil {
			homeDir = absHome
		}
		return Target{
			Scope:     "global",
			RootDir:   homeDir,
			SkillsDir: filepath.Join(homeDir, ".gemini", "config", "skills"),
			RulesDir:  filepath.Join(homeDir, ".gemini", "config", "plugins", "codegen-rules"),
		}, nil
	}

	cwd := strings.TrimSpace(opts.Cwd)
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			cwd = "."
		}
	}

	if strings.TrimSpace(opts.ProjectPath) != "" {
		root, err := resolveExplicitProjectPath(opts.ProjectPath, cwd)
		if err != nil {
			return Target{}, err
		}
		return projectTarget(root), nil
	}

	root, err := FindProjectRoot(cwd, homeDir)
	if err != nil {
		return Target{}, err
	}
	return projectTarget(root), nil
}

func projectTarget(root string) Target {
	return Target{
		Scope:     "project",
		RootDir:   root,
		SkillsDir: filepath.Join(root, ".agents", "skills"),
		RulesDir:  filepath.Join(root, ".agents", "rules"),
	}
}

func resolveExplicitProjectPath(rawPath, cwd string) (string, error) {
	expanded := chroma.ExpandPath(rawPath)
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(cwd, expanded)
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve project path %q: %w", rawPath, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project path %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project path %s is not a directory", abs)
	}
	return abs, nil
}

// FindProjectRoot walks upward from startDir (stopping before homeDir and at .git)
// looking for existing codegen/agent project markers, falling back to the git root or startDir.
func FindProjectRoot(startDir, homeDir string) (string, error) {
	absStart, err := filepath.Abs(chroma.ExpandPath(startDir))
	if err != nil {
		return "", fmt.Errorf("resolve directory %q: %w", startDir, err)
	}

	absHome := ""
	if strings.TrimSpace(homeDir) != "" {
		if h, err := filepath.Abs(chroma.ExpandPath(homeDir)); err == nil {
			absHome = h
		}
	}

	dir := absStart
	gitRoot := ""
	for {
		if absHome != "" && samePath(dir, absHome) {
			break
		}
		if hasProjectMarker(dir) {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			gitRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if gitRoot != "" {
		return gitRoot, nil
	}
	if absHome != "" && samePath(absStart, absHome) {
		return "", fmt.Errorf("current directory is home (%s); run inside a project directory, pass --path, or use --global", absHome)
	}
	return absStart, nil
}

func hasProjectMarker(dir string) bool {
	markers := []string{
		filepath.Join(dir, ".codegen", "config.json"),
		filepath.Join(dir, ".agents", "skills"),
		filepath.Join(dir, ".agents", "rules"),
		filepath.Join(dir, ".gemini", "mcp_config.json"),
		filepath.Join(dir, ".mcp.json"),
	}
	for _, m := range markers {
		if _, err := os.Stat(m); err == nil {
			return true
		}
	}
	return false
}

func samePath(a, b string) bool {
	ca := filepath.Clean(a)
	cb := filepath.Clean(b)
	if ca == cb {
		return true
	}
	ia, errA := os.Stat(ca)
	ib, errB := os.Stat(cb)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

// Sync installs or updates bundled skills and rules into Target (or previews when DryRun is true).
func Sync(opts SyncOptions) (SyncResult, error) {
	allSkills, err := discoverBundledSkills(opts.SkillsFS)
	if err != nil {
		return SyncResult{}, err
	}

	selectedSkills, err := filterBundledSkills(allSkills, opts.FilterNames)
	if err != nil {
		return SyncResult{}, err
	}

	res := SyncResult{
		Scope:        opts.Target.Scope,
		RootDir:      opts.Target.RootDir,
		SkillsDir:    opts.Target.SkillsDir,
		RulesDir:     opts.Target.RulesDir,
		DryRun:       opts.DryRun,
		TotalBundled: len(selectedSkills),
	}

	if !opts.DryRun && len(selectedSkills) > 0 {
		if err := os.MkdirAll(opts.Target.SkillsDir, 0o755); err != nil {
			return SyncResult{}, fmt.Errorf("create skills directory %s: %w", opts.Target.SkillsDir, err)
		}
	}

	for _, sk := range selectedSkills {
		change, err := syncBundledSkill(sk, opts.Target.SkillsDir, opts.DryRun)
		if err != nil {
			return res, err
		}
		res.Skills = append(res.Skills, change)
		switch change.Status {
		case StatusNew, StatusAdded:
			res.AddedCount++
		case StatusOutdated, StatusUpdated:
			res.UpdatedCount++
		case StatusUpToDate:
			res.UnchangedCount++
		}
	}

	custom, err := discoverCustomSkills(opts.Target.SkillsDir, allSkills)
	if err != nil {
		return res, err
	}
	res.CustomSkills = custom

	if opts.IncludeRules && len(opts.FilterNames) == 0 {
		changes, paths, err := syncRules(opts.SkillsFS, opts.Target, opts.DryRun)
		if err != nil {
			return res, err
		}
		res.Rules = changes
		res.RulePaths = paths
	}

	return res, nil
}

// Inspect checks the target directory against the source bundle without modifying files.
func Inspect(opts SyncOptions) (SyncResult, error) {
	opts.DryRun = true
	return Sync(opts)
}

func discoverBundledSkills(skillsFS fs.FS) ([]bundledSkill, error) {
	if skillsFS == nil {
		return nil, nil
	}
	const root = ".agents/skills"
	entries, err := fs.ReadDir(skillsFS, root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read bundled skills: %w", err)
	}

	var skills []bundledSkill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillRoot := root + "/" + entry.Name()
		info, err := fs.Stat(skillsFS, skillRoot+"/SKILL.md")
		if err != nil || info.IsDir() {
			continue
		}

		files, err := collectSkillFiles(skillsFS, skillRoot)
		if err != nil {
			return nil, err
		}
		skills = append(skills, bundledSkill{
			name:  entry.Name(),
			files: files,
		})
	}

	slices.SortFunc(skills, func(a, b bundledSkill) int {
		return strings.Compare(a.name, b.name)
	})
	return skills, nil
}

func collectSkillFiles(fsys fs.FS, skillRoot string) ([]bundledFile, error) {
	var files []bundledFile
	err := fs.WalkDir(fsys, skillRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, skillRoot+"/")
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		files = append(files, bundledFile{relPath: rel, data: data})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk bundled skill %s: %w", skillRoot, err)
	}
	return files, nil
}

func filterBundledSkills(all []bundledSkill, names []string) ([]bundledSkill, error) {
	if len(names) == 0 {
		return all, nil
	}
	byName := make(map[string]bundledSkill, len(all))
	for _, sk := range all {
		byName[sk.name] = sk
	}

	seen := make(map[string]bool, len(names))
	selected := make([]bundledSkill, 0, len(names))
	var missing []string
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		sk, ok := byName[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		selected = append(selected, sk)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("unknown bundled skill(s): %s (run 'codegen skills list' to view available skills)", strings.Join(missing, ", "))
	}
	return selected, nil
}

func syncBundledSkill(sk bundledSkill, skillsDir string, dryRun bool) (ItemChange, error) {
	dstRoot := filepath.Join(skillsDir, sk.name)
	info, err := os.Stat(dstRoot)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ItemChange{}, fmt.Errorf("stat skill directory %s: %w", dstRoot, err)
	}
	isNew := errors.Is(err, fs.ErrNotExist)
	if !isNew && !info.IsDir() {
		return ItemChange{}, fmt.Errorf("skill path %s exists and is not a directory", dstRoot)
	}

	type pendingWrite struct {
		dst  string
		data []byte
	}
	var writes []pendingWrite
	for _, f := range sk.files {
		dst := filepath.Join(dstRoot, filepath.FromSlash(f.relPath))
		existing, err := os.ReadFile(dst)
		if err == nil && bytes.Equal(existing, f.data) {
			continue
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return ItemChange{}, fmt.Errorf("read existing skill file %s: %w", dst, err)
		}
		writes = append(writes, pendingWrite{dst: dst, data: f.data})
	}

	if !dryRun {
		for _, w := range writes {
			if err := os.MkdirAll(filepath.Dir(w.dst), 0o755); err != nil {
				return ItemChange{}, err
			}
			if err := os.WriteFile(w.dst, w.data, 0o644); err != nil {
				return ItemChange{}, err
			}
		}
	}

	status := StatusUpToDate
	if isNew {
		status = StatusNew
		if !dryRun {
			status = StatusAdded
		}
	} else if len(writes) > 0 {
		status = StatusOutdated
		if !dryRun {
			status = StatusUpdated
		}
	}
	return ItemChange{Name: sk.name, Status: status}, nil
}

func discoverCustomSkills(skillsDir string, bundled []bundledSkill) ([]string, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read installed skills %s: %w", skillsDir, err)
	}

	bundledSet := make(map[string]bool, len(bundled))
	for _, sk := range bundled {
		bundledSet[sk.name] = true
	}

	var custom []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || bundledSet[name] {
			continue
		}
		skillDir := filepath.Join(skillsDir, name)
		info, err := os.Stat(skillDir)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err == nil {
			custom = append(custom, name)
		}
	}
	slices.Sort(custom)
	return custom, nil
}

// ReadBundledRules reads all markdown rule files from .agents/rules in skillsFS.
func ReadBundledRules(skillsFS fs.FS) ([]BundledRule, error) {
	if skillsFS == nil {
		return nil, nil
	}
	const root = ".agents/rules"
	entries, err := fs.ReadDir(skillsFS, root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read bundled rules: %w", err)
	}
	var rules []BundledRule
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := fs.ReadFile(skillsFS, root+"/"+entry.Name())
		if err != nil {
			return nil, err
		}
		rules = append(rules, BundledRule{Name: entry.Name(), Data: data})
	}
	slices.SortFunc(rules, func(a, b BundledRule) int {
		return strings.Compare(a.Name, b.Name)
	})
	return rules, nil
}

func syncRules(skillsFS fs.FS, target Target, dryRun bool) ([]ItemChange, []string, error) {
	rules, err := ReadBundledRules(skillsFS)
	if err != nil || len(rules) == 0 {
		return nil, nil, err
	}

	if target.Scope == "global" {
		return syncGlobalRulesPlugin(rules, target.RulesDir, dryRun)
	}

	changes := make([]ItemChange, 0, len(rules))
	paths := make([]string, 0, len(rules))
	for _, r := range rules {
		dst := filepath.Join(target.RulesDir, r.Name)
		existed, changed, err := syncManagedFile(dst, r.Data, dryRun)
		if err != nil {
			return changes, paths, err
		}
		changes = append(changes, ItemChange{
			Name:   r.Name,
			Status: resolveFileStatus(existed, changed, dryRun),
		})
		paths = append(paths, dst)
	}
	return changes, paths, nil
}

func syncGlobalRulesPlugin(rules []BundledRule, pluginDir string, dryRun bool) ([]ItemChange, []string, error) {
	sections := make([]string, 0, len(rules))
	for _, r := range rules {
		if body := strings.TrimSpace(StripYAMLFrontmatter(string(r.Data))); body != "" {
			sections = append(sections, body)
		}
	}
	if len(sections) == 0 {
		return nil, nil, nil
	}

	manifestBytes, err := json.MarshalIndent(map[string]any{
		"name":        "codegen-rules",
		"description": "Always-on session rules bundled with codegen (antislop).",
	}, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	manifestBytes = append(manifestBytes, '\n')

	manifestPath := filepath.Join(pluginDir, "plugin.json")
	agentsMDPath := filepath.Join(pluginDir, "rules", "AGENTS.md")
	agentsData := []byte(strings.Join(sections, "\n\n") + "\n")

	mExisted, mChanged, err := syncManagedFile(manifestPath, manifestBytes, dryRun)
	if err != nil {
		return nil, nil, err
	}
	aExisted, aChanged, err := syncManagedFile(agentsMDPath, agentsData, dryRun)
	if err != nil {
		return nil, nil, err
	}

	existed := mExisted && aExisted
	changed := mChanged || aChanged
	return []ItemChange{
			{
				Name:   "codegen-rules (AGENTS.md)",
				Status: resolveFileStatus(existed, changed, dryRun),
			},
		},
		[]string{agentsMDPath},
		nil
}

func syncManagedFile(dst string, data []byte, dryRun bool) (existed bool, changed bool, err error) {
	existing, err := os.ReadFile(dst)
	if err == nil {
		if bytes.Equal(existing, data) {
			return true, false, nil
		}
		if !dryRun {
			if err := os.WriteFile(dst, data, 0o644); err != nil {
				return true, true, err
			}
		}
		return true, true, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, false, err
	}
	if !dryRun {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return false, true, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return false, true, err
		}
	}
	return false, true, nil
}

func resolveFileStatus(existed, changed, dryRun bool) Status {
	if !existed {
		if dryRun {
			return StatusNew
		}
		return StatusAdded
	}
	if changed {
		if dryRun {
			return StatusOutdated
		}
		return StatusUpdated
	}
	return StatusUpToDate
}

// StripYAMLFrontmatter removes a leading YAML frontmatter block if present.
func StripYAMLFrontmatter(md string) string {
	trimmed := strings.TrimPrefix(md, "\ufeff")
	if !strings.HasPrefix(trimmed, "---\n") && !strings.HasPrefix(trimmed, "---\r\n") {
		return md
	}
	rest := strings.TrimPrefix(trimmed, "---\r\n")
	if rest == trimmed {
		rest = strings.TrimPrefix(trimmed, "---\n")
	}
	if idx := strings.Index(rest, "\n---\n"); idx >= 0 {
		return rest[idx+5:]
	}
	if idx := strings.Index(rest, "\r\n---\r\n"); idx >= 0 {
		return rest[idx+7:]
	}
	return md
}
