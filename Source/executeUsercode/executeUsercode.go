package main

import (
	"encoding/json"
	"fmt"
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
	SessionID string `json:"sessionID"`

	Command string `json:"command"`
	Mode    string `json:"mode"` //Mode ... "judge" or "others"
	//Lang      string `json:"lang"` //Lang ... c11,c++17,java8,python3,c#,ruby
}

func main() {
	listen, err := net.Listen("tcp", "0.0.0.0:8888") //from backend server
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

func executeJudge(request requestJSON) {
	var cmdResult cmdResultJSON
	cmdResult.SessionID = request.SessionID
	if request.Mode == "judge" {
		start := time.Now().UnixNano()
		err := exec.Command(request.Command).Run()
		end := time.Now().UnixNano()
		cmdResult.Time = (end - start) / int64(time.Millisecond)
		if err != nil {
			cmdResult.Result = false
		} else {
			cmdResult.Result = true
		}
		cmdResult.ErrMessage = err.Error()
	} else {
		err := exec.Command(request.Command).Run()
		if err != nil {
			cmdResult.Result = false
		} else {
			cmdResult.Result = true
		}
		cmdResult.ErrMessage = err.Error()
	}

	conn, _ := net.Dial("tcp", "133.130.112.219:3344")
	b, _ := json.Marshal(cmdResult)
	conn.Write(b)
	conn.Close()
}
