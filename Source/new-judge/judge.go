package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"

	"./tftpwrapper"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"pack.ag/tftp"
)

const (
	//BackendHostPort ... appear IP-address and port-number
	BackendHostPort = "localhost:5963"
)

type commandChicket struct {
	sync.Mutex
	channel map[string]chan cmdResultJSON
}

type cmdResultJSON struct {
	SessionID  string `json:"sessionID"`
	Time       int64  `json:"time"`
	Result     bool   `json:"result"`
	ErrMessage string `json:"errMessage"`
}

type testcaseJSON struct {
	Name       string `json:"name"`
	Result     string `json:"result"`
	MemoryUsed int64  `json:"memory_used"`
	Time       int64  `json:"time"`
}

type resultJSON struct {
	SessionID  string         `json:"sessionID"`
	Time       int64          `json:"time"`
	Result     string         `json:"result"`
	ErrMessage string         `json:"errMessage"`
	TestcaseN  int            `json:"testcaseN"`
	Testcases  []testcaseJSON `json:"testcases"`
}

type requestJSON struct {
	SessionID string `json:"sessionID"`
	Cmd       string `json:"cmd"`
	Mode      string `json:"mode"` //Mode ... "judge" or "others"
	DirName   string `json:"dirName"`
}

type submitT struct {
	SessionID    string `json:"sessionID"`
	Language     string `json:"language"`
	TestcasePath string `json:"testcasePath"`
	CodePath     string `json:"codePath"`
	TimeLimit    int64  `json:"timeLimit"` //[sec]

	dirName     string
	extension   string
	compileCmd  string
	executeCmd  string
	executePath string
	code        []byte

	containerID      string
	containerCli     *client.Client
	containerInspect types.ContainerJSON

	resultBuffer *bytes.Buffer
	errorBuffer  *bytes.Buffer
}

func main() {
	commandChickets := commandChicket{channel: make(map[string]chan cmdResultJSON)}
	go manageCmds(&commandChickets)

	listen, err := net.Listen("tcp", "0.0.0.0:8888")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	tftpCli, err := tftp.NewClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
	}
	for {
		cnct, err := listen.Accept()
		if err != nil {
			continue //continue to receive request
		}
		var submit = submitT{errorBuffer: new(bytes.Buffer), resultBuffer: new(bytes.Buffer)}
		json.NewDecoder(cnct).Decode(&submit)
		cnct.Close()

		if _, exist := commandChickets.channel[submit.SessionID]; exist {
			fmt.Fprintf(os.Stderr, "%s has already existed:(\n", submit.SessionID)
		} else {
			commandChickets.channel[submit.SessionID] = make(chan cmdResultJSON)
			go judge(submit, &tftpCli, &commandChickets.channel)
		}
	}
}

