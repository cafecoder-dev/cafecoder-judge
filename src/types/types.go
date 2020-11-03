package types

import (
	"sync"
)

type CmdTicket struct {
	sync.Mutex
	Channel map[string]chan CmdResultJSON
}

type SubmitT struct {
	Info   SubmitsGORM
	Result ResultGORM

	TestcaseResultsMap map[int64]TestcaseResultsGORM

	HashedID   string
	FileName   string
	CompileCmd string
	ExecuteCmd string
}
