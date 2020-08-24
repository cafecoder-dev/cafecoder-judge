package types

import (
	"sync"

	docker_types "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

type CmdTicket struct {
	sync.Mutex
	Channel map[string]chan CmdResultJSON
}

type SubmitT struct {
	Info   SubmitsGORM
	Result ResultGORM

	Testcases          []TestcaseGORM
	TestcaseResultsMap map[int64]TestcaseResultsGORM

	HashedID     string
	ExecDirPath  string
	ExecFilePath string
	FileName     string
	CompileCmd   string
	ExecuteCmd   string
	CodePath         string
	
	ContainerCli     *client.Client
	ContainerID      string
	ContainerInspect docker_types.ContainerJSON
}