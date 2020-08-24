package judgelib

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/cafecoder-dev/cafecoder-judge/src/cmdlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/gcplib"
	"github.com/cafecoder-dev/cafecoder-judge/src/types"
	"github.com/cafecoder-dev/cafecoder-judge/src/util"
	"github.com/cafecoder-dev/cafecoder-judge/src/langconf"
	"github.com/cafecoder-dev/cafecoder-judge/src/dkrlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/sqllib"
)

// ジャッジのフロー　tryTestcase と混同するけど致し方ない・・？
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

	submit.HashedID = makeStringHash(id)

	defer func() {
		os.Remove(submit.CodePath)
		os.Remove("./codes/" + submit.HashedID)
	}()

	if err := langconf.LangConfig(&submit); err != nil {
		println(err.Error())
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
		fmt.Printf("[ERROR] container: %s\n", err.Error())
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
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}
	if submit.Result.Status == "CE" {
		sendResult(submit)
		return
	}

	if err = tryTestcase(ctx, &submit, &sessionIDChan); err != nil {
		submit.Result.Status = "IE"
		sendResult(submit)
		return
	}

	sendResult(submit)

	return
}

// 最終的な結果を DB に投げる。モジュールの分割が雑すぎるからなんとかしたい
func sendResult(submit types.SubmitT) {
	priorityMap := map[string]int{"-": 0, "AC": 1, "WA": 2, "TLE": 3, "RE": 4, "MLE": 5, "CE": 6, "IE": 7}

	if priorityMap[submit.Result.Status] < 6 {
		submit.Result.Status = "AC"
		for _, elem := range submit.TestcaseResultsMap {
			if priorityMap[elem.Status] > priorityMap[submit.Result.Status] {
				submit.Result.Status = elem.Status
			}
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

	for _, elem := range submit.TestcaseResultsMap {
		if submit.Info.Status == "WR" {
			db.
				Table("testcase_results").
				Where("submit_id = ? AND testcase_id = ?", submit.Info.ID, elem.TestcaseID).
				Update(elem.UpdatedAt).
				Update(elem.Status).
				Update(elem.ExecutionTime).
				Update(elem.ExecutionMemory)
		} else if submit.Info.Status == "WJ" {
			db.
				Table("testcase_results").
				Create(&elem)
		}
	}

	submit.Result.Point = int(scoring(submit))

	db.
		Table("submits").
		Where("id=? AND deleted_at IS NULL", submit.Info.ID).
		Update(&submit.Result).
		Update("point", submit.Result.Point).
		Update("execution_memory", submit.Result.ExecutionMemory)

	<-util.JudgeNumberLimit
}

// テストケースセットからスコアリング
func scoring(submit types.SubmitT) int64 {
	if submit.Result.Status == "IE" || submit.Result.Status == "CE" {
		return 0
	}

	db, err := sqllib.NewDB()
	if err != nil {
		fmt.Println(err.Error())
	}
	defer db.Close()

	var (
		testcaseSets         []types.TestcaseSetsGORM
		testcaseTestcaseSets []types.TestcaseTestcaseSetsGORM
	)

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
				fmt.Printf("status(%d): %s\n", testcaseID, submit.TestcaseResultsMap[testcaseID].Status)
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
		fmt.Printf("%s\n", err.Error())
		return err
	}

	if !recv.Result {
		fmt.Printf("%s CE\n", recv.ErrMessage)
		submit.Result.Status = "CE"
		return nil
	}

	return nil
}

func makeStringHash(str string) string {
	hash := sha256.Sum256([]byte(str))
	return hex.EncodeToString(hash[:])
}

func tryTestcase(ctx context.Context, submit *types.SubmitT, sessionIDChan *chan types.CmdResultJSON) error {
	var (
		TLEcase bool
	)

	db, err := sqllib.NewDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	defer db.Close()

	testcases := []types.TestcaseGORM{}
	var testcasesNum = 0
	db.
		Table("testcases").
		Where("problem_id=? AND deleted_at IS NULL", strconv.FormatInt(submit.Info.ProblemID, 10)).
		Order("id").
		Find(&testcases).
		Count(&testcasesNum)

	submit.Testcases = testcases

	for i := 0; i < testcasesNum; i++ {
		testcaseResults := types.TestcaseResultsGORM{SubmitID: submit.Info.ID, TestcaseID: submit.Testcases[i].TestcaseID}

		// skip
		if TLEcase {
			testcaseResults.Status = "-"
			testcaseResults.CreatedAt = util.TimeToString(time.Now())
			testcaseResults.UpdatedAt = util.TimeToString(time.Now())
			submit.TestcaseResultsMap[submit.Testcases[i].TestcaseID] = testcaseResults
			continue
		}

		file, _ := os.Create("./codes/" + submit.HashedID)
		file.Write(([]byte)(testcases[i].Input))
		file.Close()

		if err = dkrlib.CopyToContainer(ctx, "./codes/"+submit.HashedID, "/testcase.txt", 0744, *submit); err != nil {
			println("tar copy error")
			return err
		}

		recv, err := cmdlib.RequestCmd(submit.ExecuteCmd, "judge", *submit, sessionIDChan)
		if err != nil {
			println("requestCmd error")
			return err
		}

		stdoutBuf, err := dkrlib.CopyFromContainer(ctx, "/userStdout.txt", *submit)
		if err != nil {
			println(err.Error())
			return err
		}
		stderrBuf, err := dkrlib.CopyFromContainer(ctx, "/userStderr.txt", *submit)
		if err != nil {
			println(err.Error())
			return err
		}

		stdoutLines := strings.Split(stdoutBuf.String(), "\n")
		stderrLines := strings.Split(stderrBuf.String(), "\n")

		outputTestcaseLines := strings.Split(string(testcases[i].Output), "\n")

		if recv.Time > 2000 {
			testcaseResults.Status = "TLE"
			TLEcase = true
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
					if strings.TrimSpace(string(stdoutLines[j])) != strings.TrimSpace(string(outputTestcaseLines[j])) {
						testcaseResults.Status = "WA"
						break
					}
				}
			}
		}

		testcaseResults.ExecutionTime = recv.Time
		testcaseResults.ExecutionMemory = recv.MemUsage
		if submit.Info.Status == "WR" {
			testcaseResults.CreatedAt = util.TimeToString(time.Now())
		}
		testcaseResults.UpdatedAt = util.TimeToString(time.Now())

		submit.TestcaseResultsMap[testcaseResults.TestcaseID] = testcaseResults
	}

	return nil
}
