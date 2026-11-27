package clitext

// Keys of the replies of the host discover command.
const (
	DiscoverIntro              = "cli.discover.intro"
	DiscoverNone               = "cli.discover.none"
	DiscoverCandidate          = "cli.discover.candidate"
	DiscoverAddHint            = "cli.discover.add_hint"
	DiscoverUnverified         = "cli.discover.unverified"
	DiscoverFingerprint        = "cli.discover.fingerprint"
	DiscoverFingerprintUnknown = "cli.discover.fingerprint_unknown"
	DiscoverSourceUnavailable  = "cli.discover.source_unavailable"
	DiscoverAddUnsafe          = "cli.discover.add_unsafe"
)

// Params of the discover replies, named as the catalog fields that carry them.
const (
	ParamAddress     = "Address"
	ParamRoute       = "Route"
	ParamTrust       = "Trust"
	ParamFingerprint = "Fingerprint"
)

func discoverKeys() map[Key][]string {
	return map[Key][]string{
		DiscoverIntro:              nil,
		DiscoverNone:               nil,
		DiscoverCandidate:          {ParamName, ParamAddress, ParamRoute, ParamTrust, ParamFingerprint},
		DiscoverAddHint:            {ParamCommand},
		DiscoverUnverified:         nil,
		DiscoverFingerprint:        {ParamFingerprint},
		DiscoverFingerprintUnknown: nil,
		DiscoverSourceUnavailable:  {ParamSource},
		DiscoverAddUnsafe:          nil,
	}
}
