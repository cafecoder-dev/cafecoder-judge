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

func readError(cmdResult *cmdResultJSON) {
	stderrFp, err := os.Open("cafecoderUsers/" + cmdResult.SessionID + "/userStderr.txt")
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
		os.Mkdir("cafecoderUsers/"+request.SessionID, 0777)
		cmd := exec.Command("sh", "-c", request.Command+"< cafecoderUsers/"+request.SessionID+"/testcase.txt > cafecoderUsers/"+request.SessionID+"/userStdout.txt"+" 2> cafecoderUsers/"+request.SessionID+"/userStderr.txt")

		start := time.Now().UnixNano()
		err := cmd.Run()
		end := time.Now().UnixNano()
		cmdResult.Time = (end - start) / int64(time.Millisecond)
		if err != nil {
			cmdResult.Result = false
		} else {
			cmdResult.Result = true
		}

		readError(&cmdResult)

	} else {
		os.Mkdir("cafecoderUsers/"+request.SessionID, 0777)
		err := exec.Command("sh", "-c", request.Command+" > cafecoderUsers/"+request.SessionID+"/userStdout.txt"+" 2> cafecoderUsers/"+request.SessionID+"/userStderr.txt").Run()
		if err != nil {
			cmdResult.Result = false

		} else {
			cmdResult.Result = true
		}

		readError(&cmdResult)
	}

	conn, err := net.Dial("tcp", "172.17.0.1:3344")
	b, err := json.Marshal(cmdResult)
	if err != nil {
		conn.Write([]byte("err marshal"))
	}
	conn.Write(b)
	conn.Close()

}
