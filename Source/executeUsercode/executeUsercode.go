package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/struCoder/pidusage"
)

const (
	ContainerPort = "0.0.0.0:8887"
	HostPort      = "172.17.0.1:3344"
)

type cmdResultJSON struct {
	SessionID  string  `json:"sessionID"`
	Result     bool    `json:"result"`
	ErrMessage string  `json:"errMessage"`
	Time       int64   `json:"time"`
	MemUsage   float64 `json:"memUsage"`
}

type requestJSON struct {
	SessionID string `json:"sessionID"`
	DirName   string `json:"dirName"`
	Cmd       string `json:"cmd"`
	Mode      string `json:"mode"` //Mode ... "judge" or "other"
}

func main() {
	listen, err := net.Listen("tcp", ContainerPort) //from backend server
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	for {
		cnct, err := listen.Accept()
		if err != nil {
			continue //continue to receive request
		}

		var request requestJSON
		json.NewDecoder(cnct).Decode(&request)
		cnct.Close()
		println("connection closed")
		go executeJudge(request)
	}
}

func readError(cmdResult *cmdResultJSON) {
	stderrFp, err := os.Open("userStderr.txt")
	if err != nil {
		cmdResult.ErrMessage = err.Error()
	}
	buf := make([]byte, 65536)
	buf, err = ioutil.ReadAll(stderrFp)
	if err != nil {
		cmdResult.ErrMessage = err.Error()
		return
	}
	cmdResult.ErrMessage += base64.StdEncoding.EncodeToString(buf) + "\n"
	stderrFp.Close()
}

func executeJudge(request requestJSON) {
	var cmdResult cmdResultJSON
	cmdResult.SessionID = request.SessionID
	//exec.Command("sh", "-c", "ls > userStdout.txt").Run()
	if request.Mode == "judge" {
		cmd := exec.Command("sh", "-c", request.Cmd)
		start := time.Now().UnixNano()
		err := cmd.Start()
		if err != nil {
			fmt.Println("start exception")
			cmdResult.ErrMessage += "start exception\n"
		}
		info, _ := pidusage.GetStat(cmd.Process.Pid)
		cmdResult.MemUsage = info.Memory
		done := make(chan error)
		go func() { done <- cmd.Wait() }()
		timeout := time.After(2 * time.Second)
		select {
		case <-timeout:
			// Timeout happened first, kill the process and print a message.
			cmd.Process.Kill()
			fmt.Println("Command timed out")
			cmdResult.ErrMessage += "Command timed out\n"
		case err := <-done:
			if err != nil {
				fmt.Println("exception")
				cmdResult.ErrMessage += "exception\n"
			}
		}
		end := time.Now().UnixNano()
		cmdResult.Time = (end - start) / int64(time.Millisecond)
		if err != nil {
			cmdResult.Result = false
		} else {
			cmdResult.Result = true
		}
	} else {
		err := exec.Command("sh", "-c", request.Cmd).Run()
		if err != nil {
			cmdResult.Result = false
		} else {
			cmdResult.Result = true
		}
	}
	readError(&cmdResult)
	conn, err := net.Dial("tcp", HostPort)
	b, err := json.Marshal(cmdResult)
	if err != nil {
		conn.Write([]byte("err marshal"))
	}
	conn.Write(b)
	conn.Close()

}
