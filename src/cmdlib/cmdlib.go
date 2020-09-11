package cmdlib

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/cafecoder-dev/cafecoder-judge/src/types"
)

// ManageCmds ... コンテナからの応答を待つ。
func ManageCmds(cmdChickets *types.CmdTicket) {
	listen, err := net.Listen("tcp", "0.0.0.0:3344")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}

	for {
		cnct, err := listen.Accept()
		if err != nil {
			continue //continue to receive request
		}
		go func() {
			var cmdResult types.CmdResultJSON
			json.NewDecoder(cnct).Decode(&cmdResult)
			cnct.Close()

			data, _ := base64.StdEncoding.DecodeString(cmdResult.ErrMessage)

			cmdResult.ErrMessage = string(data)
			go func() {
				(*cmdChickets).Lock()
				(*cmdChickets).Channel[cmdResult.SessionID] <- cmdResult
				(*cmdChickets).Unlock()
			}()
		}()
	}
}

// RequestCmd ... 起動中のコンテナにコマンドの実行をリクエストする
func RequestCmd(cmd string, mode string, submit types.SubmitT, sessionIDChan *chan types.CmdResultJSON) (types.CmdResultJSON, error) {
	var (
		request types.RequestJSON
		recv    types.CmdResultJSON
		start   time.Time
		end     time.Time
	)

	containerConn, err := net.Dial("tcp", submit.ContainerInspect.NetworkSettings.IPAddress+":8887")
	if err != nil {
		return recv, err
	}

	request = types.RequestJSON{Cmd: cmd, SessionID: fmt.Sprintf("%d", submit.Info.ID), Mode: mode}
	b, err := json.Marshal(request)
	if err != nil {
		return recv, err
	}

	_, err = containerConn.Write(b)
	if err != nil {
		return recv, err
	}
	containerConn.Close()

	start = time.Now()
	for {
		tmp := <-*sessionIDChan
		if tmp.SessionID == fmt.Sprintf("%d", submit.Info.ID) {
			recv = tmp
			break
		}

		end = time.Now()
		if (end.Sub(start)).Milliseconds() >= 2000+5000 {
			recv = types.CmdResultJSON{
				SessionID: fmt.Sprintf("%d", submit.Info.ID),
				Time:      int((end.Sub(start)).Milliseconds()),
				IsPLE:     true,
			}
			break
		}
	}

	return recv, nil
}
