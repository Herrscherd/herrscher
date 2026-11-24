package clitext

import contracts "github.com/Herrscherd/herrscher-contracts"

const (
	forgeSourceSaved      = "cli.forge.source.saved"
	forgeSourceApp        = "cli.forge.source.app"
	forgeSourceEnv        = "cli.forge.source.env"
	forgeSourceCLI        = "cli.forge.source.cli"
	forgeSourceUnknown    = "cli.forge.source.unknown"
	forgePermissionNone   = "cli.forge.permission.none"
	forgePermissionRead   = "cli.forge.permission.read"
	forgePermissionTriage = "cli.forge.permission.triage"
	forgePermissionWrite  = "cli.forge.permission.write"
	forgePermissionMaint  = "cli.forge.permission.maintain"
	forgePermissionAdmin  = "cli.forge.permission.admin"
	forgePermissionUnkn   = "cli.forge.permission.unknown"
)

func forgeKeys() map[Key][]string {
	return map[Key][]string{
		forgeSourceSaved:      nil,
		forgeSourceApp:        nil,
		forgeSourceEnv:        nil,
		forgeSourceCLI:        nil,
		forgeSourceUnknown:    nil,
		forgePermissionNone:   nil,
		forgePermissionRead:   nil,
		forgePermissionTriage: nil,
		forgePermissionWrite:  nil,
		forgePermissionMaint:  nil,
		forgePermissionAdmin:  nil,
		forgePermissionUnkn:   nil,
	}
}

// ForgeSourceKey returns the key of the word that names source in a sentence.
func ForgeSourceKey(source contracts.TokenSource) Key {
	switch source {
	case contracts.TokenSourceSaved:
		return forgeSourceSaved
	case contracts.TokenSourceApp:
		return forgeSourceApp
	case contracts.TokenSourceEnv:
		return forgeSourceEnv
	case contracts.TokenSourceCLI:
		return forgeSourceCLI
	}
	return forgeSourceUnknown
}

// ForgePermissionKey returns the key of the word that names permission in a sentence.
func ForgePermissionKey(permission contracts.RepoPermission) Key {
	switch permission {
	case contracts.PermissionNone:
		return forgePermissionNone
	case contracts.PermissionRead:
		return forgePermissionRead
	case contracts.PermissionTriage:
		return forgePermissionTriage
	case contracts.PermissionWrite:
		return forgePermissionWrite
	case contracts.PermissionMaintain:
		return forgePermissionMaint
	case contracts.PermissionAdmin:
		return forgePermissionAdmin
	}
	return forgePermissionUnkn
}
