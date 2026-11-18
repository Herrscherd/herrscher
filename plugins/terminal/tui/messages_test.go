package tui

import "github.com/Herrscherd/herrscher/core/clitext"

func testMessages() *clitext.Messages {
	return clitext.NewMessages()
}

func testTranslator() translator {
	return newTranslator(testMessages())
}
