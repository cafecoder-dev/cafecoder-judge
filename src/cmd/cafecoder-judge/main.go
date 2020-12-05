package main

import (
	"fmt"
	"log"
	"strconv"

	"github.com/cafecoder-dev/cafecoder-judge/src/cmdlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/judgelib"
	"github.com/cafecoder-dev/cafecoder-judge/src/sqllib"
	"github.com/cafecoder-dev/cafecoder-judge/src/types"
	_ "github.com/go-sql-driver/mysql"
)

// JudgeNumberLimit ... limits the number of judges
//
// see: https://mattn.kaoriya.net/software/lang/go/20171221111857.htm
var MaxJudge string
var JudgeNumberLimit chan struct{}

func main() {
	m, err := strconv.Atoi(MaxJudge)
	if err != nil {
		log.Fatal(err)
	}

	JudgeNumberLimit = make(chan struct{}, m)
	fmt.Println(len(JudgeNumberLimit))

	cmdChickets := cmdlib.CmdTicket{Channel: make(map[string]chan types.CmdResultJSON)}
	go cmdlib.ManageCmds(&cmdChickets)

	db, err := sqllib.NewDB()
	if err != nil {
		log.Fatal(err)
	}

	for {
		var res []types.SubmitsGORM

		if result := db.Table("submits").
			Where("deleted_at IS NULL").
			Where("status='WR' OR status='WJ'").
			Order("updated_at").
			Find(&res); result.Error != nil {
			log.Fatal(err)
		}

		for _, elem := range res {
			cmdChickets.Lock()
			_, exist := cmdChickets.Channel[fmt.Sprintf("%d", elem.ID)]
			cmdChickets.Unlock()

			if exist {
				continue
			} else {
				// wait until the number of judges becomes less than maxJudge
				JudgeNumberLimit <- struct{}{}

				cmdChickets.Lock()
				cmdChickets.Channel[fmt.Sprintf("%d", elem.ID)] = make(chan types.CmdResultJSON)
				cmdChickets.Unlock()

				go func() {
					judgelib.Judge(elem, &cmdChickets)
					<-JudgeNumberLimit
				}()
			}
		}
	}
}
