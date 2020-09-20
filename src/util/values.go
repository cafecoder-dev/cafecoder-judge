package util

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// JudgeNumberLimit ... limits the number of judges
//
// see: https://mattn.kaoriya.net/software/lang/go/20171221111857.htm
var JudgeNumberLimit chan struct{}

func SetJudgeNumberLimit() error {
	if err := godotenv.Load("./.env"); err != nil {
		return err
	}

	MaxJudge, err := strconv.Atoi(os.Getenv("MAX_JUDGE"))
	if err != nil {
		return err
	}

	JudgeNumberLimit = make(chan struct{}, MaxJudge)

	return nil
}
