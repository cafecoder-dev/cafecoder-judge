package langconf

import (
	"errors"
)

type LanguageConfig struct {
	FileName   string
	CompileCmd string
	ExecuteCmd string
}

// todo json 化
func LangConfig(lang string) (LanguageConfig, error) {
	langConfig := LanguageConfig{}

	switch lang {
	case "c17_gcc10": //C17
		langConfig.CompileCmd = "gcc-10 Main.c -O2 -lm -std=gnu17 -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.c"
	case "cpp17_gcc10": //C++17
		langConfig.CompileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++17 -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cpp"
	case "cpp17_gcc10_acl": //C++17 + ACL
		langConfig.CompileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++17 -I . -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cpp"
	case "cpp20_gcc10": //C++20
		langConfig.CompileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++2a -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cpp"
	case "java11": //java11
		langConfig.CompileCmd = "javac -encoding UTF-8 Main.java 2> userStderr.txt"
		langConfig.ExecuteCmd = "java Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.java"
	case "python38": //python3
		langConfig.CompileCmd = "python3 -m py_compile Main.py 2> userStderr.txt"
		langConfig.ExecuteCmd = "python3 Main.py < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.py"
	case "cs_mono6": //C#
		langConfig.CompileCmd = "mcs Main.cs -out:Main.exe 2> userStderr.txt"
		langConfig.ExecuteCmd = "mono Main.exe < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cs"
	case "cs_dotnet31": // C#
		langConfig.CompileCmd = "cd Main && dotnet new console && mv ./../Main.cs Program.cs && dotnet publish -c Release --nologo -v q -o . 2> ../userStderr.txt && cd /"
		langConfig.ExecuteCmd = "dotnet ./Main/Main.dll < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cs"
	case "go_115": //golang
		langConfig.CompileCmd = "mv Main.go Main && cd Main && go build Main.go 2> ../userStderr.txt"
		langConfig.ExecuteCmd = "./Main/Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.go"
	case "nim_14":
		langConfig.CompileCmd = "nim cpp -d:release --opt:speed --multimethods:on -o:Main.out Main.nim 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.nim"
	case "rust_147":
		langConfig.CompileCmd = "rustc -O -o Main.out Main.rs 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.rs"
	case "ruby_27":
		langConfig.CompileCmd = "ruby -w -c ./Main.rb 2> userStderr.txt"
		langConfig.ExecuteCmd = "ruby ./Main.rb < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.rb"
	case "kotlin_14":
		langConfig.CompileCmd = "kotlinc ./Main.kt -include-runtime -d Main.jar 2> userStderr.txt"
		langConfig.ExecuteCmd = "kotlin Main.jar < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.kt"
	case "fortlan_10":
		langConfig.CompileCmd = "gfortran -O2 Main.f90 -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.f90"
	case "Perl_5_30_0":
		langConfig.CompileCmd = "perl -c Main.pl"
		langConfig.ExecuteCmd = "perl Main.pl"
		langConfig.FileName = "Main.pl"

	default:
		return langConfig, errors.New("undefined language")
	}

	return langConfig, nil
}
