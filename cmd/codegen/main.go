package main

import (
	"os"

	"github.com/zerochirou/codegen"
	"github.com/zerochirou/codegen/internal/cli"
)

func main() {
	os.Exit(cli.Execute(codegen.BundledSkills, os.Args[1:]))
}
