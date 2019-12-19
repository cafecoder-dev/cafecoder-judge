package main

import (
	"encoding/base64"
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
	if err != nil {
		cmdResult.ErrMessage = err.Error()
	}
	buf := make([]byte, 65536)
	for {
		n, err := stderrFp.Read(buf)
		if n == 0 {
			break
		}
		if err != nil {
			cmdResult.ErrMessage = err.Error()

			break
		}
		cmdResult.ErrMessage = base64.StdEncoding.EncodeToString(buf[:n])
		//cmdResult.ErrMessage = string(buf[:n])
	}
	stderrFp.Close()
}

func executeJudge(request requestJSON) {
	var cmdResult cmdResultJSON
	cmdResult.SessionID = request.SessionID
	if request.Mode == "judge" {
		os.Mkdir("cafecoderUsers/"+request.SessionID, 0777)
		cmd := exec.Command("sh", "-c", request.Command+" > cafecoderUsers/"+request.SessionID+"/userStdout.txt"+" 2> cafecoderUsers/"+request.SessionID+"/userStderr.txt")

		start := time.Now().UnixNano()
		err := cmd.Run()
		end := time.Now().UnixNano()
		cmdResult.Time = (end - start) / int64(time.Millisecond)
		if err != nil {
			cmdResult.Result = false
			cmdResult.ErrMessage = string(fmt.Sprintf("%s", err))
		} else {
			cmdResult.Result = true
			cmdResult.ErrMessage = ""
		}

		readError(&cmdResult)

	} else {
		err := exec.Command("sh", "-c", request.Command+" > cafecoderUsers/"+request.SessionID+"/userStdout.txt"+" 2> cafecoderUsers/"+request.SessionID+"/userStderr.txt").Run()
		if err != nil {
			cmdResult.Result = false
			cmdResult.ErrMessage = err.Error()

		} else {
			cmdResult.Result = true
			cmdResult.ErrMessage = ""
		}

		readError(&cmdResult)
	}

	conn, err := net.Dial("tcp", "133.130.112.219:3344")
	b, err := json.Marshal(cmdResult)
	if err != nil {
		conn.Write([]byte("err marshal"))
	}
	conn.Write(b)
	conn.Close()

}
