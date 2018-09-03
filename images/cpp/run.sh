#!/bin/bash
g++ -std=c++1z -ggdb -o ./main ./main.cpp | awk '{print "g++:$0";fflush()}'
status_code=$?
if [ ${status_code} -ne 0 ];then
	echo "Exit Code: ${status_code}"
	exit 0
fi
if [ ! -e stdout ]; then
    mkfifo stdout
fi
if [ ! -e stderr ]; then
    mkfifo stderr
fi
(cat < stdout) | awk '{print "stdout:"$0 > "/dev/stdout";fflush()}'&
(cat < stderr) | awk '{print "stderr:"$0 > "/dev/stdout";fflush()}'&
timeout 3 ./main 1> stdout 2> stderr
status_code=$?
rm stdout&
rm stderr&
if [ ${status_code} -ne 0 ];then
	echo "Exit Code: ${status_code}"
	if [ ${status_code} -eq 139 ];then
		gdb main core --batch | awk '{print "gdb:$0";fflush()}'
	fi
	exit 0
fi
