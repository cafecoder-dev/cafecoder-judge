package judgelib

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jinzhu/gorm"

	_ "github.com/go-sql-driver/mysql"

	"github.com/cafecoder-dev/cafecoder-judge/src/cmdlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/dkrlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/langconf"
	"github.com/cafecoder-dev/cafecoder-judge/src/sqllib"
	"github.com/cafecoder-dev/cafecoder-judge/src/types"
	"github.com/cafecoder-dev/cafecoder-judge/src/util"
)

var priorityMap = map[string]int{"-": 0, "AC": 2, "TLE": 3, "MLE": 4, "OLE": 5, "WA": 6, "RE": 7, "CE": 8, "IE": 9}

// Judge ... ジャッジのフロー
func Judge(submits types.SubmitsGORM, cmdChickets *cmdlib.CmdTicket) {
	result := types.ResultGORM{Status: "-"}

	ctx := context.Background()

	if !util.ValidationCheck(submits) {
		result.Status = "IE"
		sendResult(submits, result)
		return
	}

	id := fmt.Sprintf("%d", submits.ID) // submit.info.ID を文字列に変換
	(*cmdChickets).Lock()
	sessionIDChan := (*cmdChickets).Channel[id]
	(*cmdChickets).Unlock()

	defer func() {
		(*cmdChickets).Lock()
		delete((*cmdChickets).Channel, id)
		(*cmdChickets).Unlock()
	}()

	//containerName := util.MakeStringHash(id)
	containerName := util.GenRandomString(32)

	container, err := dkrlib.CreateContainer(ctx, containerName)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		result.Status = "IE"
		sendResult(submits, result)
		return
	}
	defer container.RemoveContainer(ctx)

	langConfig, err := langconf.LangConfig(submits.Lang)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		result.Status = "IE"
		sendResult(submits, result)
		return
	}

	recv, err := cmdlib.RequestCmd(
		types.RequestJSON{
			Mode:      "download",
			SessionID: fmt.Sprintf("%d", submits.ID),
			Filename:  langConfig.FileName,
			CodePath:  submits.Path,
		},
		container.IPAddress,
		&sessionIDChan,
	)
	if err != nil || !recv.Result {
		fmt.Printf("%s\n", err.Error())
		fmt.Printf("%s\n", recv.ErrMessage)
		result.Status = "IE"
		sendResult(submits, result)
		return
	}

	compileRes, err := compile(fmt.Sprintf("%d", submits.ID), container.IPAddress, langConfig, &sessionIDChan)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		result.Status = "IE"
		sendResult(submits, result)
		return
	}
	if !compileRes.Result {
		result.Status = "CE"
		result.CompileError = compileRes.ErrMessage
		sendResult(submits, result)
		return
	}

	result, err = tryTestcase(ctx, submits, langConfig, container.IPAddress, &sessionIDChan)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		result.Status = "IE"
		sendResult(submits, result)
		return
	}

	sendResult(submits, result)
}

// 最終的な結果を DB に投げる。
func sendResult(submits types.SubmitsGORM, result types.ResultGORM) {
	if priorityMap[result.Status] <= 7 {
		for _, elem := range result.TestcaseResultsMap {
			if elem.ExecutionTime > result.ExecutionTime {
				result.ExecutionTime = elem.ExecutionTime
			}
			if elem.ExecutionMemory > result.ExecutionMemory {
				result.ExecutionMemory = elem.ExecutionMemory
			}
		}
	}

	db, err := sqllib.NewDB()
	if err != nil {
		fmt.Println(err.Error())
	}
	defer db.Close()

	result.Point = int(scoring(submits, result))

	db.
		Table("submits").
		Where("id=? AND deleted_at IS NULL", submits.ID).
		Update("status", result.Status).
		Update("execution_time", result.ExecutionTime).
		Update("execution_memory", result.ExecutionMemory).
		Update("point", result.Point)

	if result.Status == "CE" {
		db.
			Table("submits").
			Where("id=? AND deleted_at IS NULL", submits.ID).
			Update("execution_memory", gorm.Expr("NULL")).
			Update("execution_time", gorm.Expr("NULL")).
			Update("compile_error", result.CompileError)
	}
}

