package judgelib

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jinzhu/gorm"

	_ "github.com/go-sql-driver/mysql"

	"github.com/cafecoder-dev/cafecoder-judge/src/checklib"
	"github.com/cafecoder-dev/cafecoder-judge/src/cmdlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/dkrlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/gcplib"
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

	res, err := cmdlib.RequestCmd(
		cmdlib.CmdRequest{
			Mode: "download",
		},
		submit.ContainerInspect.NetworkSettings.IPAddress,
		id,
		&sessionIDChan,
	)
	if err != nil || !res.Result {
		fmt.Printf("%s\n", err.Error())
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}


	if err = compile(&submit, &sessionIDChan); err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}
	if submit.Result.Status == "CE" {
		sendResult(submit)
		return
	}
	time.Sleep(time.Second * 3) // testlib.h

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

// テストケースセットからスコアリング
func scoring(submit types.SubmitT) int64 {
	var (
		testcaseSets         []types.TestcaseSetsGORM
		testcaseTestcaseSets []types.TestcaseTestcaseSetsGORM
	)

	if submit.Result.Status == "IE" || submit.Result.Status == "CE" {
		return 0
	}

	db, err := sqllib.NewDB()
	if err != nil {
		fmt.Println(err.Error())
	}
	defer db.Close()

	db.
		Table("testcase_sets").
		Where("problem_id=?", submit.Info.ProblemID).
		Find(&testcaseSets)
	db.
		Table("testcase_testcase_sets").
		Joins("INNER JOIN testcases ON testcase_testcase_sets.testcase_id = testcases.id").
		Where("problem_id=? AND testcase_testcase_sets.deleted_at IS NULL AND testcases.deleted_at IS NULL", submit.Info.ProblemID).
		Find(&testcaseTestcaseSets)

	// testcase_set_id -> testcase_id
	testcaseSetMap := map[int64][]int64{}

	for _, testcaseSet := range testcaseSets {
		testcaseSetMap[testcaseSet.ID] = make([]int64, 0)
	}
	for _, testcaseTestcaseSet := range testcaseTestcaseSets {
		testcaseSetMap[testcaseTestcaseSet.TestcaseSetID] =
			append(testcaseSetMap[testcaseTestcaseSet.TestcaseSetID], testcaseTestcaseSet.TestcaseID)
	}

	score := int64(0)
	for _, testcaseSet := range testcaseSets {
		isAC := true

		for _, testcaseID := range testcaseSetMap[testcaseSet.ID] {
			if submit.TestcaseResultsMap[testcaseID].Status != "AC" {
				isAC = false
				break
			}
		}

		if isAC {
			score += int64(testcaseSet.Points)
		}
	}

	return score
}

func compile(submit *types.SubmitT, sessionIDchan *chan types.CmdResultJSON) error {
	recv, err := cmdlib.RequestCmd(submit.CompileCmd, "other", *submit, sessionIDchan)
	if err != nil {
		return err
	}

	fmt.Println("Compile Result: ", recv)

	if len(recv.ErrMessage) < 65535 {
		submit.Result.CompileError = recv.ErrMessage
	} else {
		submit.Result.CompileError = recv.ErrMessage[:65535]
	}

	if !recv.Result {
		submit.Result.Status = "CE"
	}

	return nil
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
		Table("testcases").
		Where("problem_id=? AND deleted_at IS NULL", strconv.FormatInt(submit.Info.ProblemID, 10)).
		Order("id").
		Find(&testcases)
	if len(testcases) == 0 {
		return errors.New("not found testcases")
	}
	submit.Testcases = testcases

	if submit.Info.Status == "WR" {
		db.
			Table("testcase_results").
			Where("submit_id = ? AND deleted_at IS NULL", submit.Info.ID).
			Update("deleted_at", util.TimeToString(time.Now()))
	}

	db.
		Table("problems").
		Where("id = ? AND deleted_at IS NULL", submit.Info.ProblemID).
		First(&problem)

	for _, elem := range testcases {
		testcaseResults := types.TestcaseResultsGORM{SubmitID: submit.Info.ID, TestcaseID: elem.TestcaseID}

		file, _ := os.Create("./codes/" + submit.HashedID)
		input, output, err := gcplib.DownloadTestcase(ctx, problem.UUID, elem.Name)
		if err != nil {
			return err
		}
		file.Write(input)
		file.Close()

		if err = dkrlib.CopyToContainer(ctx, "./codes/"+submit.HashedID, "/testcase.txt", 0744, *submit); err != nil {
			return err
		}

		recv, err := cmdlib.RequestCmd(submit.ExecuteCmd, "judge", *submit, sessionIDChan)
		if err != nil {
			return err
		}

		testcaseResults.Status, err = judging(ctx, submit, recv, output)
		if err != nil {
			return err
		}

		if testcaseResults.Status == "PLE" {
			for range testcases {
				testcaseResults := types.TestcaseResultsGORM{SubmitID: submit.Info.ID, TestcaseID: elem.TestcaseID}
				testcaseResults.ExecutionTime = 2200
				testcaseResults.CreatedAt = util.TimeToString(time.Now())
				testcaseResults.UpdatedAt = util.TimeToString(time.Now())
				testcaseResults.Status = "TLE"
				submit.Result.Status = "TLE"
				db.Table("testcase_results").Create(&testcaseResults)
				submit.TestcaseResultsMap[testcaseResults.TestcaseID] = testcaseResults
			}
			return nil
		}

		if priorityMap[submit.Result.Status] < priorityMap[testcaseResults.Status] {
			submit.Result.Status = testcaseResults.Status

			if submit.Result.Status != "AC" {
				db.
					Table("submits").
					Where("id = ? AND deleted_at IS NULL", submit.Info.ID).
					Update("status", submit.Result.Status)
			}
		}

		testcaseResults.ExecutionTime = recv.Time
		testcaseResults.ExecutionMemory = recv.MemUsage
		testcaseResults.CreatedAt = util.TimeToString(time.Now())
		testcaseResults.UpdatedAt = util.TimeToString(time.Now())

		fmt.Println("Testcase Result: ", recv)

		// testcase_results の挿入
		db.
			Table("testcase_results").
			Create(&testcaseResults)

		submit.TestcaseResultsMap[testcaseResults.TestcaseID] = testcaseResults
	}

	return nil
}

func judging(ctx context.Context, submit *types.SubmitT, cmdres types.CmdResultJSON, output string) (string, error) {
	if cmdres.IsPLE {
		return "PLE", nil
	}
	if !cmdres.Result {
		return "RE", nil
	}
	stdoutBuf, err := dkrlib.CopyFromContainer(ctx, "/userStdout.txt", *submit)
	if err != nil {
		return "", err
	}
	if !checklib.Normal(stdoutBuf.String(), output) {
		return "WA", nil
	}
	if cmdres.IsOLE {
		return "OLE", nil
	}
	if cmdres.MemUsage > 1024000 {
		return "MLE", nil
	}
	if cmdres.Time > 2000 {
		return "TLE", nil
	}
	return "AC", nil
}
