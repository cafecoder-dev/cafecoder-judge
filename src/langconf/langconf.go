package langconf

import (
	"errors"

	"github.com/cafecoder-dev/cafecoder-judge/src/types"
)

func LangConfig(submit *types.SubmitT) error {
	var err error

	switch submit.Info.Lang {
	case "c17_gcc10": //C17
		submit.CompileCmd = "gcc-10 Main.c -O2 -lm -std=gnu17 -o Main.out 2> userStderr.txt"
		submit.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.c"
	case "cpp17_gcc10": //C++17
		submit.CompileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++17 -o Main.out 2> userStderr.txt"
		submit.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.cpp"
	case "cpp17_gcc10_acl": //C++17 + ACL
		submit.CompileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++17 -I . -o Main.out 2> userStderr.txt"
		submit.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.cpp"
	case "cpp20_gcc10": //C++20
		submit.CompileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++2a -o Main.out 2> userStderr.txt"
		submit.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.cpp"
	case "java11": //java11
		submit.CompileCmd = "javac -encoding UTF-8 Main.java 2> userStderr.txt"
		submit.ExecuteCmd = "java Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.java"
	case "python38": //python3
		submit.CompileCmd = "python3 -m py_compile Main.py 2> userStderr.txt"
		submit.ExecuteCmd = "python3 Main.py < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.py"
	case "cs_mono6": //C#
		submit.CompileCmd = "mcs Main.cs -out:Main.exe 2> userStderr.txt"
		submit.ExecuteCmd = "mono Main.exe < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.cs"
	case "cs_dotnet31": // C#
		submit.CompileCmd = "cd Main && dotnet new console && mv ./../Main.cs Program.cs && dotnet publish -c Release --nologo -v q -o . 2>> ../userStderr.txt && cd /"
		submit.ExecuteCmd = "dotnet ./Main/Main.dll < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.cs"
	case "go_114": //golang
		submit.CompileCmd = "export PATH=$PATH:/usr/local/go/bin && mv Main.go Main && cd Main && go build Main.go 2> ../userStderr.txt"
		submit.ExecuteCmd = "./Main/Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.go"
	case "nim":
		submit.CompileCmd = "nim cpp -d:release --opt:speed --multimethods:on -o:Main.out Main.nim 2> userStderr.txt"
		submit.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.nim"
	case "rust_115":
		submit.CompileCmd = "export PATH=\"$HOME/.cargo/bin:$PATH\" 2> userStderr.txt && rustc -O -o Main.out Main.rs 2> userStderr.txt"
		submit.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		submit.FileName = "Main.rs"
	default:
		err = errors.New("undefined language")
	}

	return err
}
