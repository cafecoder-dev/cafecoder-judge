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
func Judge(args types.SubmitsGORM, cmdChickets *types.CmdTicket) {
	var submit = types.SubmitT{Result: types.ResultGORM{Status: "-"}}
	submit.TestcaseResultsMap = map[int64]types.TestcaseResultsGORM{}
	ctx := context.Background()

	if !util.ValidationCheck(args) {
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}

	submit.Info = args

	id := fmt.Sprintf("%d", submit.Info.ID) // submit.info.ID を文字列に変換
	(*cmdChickets).Lock()
	sessionIDChan := (*cmdChickets).Channel[id]
	(*cmdChickets).Unlock()
	defer func() {
		(*cmdChickets).Lock()
		delete((*cmdChickets).Channel, id)
		(*cmdChickets).Unlock()
	}()

	submit.HashedID = util.MakeStringHash(id)

	err := dkrlib.CreateContainer(ctx, &submit)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}
	defer dkrlib.RemoveContainer(ctx, submit)

	if err := langconf.LangConfig(&submit); err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}

	recv, err := cmdlib.RequestCmd(
		types.RequestJSON{
			Mode:      "download",
			SessionID: fmt.Sprintf("%d", submit.Info.ID),
			Filename:  submit.FileName,
			CodePath:  submit.Info.Path,
		},
		submit.ContainerInspect.NetworkSettings.IPAddress,
		&sessionIDChan,
	)
	if err != nil || !recv.Result {
		fmt.Printf("%s\n", err.Error())
		fmt.Printf("%s\n", recv.ErrMessage)
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}

	compileRes, err := compile(&submit, &sessionIDChan)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}
	if !compileRes {
		submit.Result.Status = "CE"
		sendResult(submit)
		return
	}

	if err = tryTestcase(ctx, &submit, &sessionIDChan); err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}

	sendResult(submit)

	return
}

// 最終的な結果を DB に投げる。
func sendResult(submit types.SubmitT) {
	if priorityMap[submit.Result.Status] <= 7 {
		for _, elem := range submit.TestcaseResultsMap {
			if elem.ExecutionTime > submit.Result.ExecutionTime {
				submit.Result.ExecutionTime = elem.ExecutionTime
			}
			if elem.ExecutionMemory > submit.Result.ExecutionMemory {
				submit.Result.ExecutionMemory = elem.ExecutionMemory
			}
		}
	}

	db, err := sqllib.NewDB()
	if err != nil {
		fmt.Println(err.Error())
	}
	defer db.Close()

	submit.Result.Point = int(scoring(submit))

	db.
		Table("submits").
		Where("id=? AND deleted_at IS NULL", submit.Info.ID).
		Update(&submit.Result).
		Update("execution_time", submit.Result.ExecutionTime).
		Update("execution_memory", submit.Result.ExecutionMemory).
		Update("point", submit.Result.Point)

	if submit.Result.Status == "CE" {
		db.
			Table("submits").
			Where("id=? AND deleted_at IS NULL", submit.Info.ID).
			Update("execution_memory", gorm.Expr("NULL")).
			Update("execution_time", gorm.Expr("NULL"))
	}

	<-util.JudgeNumberLimit
}

func compile(submit *types.SubmitT, sessionIDchan *chan types.CmdResultJSON) (bool, error) {
	recv, err := cmdlib.RequestCmd(
		types.RequestJSON{
			Mode:      "compile",
			Cmd:       submit.CompileCmd,
			SessionID: fmt.Sprintf("%d", submit.Info.ID),
			Filename:  submit.FileName,
		},
		submit.ContainerInspect.NetworkSettings.IPAddress,
		sessionIDchan,
	)
	if err != nil {
		return false, err
	}

	fmt.Println("Compile Result: ", recv)

	if len(recv.ErrMessage) < 65535 {
		submit.Result.CompileError = recv.ErrMessage
	} else {
		submit.Result.CompileError = recv.ErrMessage[:65535]
	}

	return recv.Result, nil
}

func tryTestcase(ctx context.Context, submit *types.SubmitT, sessionIDChan *chan types.CmdResultJSON) error {
	var (
		testcases []types.TestcaseGORM
		problem   types.ProblemsGORM
	)

	db, err := sqllib.NewDB()
	if err != nil {
		return err
	}
	defer db.Close()

	db.
		Table("problems").
		Where("id = ? AND deleted_at IS NULL", submit.Info.ProblemID).
		First(&problem)

	db.
		Table("testcases").
		Where("problem_id=? AND deleted_at IS NULL", submit.Info.ProblemID).
		Find(&testcases)

	if len(testcases) == 0 {
		return errors.New("testcases not found")
	}

	if submit.Info.Status == "WR" {
		db.
			Table("testcase_results").
			Where("submit_id = ? AND deleted_at IS NULL", submit.Info.ID).
			Update("deleted_at", util.TimeToString(time.Now()))
	}

	for _, elem := range testcases {
		recv, err := cmdlib.RequestCmd(
			types.RequestJSON{
				Mode:      "judge",
				Cmd:       submit.ExecuteCmd,
				SessionID: fmt.Sprintf("%d", submit.Info.ID),
				ProblemID: fmt.Sprintf("%d", submit.Info.ProblemID),
				Filename:  submit.FileName,
				Testcase:  elem,
				Problem:   problem,
			},
			submit.ContainerInspect.NetworkSettings.IPAddress,
			sessionIDChan,
		)
		if err != nil {
			return err
		}

		if priorityMap[submit.Result.Status] < priorityMap[recv.TestcaseResults.Status] {
			submit.Result.Status = recv.TestcaseResults.Status

			if submit.Result.Status != "AC" {
				db.
					Table("submits").
					Where("id = ? AND deleted_at IS NULL", submit.Info.ID).
					Update("status", submit.Result.Status)
			}
		}

		fmt.Println("Testcase Result: ", recv.TestcaseResults)

		// testcase_results の挿入
		db.
			Table("testcase_results").
			Create(&recv.TestcaseResults)

		submit.TestcaseResultsMap[recv.TestcaseResults.TestcaseID] = recv.TestcaseResults
	}

	return nil
}
