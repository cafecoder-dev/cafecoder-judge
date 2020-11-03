package types

import (
	"sync"
)

type CmdTicket struct {
	sync.Mutex
	Channel map[string]chan CmdResultJSON
}

type SubmitT struct {
	Info SubmitsGORM

	HashedID string
}

type LanguageConfig struct {
	FileName   string
	CompileCmd string
	ExecuteCmd string
}
