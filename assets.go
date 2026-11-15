package codegen

import "embed"

// BundledSkills embeds all workflow, memory, PR, and always-on antislop assets bundled with codegen.
//
//go:embed all:.agents/skills all:.agents/rules
var BundledSkills embed.FS
