#!/bin/bash

if [ $# -ne 2 ]; then
    echo "few or much args.need 2 args." 1>&2
    exit 1
fi
chmod -R 777 cafecoderUsers/$2


ts=$(date +%s%N)

case $1 in
    0 ) sudo -u rbash_user timeout 3 ./cafecoderUsers/$2/Main.out < cafecoderUsers/$2/testcase.txt > cafecoderUsers/$2/userStdout.txt 2> cafecoderUsers/$2/userStderr.txt;;#c11
    1 ) sudo -u rbash_user timeout 3 ./cafecoderUsers/$2/Main.out < cafecoderUsers/$2/testcase.txt > cafecoderUsers/$2/userStdout.txt 2> cafecoderUsers/$2/userStderr.txt;;#c++17
    2 ) sudo -u rbash_user timeout 3 java -cp ./cafecoderUsers/$2/ Main < cafecoderUsers/$2/testcase.txt > cafecoderUsers/$2/userStdout.txt 2> cafecoderUsers/$2/userStderr.txt;;#java8
    3 ) sudo -u rbash_user timeout 3 python3 /cafecoderUsers/$2/Main.py < cafecoderUsers/$2/testcase.txt > cafecoderUsers/$2/userStdout.txt 2> cafecoderUsers/$2/userStderr.txt;;#python3
    4 ) sudo -u rbash_user timeout 3 mono ./cafecoderUsers/$2/Main.exe < cafecoderUsers/$2/testcase.txt > cafecoderUsers/$2/userStdout.txt 2> cafecoderUsers/$2/userStderr.txt;;#c#
esac
rm cafecoderUsers/$2/testcase.txt

tt=$((($(date +%s%N) - $ts)/1000000))
echo -n "$tt" > cafecoderUsers/$2/userTime.txt