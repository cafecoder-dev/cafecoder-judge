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
)

type cmdResultJSON struct {
	SessionID  string `json:"sessionID"`
	Result     bool   `json:"result"`
	ErrMessage string `json:"errMessage"`
	Time       int64  `json:"time"`
}

type requestJSON struct {
	SessionID     string `json:"sessionID"`
	DirectoryName string `json:"directoryName"`
	Command       string `json:"command"`
	Mode          string `json:"mode"` //Mode ... "judge" or "others"
	//Lang      string `json:"lang"` //Lang ... c11,c++17,java8,python3,c#,ruby
}

func main() {
	listen, err := net.Listen("tcp", "0.0.0.0:8887") //from backend server
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

func readError(cmdResult *cmdResultJSON, directoryName string) {
	stderrFp, err := os.Open("cafecoderUsers/" + directoryName + "/userStderr.txt")
	defer stderrFp.Close()
	if err != nil {
		cmdResult.ErrMessage = err.Error()
	}
	buf := make([]byte, 65536)
	buf, err = ioutil.ReadAll(stderrFp)
	if err != nil {
		cmdResult.ErrMessage = err.Error()
		return
	}
	cmdResult.ErrMessage = base64.StdEncoding.EncodeToString(buf)
	//cmdResult.ErrMessage = string(buf[:n])
}

func executeJudge(request requestJSON) {
	var cmdResult cmdResultJSON
	cmdResult.SessionID = request.SessionID
	if request.Mode == "judge" {
		os.Mkdir("cafecoderUsers/"+request.DirectoryName, 0777)
		cmd := exec.Command("sh", "-c", request.Command+"< cafecoderUsers/"+request.DirectoryName+"/testcase.txt > cafecoderUsers/"+request.DirectoryName+"/userStdout.txt"+" 2> cafecoderUsers/"+request.DirectoryName+"/userStderr.txt")
		cmdResult.ErrMessage = "testtesttest"
		start := time.Now().UnixNano()
		err := cmd.Start()
		if err != nil {
			fmt.Println("start exception")
			cmdResult.ErrMessage += "start exception\n"
		}
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

		readError(&cmdResult, request.DirectoryName)

	} else {
		cmdResult.ErrMessage = "testtesttest"
		os.Mkdir("cafecoderUsers/"+request.DirectoryName, 0777)
		err := exec.Command("sh", "-c", request.Command+" > cafecoderUsers/"+request.DirectoryName+"/userStdout.txt"+" 2> cafecoderUsers/"+request.DirectoryName+"/userStderr.txt").Run()
		if err != nil {
			cmdResult.Result = false

		} else {
			cmdResult.Result = true
		}

		readError(&cmdResult, request.DirectoryName)
	}

	conn, err := net.Dial("tcp", "172.17.0.1:3344")
	b, err := json.Marshal(cmdResult)
	if err != nil {
		conn.Write([]byte("err marshal"))
	}
	conn.Write(b)
	conn.Close()

}
