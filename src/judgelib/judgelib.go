package judgelib

import (
	"context"
	"fmt"
	"github.com/jinzhu/gorm"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/cafecoder-dev/cafecoder-judge/src/cmdlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/dkrlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/gcplib"
	"github.com/cafecoder-dev/cafecoder-judge/src/langconf"
	"github.com/cafecoder-dev/cafecoder-judge/src/sqllib"
	"github.com/cafecoder-dev/cafecoder-judge/src/types"
	"github.com/cafecoder-dev/cafecoder-judge/src/util"
)

var priorityMap = map[string]int{"WJ": 0, "WR": 1, "AC": 2, "TLE": 3, "MLE": 4, "OLE": 5, "WA": 6, "RE": 7, "CE": 8, "IE": 9}

// Judge ... ジャッジのフロー
func Judge(args types.SubmitsGORM, cmdChickets *types.CmdTicket) {
	var submit = types.SubmitT{}
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

	defer func() {
		os.Remove(submit.CodePath)
		os.Remove("./codes/" + submit.HashedID)
	}()

	if err := langconf.LangConfig(&submit); err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}

	codePath, err := gcplib.DownloadSourceCode(ctx, submit)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}

	submit.CodePath = codePath

	err = dkrlib.CreateContainer(ctx, &submit)
	if err != nil {
		fmt.Printf("%s\n", err.Error())
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}
	defer dkrlib.RemoveContainer(ctx, submit)

	if err := dkrlib.CopyToContainer(ctx, codePath, submit.FileName, 0777, submit); err != nil {
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
	if priorityMap[submit.Result.Status] <= 6 {
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

	fmt.Println(submit.Result)

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
		Where("problem_id=? AND testcase_testcase_sets.deleted_at IS NULL", submit.Info.ProblemID).
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

	fmt.Println(recv.ErrMessage)

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

	submit.Result.Status = "-"

	db, err := sqllib.NewDB()
	if err != nil {
		return err
	}
	defer db.Close()

	var testcases []types.TestcaseGORM
	var testcasesNum = 0
	db.
		Table("testcases").
		Where("problem_id=? AND deleted_at IS NULL", strconv.FormatInt(submit.Info.ProblemID, 10)).
		Order("id").
		Find(&testcases).
		Count(&testcasesNum)

	if submit.Info.Status == "WR" {
		db.
			Table("testcase_results").
			Where("submit_id = ? AND deleted_at IS NULL", submit.Info.ID).
			Update("deleted_at", util.TimeToString(time.Now())) // todo gorm に追加
	}

	submit.Testcases = testcases

	var problem types.ProblemsGORM
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

		stdoutBuf, err := dkrlib.CopyFromContainer(ctx, "/userStdout.txt", *submit)
		if err != nil {
			return err
		}
		stdoutLines := strings.Split(stdoutBuf.String(), "\n")

		stderrBuf, err := dkrlib.CopyFromContainer(ctx, "/userStderr.txt", *submit)
		if err != nil {
			return err
		}
		stderrLines := strings.Split(stderrBuf.String(), "\n")

		outputTestcaseLines := strings.Split(output, "\n")

		if recv.Time > 2000 {
			testcaseResults.Status = "TLE"
		} else {
			if !recv.Result {
				for j := 0; j < len(stderrLines); j++ {
					println(stderrLines[j])
				}
				testcaseResults.Status = "RE"
			} else {
				testcaseResults.Status = "WA"
				for j := 0; j < len(stdoutLines) && j < len(outputTestcaseLines); j++ {
					testcaseResults.Status = "AC"
					if strings.TrimSpace(stdoutLines[j]) != strings.TrimSpace(outputTestcaseLines[j]) {
						testcaseResults.Status = "WA"
						break
					}
				}
			}
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

		// testcase_results の挿入
		db.
			Table("testcase_results").
			Create(&testcaseResults)

		submit.TestcaseResultsMap[testcaseResults.TestcaseID] = testcaseResults
	}

	return nil
}
