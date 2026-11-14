package codegen

import "embed"

// BundledSkills embeds all workflow and memory skills bundled with codegen.
//
//go:embed all:.agents/skills
var BundledSkills embed.FS
