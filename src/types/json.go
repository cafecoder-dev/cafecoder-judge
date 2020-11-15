package types

type CmdResultJSON struct {
	SessionID  string `json:"sessionID"`
	Time       int    `json:"time"`
	Result     bool   `json:"result"`
	ErrMessage string `json:"errMessage"`
	MemUsage   int    `json:"memUsage"`
	StdoutSize int64  `json:"stdoutSize"`
	IsPLE      bool   `json:"isPLE"`

	Status   string `json:"status"`
	Filename string `json:"filename"`

	Timeout         bool
	TestcaseResults TestcaseResultsGORM `json:"testcase_results"`
}

type RequestJSON struct {
	Mode      string       `json:"mode"` //Mode ... "judge" or "compile" or "download"
	SessionID string       `json:"sessionID"`
	Cmd       string       `json:"cmd"`
	CodePath  string       `json:"codePath"`
	Filename  string       `json:"filename"`
	ProblemID string       `json:"problemID"`
	TimeLimit int          `json:"timeLimit"`
	Testcase  TestcaseGORM `json:"testcase"`
	Problem   ProblemsGORM `json:"problem"`
}
