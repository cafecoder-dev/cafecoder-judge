package cmdlib

import (
	"encoding/base64"
	"encoding/json"

	// "errors"
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

func RequestCmd(request types.RequestJSON, containerIPAddress string, sessionIDChan *chan types.CmdResultJSON) (types.CmdResultJSON, error) {
	var (
		recv          types.CmdResultJSON
		containerConn net.Conn
		err           error
	)

	// コンテナへのリクエストが失敗したら再リクエストする。
	count := 0
	for {
		containerConn, err = net.Dial("tcp", containerIPAddress+":8887")
		if err != nil {
			time.Sleep(time.Second)
			fmt.Println("Request again")
			count++
			if count > 10 {
				return recv, err
			}
			continue
		}

		break
	}

	b, err := json.Marshal(request)
	if err != nil {
		return recv, err
	}

	_, err = containerConn.Write(b)
	if err != nil {
		return recv, err
	}
	containerConn.Close()

	timeout := time.After(20 * time.Second)
	for {
		select {
		case <-timeout:
			fmt.Println("Request timed out")
			return types.CmdResultJSON{
				SessionID: request.SessionID,
				Time:      2200,
				Timeout:   true,
			}, nil
		case recv := <-*sessionIDChan:
			if recv.SessionID == request.SessionID {
				return recv, nil
			}
		}
	}
}