func compile(submitID string, containerIPAddress string, langConfig langconf.LanguageConfig, sessionIDchan *chan types.CmdResultJSON) (types.CmdResultJSON, error) {
	recv, err := cmdlib.RequestCmd(
		types.RequestJSON{
			Mode:      "compile",
			Cmd:       langConfig.CompileCmd,
			SessionID: submitID,
			Filename:  langConfig.FileName,
		},
		containerIPAddress,
		sessionIDchan,
	)
	if err != nil {
		return types.CmdResultJSON{}, err
	}

	fmt.Println("Compile Result: ", recv)

	time.Sleep(2 * time.Second)

	return recv, nil
}

func tryTestcase(ctx context.Context, submits types.SubmitsGORM, langConfig langconf.LanguageConfig, containerIPAddress string, sessionIDChan *chan types.CmdResultJSON) (types.ResultGORM, error) {
	var (
		testcases []types.TestcaseGORM
		problem   types.ProblemsGORM
		result    types.ResultGORM
	)

	db, err := sqllib.NewDB()
	if err != nil {
		return types.ResultGORM{}, err
	}
	defer db.Close()

	db.
		Table("problems").
		Where("id = ? AND deleted_at IS NULL", submits.ProblemID).
		First(&problem)

	db.
		Table("testcases").
		Where("problem_id=? AND deleted_at IS NULL", submits.ProblemID).
		Find(&testcases)

	if len(testcases) == 0 {
		return types.ResultGORM{}, errors.New("testcases not found")
	}

	if submits.Status == "WR" {
		db.
			Table("testcase_results").
			Where("submit_id = ? AND deleted_at IS NULL", submits.ID).
			Update("deleted_at", util.TimeToString(time.Now()))
	}

	result.TestcaseResultsMap = make(map[int64]types.TestcaseResultsGORM)

	for _, elem := range testcases {
		req := types.RequestJSON{
			Mode:      "judge",
			Cmd:       langConfig.ExecuteCmd,
			SessionID: fmt.Sprintf("%d", submits.ID),
			ProblemID: fmt.Sprintf("%d", submits.ProblemID),
			Filename:  langConfig.FileName,
			Testcase:  elem,
			Problem:   problem,
			TimeLimit: func() int {
				if submits.Lang == "python38" {
					return 6000
				} else {
					return 2000
				}
			}(),
		}
		recv, err := cmdlib.RequestCmd(
			req,
			containerIPAddress,
			sessionIDChan,
		)
		if err != nil {
			return types.ResultGORM{}, err
		}

		if recv.Timeout { // コンテナにリクエストが送れなかったとき
			result.Status = "TLE"
			db.
				Table("submits").
				Where("id = ? AND deleted_at IS NULL", submits.ID).
				Update("status", result.Status)
			for _, elem := range testcases {
				testcaseResults := types.TestcaseResultsGORM{
					SubmitID:      submits.ID,
					TestcaseID:    elem.TestcaseID,
					Status:        "TLE",
					ExecutionTime: req.TimeLimit,
					CreatedAt:     util.TimeToString(time.Now()),
					UpdatedAt:     util.TimeToString(time.Now()),
				}
				db.
					Table("testcase_results").
					Create(&testcaseResults)
			}
			break
		}

		if priorityMap[result.Status] < priorityMap[recv.TestcaseResults.Status] {
			result.Status = recv.TestcaseResults.Status

			if result.Status != "AC" {
				db.
					Table("submits").
					Where("id = ? AND deleted_at IS NULL", submits.ID).
					Update("status", result.Status)
			}
		}

		fmt.Println("Testcase Result: ", recv.TestcaseResults)

		// testcase_results の挿入
		db.
			Table("testcase_results").
			Create(&recv.TestcaseResults)

		result.TestcaseResultsMap[recv.TestcaseResults.TestcaseID] = recv.TestcaseResults
	}

	return result, nil
}
