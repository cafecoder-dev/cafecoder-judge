package judgelib

import (
	"fmt"

	"github.com/cafecoder-dev/cafecoder-judge/src/sqllib"
	"github.com/cafecoder-dev/cafecoder-judge/src/types"
)

// テストケースセットからスコアリング
func scoring(submits types.SubmitsGORM, result types.ResultGORM) int64 {
	var (
		testcaseSets         []types.TestcaseSetsGORM
		testcaseTestcaseSets []types.TestcaseTestcaseSetsGORM
	)

	if result.Status == "IE" || result.Status == "CE" {
		return 0
	}

	db, err := sqllib.NewDB()
	if err != nil {
		fmt.Println(err.Error())
	}
	defer db.Close()

	db.
		Table("testcase_sets").
		Where("deleted_at IS NULL").
		Where("problem_id=?", submits.ProblemID).
		Find(&testcaseSets)
	db.
		Table("testcase_testcase_sets").
		Joins("INNER JOIN testcases ON testcase_testcase_sets.testcase_id = testcases.id").
		Where("problem_id=? AND testcase_testcase_sets.deleted_at IS NULL AND testcases.deleted_at IS NULL", submits.ProblemID).
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
			if result.TestcaseResultsMap[testcaseID].Status != "AC" {
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