func judge(submit submitT, tftpCli **tftp.Client, commandChickets *map[string]chan cmdResultJSON) {
	var (
		result resultJSON
	)
	priority := map[string]int{"AC": 0, "WA": 1, "TLE": 2, "RE": 3, "CE": 4, "IE": 5}

	result.SessionID = submit.SessionID

	if !validationChack(submit) {
		result.Result = "IE"
		fmt.Fprintf(submit.errorBuffer, "validationChack() Error\n")
		sendResult(&result, submit)
		return
	}

	sessionIDChan := (*commandChickets)[submit.SessionID]
	defer func() { delete((*commandChickets), submit.SessionID) }()
	hash := sha256.Sum256([]byte(submit.SessionID))
	submit.dirName = hex.EncodeToString(hash[:])
	setLanguage(&submit)

	//download file
	submit.code = tftpwrapper.DownloadFromPath(tftpCli, submit.CodePath)

	os.Mkdir("cafecoderUsers/"+submit.dirName, 0777)
	file, _ := os.Create("cafecoderUsers/" + submit.dirName + "/" + submit.dirName)
	file.Write(submit.code)
	file.Close()
	defer os.Remove("cafecoderUsers/" + submit.dirName)

	err := createContainer(&submit)
	if err != nil {
		result.Result = "IE"
		fmt.Fprintf(submit.errorBuffer, "%s\n", err.Error())
		sendResult(&result, submit)
		return
	}
	defer operateContainer(submit)
	/*
		containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
		if err != nil {
			result.Result = "IE"
		}
	*/

	recv, err := reqCmd(&sessionIDChan, submit, "mkdir -p cafecoderUsers/"+submit.dirName, "others")
	if !recv.Result || err != nil {
		result.Result = "IE"
		fmt.Fprintf(submit.errorBuffer, "%s\n", err.Error())
		sendResult(&result, submit)
		return
	}

	tarCopy(
		submit, "cafecoderUsers/"+submit.dirName+"/"+submit.dirName,
		"cafecoderUsers/"+submit.dirName+"/Main"+submit.extension,
	)

	err = compile(submit, &sessionIDChan, &result)
	if result.Result == "CE" || err != nil {
		//finish judge...
		fmt.Fprintf(submit.errorBuffer, "%s\n", err.Error())
		sendResult(&result, submit)
		return
	}

	err = tryTestcase(submit, &sessionIDChan, &result)
	if err != nil {
		result.Result = "IE"
		fmt.Fprintf(submit.errorBuffer, "%s\n", err.Error())
		sendResult(&result, submit)
	}

	for i := 0; i < result.TestcaseN; i++ {
		if result.Time < result.Testcases[i].Time {
			result.Time = result.Testcases[i].Time
		}
		if priority[result.Result] < priority[result.Testcases[i].Result] {
			result.Result = result.Testcases[i].Result
		}
	}
	sendResult(&result, submit)

	return
}

func sendResult(result *resultJSON, submit submitT) {
	result.ErrMessage = base64.StdEncoding.EncodeToString([]byte(submit.errorBuffer.String()))
	b, _ := json.Marshal(*result)
	back := submitT{resultBuffer: new(bytes.Buffer)}
	fmt.Fprintf(back.resultBuffer, "%s\n", string(b))
	passResultTCP(back, BackendHostPort)
}

func passResultTCP(submit submitT, hostAndPort string) {
	conn, err := net.Dial("tcp", hostAndPort)
	if err != nil {
		fmt.Println(err)
		return
	}
	passStr := strings.Trim(submit.resultBuffer.String(), "\n")
	fmt.Println(passStr)
	conn.Write([]byte(passStr))
	conn.Close()
}

func tryTestcase(submit submitT, sessionIDChan *chan cmdResultJSON, result *resultJSON) error {
	var testcaseName [100]string

	testcaseList, err := os.Open(submit.TestcasePath + "/testcase_list.txt")
	if err != nil {
		return err
	}

	sc := bufio.NewScanner(testcaseList)
	for sc.Scan() {
		testcaseName[result.TestcaseN] = sc.Text()
		result.TestcaseN++
	}
	testcaseList.Close()

	for i := 0; i < result.TestcaseN; i++ {
		result.Testcases[i].Name = strings.TrimSpace(testcaseName[i])
		outputTestcase, err := ioutil.ReadFile(submit.TestcasePath + "/out/" + result.Testcases[i].Name)
		if err != nil {
			return err
		}
		tarCopy(submit, submit.TestcasePath+"/in/"+result.Testcases[i].Name, "/cafecoderUsers/"+submit.dirName+"/testcase.txt")

		recv, err := reqCmd(sessionIDChan, submit, submit.executeCmd+" < "+submit.TestcasePath+"/in/"+testcaseName[i]+" > cafecoderUsers/"+submit.dirName+"/stdout.txt 2> cafecoderUsers/"+submit.dirName+"/stderr.txt", "judge")
		if err != nil {
			return err
		}

		userStdoutReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.SessionID, "cafecoderUsers/"+submit.dirName+"/userStdout.txt")
		if err != nil {
			return err
		}
		tr := tar.NewReader(userStdoutReader)
		tr.Next()
		userStdout := new(bytes.Buffer)
		userStdout.ReadFrom(tr)
		userStderrReader, _, err := submit.containerCli.CopyFromContainer(context.TODO(), submit.SessionID, "cafecoderUsers/"+submit.dirName+"/userStderr.txt")
		if err != nil {
			return err
		}
		tr = tar.NewReader(userStderrReader)
		tr.Next()
		userStderr := new(bytes.Buffer)
		userStderr.ReadFrom(tr)

		userStdoutLines := strings.Split(userStdout.String(), "\n")
		userStderrLines := strings.Split(userStderr.String(), "\n")
		outputTestcaseLines := strings.Split(string(outputTestcase), "\n")

		if !recv.Result {
			result.Testcases[i].Result = "RE"
			for j := 0; j < len(userStderrLines); j++ {
				result.ErrMessage = userStderrLines[j]
			}
			continue
		}

		result.Testcases[i].Time = recv.Time
		if result.Testcases[i].Time > submit.TimeLimit*1000 {
			result.Testcases[i].Result = "TLE"
			continue
		}

		result.Testcases[i].Result = "WA"
		for j := 0; j < len(userStdoutLines) && j < len(outputTestcaseLines); j++ {
			result.Testcases[i].Result = "AC"
			if strings.TrimSpace(string(userStdoutLines[j])) != strings.TrimSpace(string(outputTestcaseLines[j])) {
				result.Testcases[i].Result = "WA"
				break
			}
		}
	}
	return nil
}

