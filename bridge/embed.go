// Package bridge embeds the PowerShell bridge scripts into the provider binary.
//
// Embedding means a released binary is self-contained: users only download the
// provider from the Terraform Registry, they never need a copy of this repository.
// At runtime the client extracts these files to a private per-user directory
// (see internal/client) and runs them from there.
package bridge

import "embed"

// Scripts contains every file under bridge/scripts/, keyed as "scripts/<name>.ps1".
//
//go:embed scripts/*.ps1
var Scripts embed.FS

// ScriptsRoot is the directory inside Scripts that holds the .ps1 files.
const ScriptsRoot = "scripts"
