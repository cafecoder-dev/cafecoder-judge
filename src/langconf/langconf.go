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
func LangConfig(langID string) (LanguageConfig, error) {
	/*
		var languageConfigs []types.LanguageConfigJSON
		bytes, err := ioutil.ReadFile("language_configs.json")
		if err != nil {
			return LanguageConfig{}, err
		}

		if err := json.Unmarshal(bytes, &languageConfigs); err != nil {
			return LanguageConfig{}, err
		}

		for _, langConf := range languageConfigs {
			if langConf.Name == langID {
				return LanguageConfig{
					FileName:   langConf.Name,
					CompileCmd: langConf.CompileCmd,
					ExecuteCmd: langConf.ExecuteCmd,
				}, nil
			}
		}

		return LanguageConfig{}, errors.New("undefine language")
	*/

	langConfig := LanguageConfig{}

	switch langID {
	case "c17_gcc:10.2.0": //C17
		langConfig.CompileCmd = "gcc-10 Main.c -O2 -lm -std=gnu17 -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.c"
	case "cpp17_gcc:10.2.0": //C++17
		langConfig.CompileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++17 -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cpp"
	case "cpp17-acl_gcc:10.2.0": //C++17 + ACL
		langConfig.CompileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++17 -I . -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cpp"
	case "cpp20_gcc:10.2.0": //C++20
		langConfig.CompileCmd = "g++-10 Main.cpp -O2 -lm -std=gnu++2a -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cpp"
	case "java:11.0.9": //java11
		langConfig.CompileCmd = "javac -encoding UTF-8 Main.java 2> userStderr.txt"
		langConfig.ExecuteCmd = "java Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.java"
	case "python:3.9.0": //python3
		langConfig.CompileCmd = "python3.9 -m py_compile Main.py 2> userStderr.txt"
		langConfig.ExecuteCmd = "python3.9 Main.py < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.py"
	case "pypy3:7.3.3": //pypy3
		langConfig.CompileCmd = "pypy3 -m py_compile Main.py 2> userStderr.txt"
		langConfig.ExecuteCmd = "pypy3 Main.py < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.py"
	case "cs_mono:6.12.0.90": //C#
		langConfig.CompileCmd = "source ~/.profile && mcs Main.cs -out:Main.exe 2> userStderr.txt"
		langConfig.ExecuteCmd = "source ~/.profile && mono Main.exe < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cs"
	case "cs_dotnet:5.0": // C#
		langConfig.CompileCmd = "source ~/.profile && cd Main && dotnet new console && mv ./../Main.cs Program.cs && dotnet publish -c Release --nologo -v q -o . 2> ../userStderr.txt && cd /"
		langConfig.ExecuteCmd = "source ~/.profile && dotnet ./Main/Main.dll < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cs"
	case "go:1.15.5": //golang
		langConfig.CompileCmd = "source ~/.profile && mv Main.go Main && cd Main && go build Main.go 2> ../userStderr.txt"
		langConfig.ExecuteCmd = "./Main/Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.go"
	case "nim:1.4.0":
		langConfig.CompileCmd = "source ~/.profile && nim cpp -d:release --opt:speed --multimethods:on -o:Main.out Main.nim 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.nim"
	case "rust:1.48.0":
		langConfig.CompileCmd = "source ~/.profile && cd rust_workspace && mv /Main.rs ./src/main.rs && cargo build --release 2> /userStderr.txt && cd /"
		langConfig.ExecuteCmd = "./rust_workspace/target/release/Rust < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.rs"
	case "ruby:2.7.2":
		langConfig.CompileCmd = "source ~/.profile && ruby -w -c ./Main.rb 2> userStderr.txt"
		langConfig.ExecuteCmd = "source ~/.profile && ruby ./Main.rb < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.rb"
	case "kotlin:1.4.10":
		langConfig.CompileCmd = "kotlinc ./Main.kt -include-runtime -d Main.jar 2> userStderr.txt"
		langConfig.ExecuteCmd = "kotlin Main.jar < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.kt"
	case "fortran:10.2.0":
		langConfig.CompileCmd = "gfortran-10 -O2 Main.f90 -o Main.out 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.out < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.f90"
	case "perl:5.30.0":
		langConfig.CompileCmd = "perl -c Main.pl 2> userStderr.txt"
		langConfig.ExecuteCmd = "perl Main.pl < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.pl"
	case "raku:2020.10":
		langConfig.CompileCmd = "source ~/.profile && perl6 -c Main.pl 2> userStderr.txt"
		langConfig.ExecuteCmd = "source ~/.profile && perl6 Main.pl < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.p6"
	case "crystal:0.35.1":
		langConfig.CompileCmd = "crystal build Main.cr 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.cr"
	case "text_cat:8.30":
		langConfig.CompileCmd = ":"
		langConfig.ExecuteCmd = "cat Main.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.txt"
	case "bash:5.0.17":
		langConfig.CompileCmd = "bash -n Main.sh 2> userStderr.txt"
		langConfig.ExecuteCmd = "./Main.sh < testcase.txt > userStdout.txt 2> userStderr.txt"
		langConfig.FileName = "Main.sh"
	default:
		return langConfig, errors.New("undefined language")
	}

	return langConfig, nil
}
