package types

type ResultGORM struct {
	Status          string `gorm:"column:status"`
	ExecutionTime   int    `gorm:"column:execution_time"`
	ExecutionMemory int    `gorm:"column:execution_memory"`
	Point           int    `gorm:"column:point"` // int64 にしたほうがいいかもしれない(カラムにあわせて int にした)
	CompileError    string `gorm:"column:compile_error"`
}

type TestcaseResultsGORM struct {
	SubmitID        int64  `gorm:"column:submit_id"`
	TestcaseID      int64  `gorm:"column:testcase_id"`
	Status          string `gorm:"column:status"`
	ExecutionTime   int    `gorm:"column:execution_time"`
	ExecutionMemory int    `gorm:"column:execution_memory"`
	CreatedAt       string `gorm:"column:created_at"`
	UpdatedAt       string `gorm:"column:updated_at"`
}

type TestcaseGORM struct {
	TestcaseID int64  `gorm:"column:id"`
	Input      string `gorm:"column:input"`
	Output     string `gorm:"column:output"`
}

type SubmitsGORM struct {
	ID        int64  `gorm:"column:id"`
	Status    string `gorm:"column:status"`
	ProblemID int64  `gorm:"column:problem_id"`
	Path      string `gorm:"column:path"`
	Lang      string `gorm:"column:lang"`
}

type TestcaseSetsGORM struct {
	ID     int64 `gorm:"column:id"`
	Points int   `gorm:"column:points"`
}

type TestcaseTestcaseSetsGORM struct {
	TestcaseID    int64 `gorm:"column:testcase_id"`
	TestcaseSetID int64 `gorm:"column:testcase_set_id"`
}
