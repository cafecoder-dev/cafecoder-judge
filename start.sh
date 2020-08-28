#!/bin/bash

service docker start
./cafecoder-judge 2&>> log.txt
# nohup ./cafecoder-judge 2&>> log.txt
