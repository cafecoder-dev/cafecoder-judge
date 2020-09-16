package types

import "github.com/cafecoder-dev/cafecoder-judge/src/types"

type CmdResultJSON struct {
	SessionID  string `json:"sessionID"`
	Time       int    `json:"time"`
	Result     bool   `json:"result"`
	ErrMessage string `json:"errMessage"`
	MemUsage   int    `json:"memUsage"`
	IsOLE      bool   `json:"isOLE"`
	StdoutSize int64  `json:"stdoutSize"`
	IsPLE      bool   `json:"isPLE"`

	Status   string `json:"status"`
	Filename string `json:"filename"`

	Testcases types.TestcaseResultsGORM
}

type RequestJSON struct {
	SessionID string `json:"sessionID"`
	Cmd       string `json:"cmd"`
	Mode      string `json:"mode"` //Mode ... "judge" or "compile" or "download"
	CodePath  string `json:"codePath"`
	Filename  string `json:"filename"`
	ProblemID string `json:"problemID"`
}
