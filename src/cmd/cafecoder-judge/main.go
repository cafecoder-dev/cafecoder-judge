package main

import (
	"fmt"
	"log"

	"github.com/cafecoder-dev/cafecoder-judge/src/cmdlib"
	"github.com/cafecoder-dev/cafecoder-judge/src/judgelib"
	"github.com/cafecoder-dev/cafecoder-judge/src/sqllib"
	"github.com/cafecoder-dev/cafecoder-judge/src/types"
	"github.com/cafecoder-dev/cafecoder-judge/src/util"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	if err := util.SetJudgeNumberLimit(); err != nil {
		log.Fatal(err)
	}

	cmdChickets := cmdlib.CmdTicket{Channel: make(map[string]chan types.CmdResultJSON)}
	go cmdlib.ManageCmds(&cmdChickets)

	db, err := sqllib.NewDB()
		if err != nil {
			log.Fatal(err)
		}


	for {
		var res []types.SubmitsGORM

		
		db.Table("submits").
			Where("deleted_at IS NULL").
			Where("status='WR' OR status='WJ'").
			Order("updated_at").
			Find(&res)

		for _, elem := range res {
			cmdChickets.Lock()
			_, exist := cmdChickets.Channel[fmt.Sprintf("%d", elem.ID)]
			cmdChickets.Unlock()

			if exist {
				continue
			} else {
				// wait until the number of judges becomes less than maxJudge
				util.JudgeNumberLimit <- struct{}{}

				cmdChickets.Lock()
				cmdChickets.Channel[fmt.Sprintf("%d", elem.ID)] = make(chan types.CmdResultJSON)
				cmdChickets.Unlock()

				go judgelib.Judge(elem, &cmdChickets)
			}
		}
	}
}
