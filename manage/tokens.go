package manage

const (
	manifestFile    = "plugins.go"
	catGateway      = "gateway"
	catBackend      = "backend"
	catMemory       = "memory"
	catOrchestrator = "orchestrator"
	catExtractor    = "extractor"
	catSkills       = "skills"
	catCompactor    = "compactor"
	kindDiscord     = "discord"
	kindNone        = "none"
	verbGet         = "get"
	verbPin         = "pin"
	verbAdd         = "add"
	verbRemove      = "remove"
	verbUnpin       = "unpin"
)

const (
	exitUsage              = 2
	modeFile               = 0o644
	modeDirPrivate         = 0o700
	modeSecret             = 0o600
	decimalBase            = 10
	semverParts            = 3
	minVersionParts        = 2
	positionalsWithVersion = 2
)
