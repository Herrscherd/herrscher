package clitext

const (
	TaskNeedsText         Key = "cli.task.needs_text"
	ErrorLine             Key = "cli.error.line"
	CommandUnknown        Key = "cli.command.unknown"
	WhoamiUnknownArgument Key = "cli.whoami.unknown_argument"
	EnvContinuing         Key = "cli.env.continuing"
	EnvOpenFailed         Key = "cli.env.open_failed"
	EnvSetFailed          Key = "cli.env.set_failed"
	FlagChannel           Key = "cli.flag.channel"
	FlagChannelShort      Key = "cli.flag.channel_short"
)

var cliKeys = map[Key][]string{
	TaskNeedsText:         {"Verb"},
	ErrorLine:             {"Message"},
	CommandUnknown:        {"Name"},
	WhoamiUnknownArgument: {"Arg"},
	EnvContinuing:         {"Err"},
	EnvOpenFailed:         {"Err"},
	EnvSetFailed:          {"Key", "Err"},
	FlagChannel:           nil,
	FlagChannelShort:      nil,
}
