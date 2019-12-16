package main

import (
	"encoding/json"
    "io"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
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

func executeJudge(request requestJSON) {
	var cmdResult cmdResultJSON
	cmdResult.SessionID = request.SessionID
	if request.Mode == "judge" {
		start := time.Now().UnixNano()
		cmdStr := strings.Split(request.Command, " ")
        cmd :=  exec.Command(cmdStr[0], cmdStr[1:]...)
        pipe , _ := cmd.StdinPipe() 
        t , _ :=os.Open("cafecoderUsers/" + cmdResult.SessionID + "/testcase.txt")
        defer t.Close()
        defer pipe.Close()
        io.Copy(pipe,t)
		out, err := cmd.CombinedOutput()
        o , _ := os.Create("cafecoderUsers/"+cmdResult.SessionID+"/userStdout.txt") 
        defer o.Close()
        o.WriteString(string(fmt.Sprintf("%s",out)))
        e , _ := os.Create("cafecoderUsers/"+cmdResult.SessionID+"/userStderr.txt") 
        defer e.Close()
        e.WriteString(string( (fmt.Sprintf("%s",err) )))
		end := time.Now().UnixNano()
		cmdResult.Time = (end - start) / int64(time.Millisecond)
		if err != nil {
			cmdResult.Result = false
			cmdResult.ErrMessage = string( fmt.Sprintf("%s",err)  )
		} else {
			cmdResult.Result = true
			cmdResult.ErrMessage = ""
		}
	} else {
		cmdStr := strings.Split(request.Command, " ")
		_, err := exec.Command(cmdStr[0], cmdStr[1:]...).CombinedOutput()
		if err != nil {
			cmdResult.Result = false 
			cmdResult.ErrMessage = string( fmt.Sprintf("%s",err)  )
		} else {
			cmdResult.Result = true
			cmdResult.ErrMessage = ""
		}
	}
	conn, err := net.Dial("tcp", "172.17.0.1:3344")
	b, err := json.Marshal(cmdResult)
	if err != nil {
		conn.Write([]byte("err marshal"))
	}
	conn.Write(b)
	conn.Close()
}
