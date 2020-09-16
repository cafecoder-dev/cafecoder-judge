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

type CmdRequest struct {
	Mode string
	Cmd  string
}

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

func RequestCmd(cmdRequest CmdRequest, containerIPAddress string, sessionID string, sessionIDChan *chan types.CmdResultJSON) (types.CmdResultJSON, error) {
	var (
		request types.RequestJSON
		recv    types.CmdResultJSON
	)

	containerConn, err := net.Dial("tcp", containerIPAddress+":8887")
	if err != nil {
		return recv, err
	}

	request = types.RequestJSON{Cmd: cmdRequest.Cmd, SessionID: sessionID, Mode: cmdRequest.Mode}
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
			fmt.Println("Request timeout")
			return types.CmdResultJSON{
				SessionID: sessionID,
				Time:      2200,
				IsPLE:     true,
			}, nil
		case recv := <-*sessionIDChan:
			if recv.SessionID == sessionID {
				return recv, nil
			}
		}
	}
}