func compile(submit submitT, sessionIDChan *chan cmdResultJSON, result *resultJSON) error {
	println("wait for compile...")
	recv, err := reqCmd(sessionIDChan, submit, submit.compileCmd, "others")
	if err != nil {
		result.Result = "IE"
		return err
	}
	println("compile done")
	fmt.Fprintf(submit.errorBuffer, "%s\n", recv.ErrMessage)
	if !recv.Result {
		result.Result = "CE"
	}

	recv, err = reqCmd(sessionIDChan, submit, "chown rbash_user "+submit.executePath, "others")
	if err != nil {
		result.Result = "IE"
		return err
	}
	fmt.Fprintf(submit.errorBuffer, "%s\n", recv.ErrMessage)

	return nil
}

func validationChack(submit submitT) bool {
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, submit.CodePath) {
		return false
	}
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, submit.Language) {
		return false
	}
	if !checkRegexp(`[(A-Za-z0-9\./_\/)]*`, submit.TestcasePath) {
		return false
	}
	return true
}

func tarCopy(submit submitT, localFilePath string, containerFilePath string) {
	File, _ := os.Open(localFilePath)
	content, _ := ioutil.ReadAll(File)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{
		Name: containerFilePath,   // filename
		Mode: 0777,                // permissions
		Size: int64(len(content)), // filesize
	})
	tw.Write(content)
	tw.Close()
	submit.containerCli.CopyToContainer(
		context.TODO(), submit.containerID,
		"/",
		&buf, types.CopyToContainerOptions{},
	)
	File.Close()
}

func reqCmd(sessionIDChan *chan cmdResultJSON, submit submitT, cmd string, mode string) (cmdResultJSON, error) {
	var request = requestJSON{
		SessionID: submit.SessionID,
		Cmd:       cmd,
		Mode:      mode,
		DirName:   submit.dirName,
	}
	var recv cmdResultJSON

	b, _ := json.Marshal(request)
	containerConn, err := net.Dial("tcp", submit.containerInspect.NetworkSettings.IPAddress+":8887")
	if err != nil {
		return recv, err
	}
	containerConn.Write(b)
	containerConn.Close()
	for {
		recv = <-*sessionIDChan
		if submit.SessionID == recv.SessionID {
			break
		}
	}
	return recv, nil
}

func createContainer(submit *submitT) error {
	var err error
	submit.containerCli, err = client.NewClientWithOpts(client.WithVersion("1.35"))
	defer submit.containerCli.Close()
	if err != nil {
		return err
	}
	config := &container.Config{
		Image: "cafecoder",
	}
	resp, err := submit.containerCli.ContainerCreate(context.TODO(), config, nil, nil, strings.TrimSpace(submit.SessionID))
	if err != nil {
		return err
	}
	submit.containerID = resp.ID
	err = submit.containerCli.ContainerStart(context.TODO(), submit.containerID, types.ContainerStartOptions{})
	if err != nil {
		return err
	}
	//get container IP address
	submit.containerInspect, _ = submit.containerCli.ContainerInspect(context.TODO(), submit.containerID)

	return nil
}

func operateContainer(submit submitT) {
	submit.containerCli.ContainerStop(context.Background(), submit.containerID, nil)
	submit.containerCli.ContainerRemove(context.Background(), submit.containerID, types.ContainerRemoveOptions{RemoveVolumes: true, RemoveLinks: true, Force: true})
	labelFilters := filters.NewArgs()
	submit.containerCli.ContainersPrune(context.Background(), labelFilters)
	fmt.Println("container " + submit.SessionID + " removed")
}

/*Compile Cmds & Execute Cmds*/
//if you want to add new language, please write language's compile cmd & execute cmd.
func setLanguage(submit *submitT) {
	if submit.Language == "c11" {
		submit.extension = ".c"
		submit.compileCmd = "gcc cafecoderUsers/" + submit.dirName + "/Main.c -std=gnu11 -o cafecoderUsers/" + submit.dirName + "/Main.out"
		submit.executeCmd = "timeout " + string(submit.TimeLimit) + " ./cafecoderUsers/" + submit.dirName + "/Main.out"
		submit.executePath = "cafecoderUsers/" + submit.dirName + "Main.out"
	} else if submit.Language == "cpp17" {
		submit.extension = ".cpp"
		submit.compileCmd = "g++ cafecoderUsers/" + submit.dirName + "/Main.cpp -std=gnu++17 -o cafecoderUsers/" + submit.dirName + "/Main.out"
		submit.executeCmd = "timeout " + string(submit.TimeLimit) + " ./cafecoderUsers/" + submit.dirName + "/Main.out"
		submit.executePath = "cafecoderUsers/" + submit.dirName + "Main.out"
	} else if submit.Language == "cpp20" {
		submit.extension = ".cpp"
		submit.compileCmd = "g++ cafecoderUsers/" + submit.dirName + "/Main.cpp -std=gnu++2a -o cafecoderUsers/" + submit.dirName + "/Main.out"
		submit.executeCmd = "timeout " + string(submit.TimeLimit) + " ./cafecoderUsers/" + submit.dirName + "/Main.out"
		submit.executePath = "cafecoderUsers/" + submit.dirName + "Main.out"
	} else if submit.Language == "java11" {
		submit.extension = ".java"
		submit.compileCmd = "javac cafecoderUsers/" + submit.dirName + "/Main.java -d cafecoderUsers/" + submit.dirName
		submit.executeCmd = "timeout " + string(submit.TimeLimit) + " java -cp /cafecoderUsers/" + submit.dirName + " Main"
		submit.executePath = "cafecoderUsers/" + submit.dirName + "Main.class"
	} else if submit.Language == "python3" {
		submit.extension = ".py"
		submit.compileCmd = "python3 -m py_compile cafecoderUsers/" + submit.dirName + "/Main.py"
		submit.executeCmd = "timeout " + string(submit.TimeLimit) + " python3 cafecoderUsers/" + submit.dirName + "/Main.py"
		submit.executePath = "cafecoderUsers/" + submit.dirName + "Main.py"

	}
}

func manageCmds(commandChickets *commandChicket) {
	var cmdResult cmdResultJSON

	listen, err := net.Listen("tcp", "0.0.0.0:3344")
	if err != nil {
		//...
	}
	for {
		cnct, err := listen.Accept()
		if err != nil {
			continue //continue to receive request
		}
		go func() {
			json.NewDecoder(cnct).Decode(&cmdResult)
			cnct.Close()
			println("Connection Closed")

			errmsgByte, _ := base64.StdEncoding.DecodeString(cmdResult.ErrMessage)
			cmdResult.ErrMessage = string(errmsgByte)
			go func() {
				(*commandChickets).channel[cmdResult.SessionID] <- cmdResult
			}()
		}()
	}
}

func checkRegexp(reg, str string) bool {
	return regexp.MustCompile(reg).Match([]byte(str))
}
