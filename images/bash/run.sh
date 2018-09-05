#!/bin/bash
if [ ! -e stdout ]; then
    mkfifo stdout
fi
if [ ! -e stderr ]; then
    mkfifo stderr
fi
(cat < stdout) | awk '{print "stdout:"$0 > "/dev/stdout";fflush()}'&
(cat < stderr) | awk '{print "stderr:"$0 > "/dev/stdout";fflush()}'&
timeout 3 bash ./main.sh 1> stdout 2> stderr
status_code=$?
rm stdout&
rm stderr&
if [ ${status_code} -ne 0 ];then
	echo "Exit Code: ${status_code}"
	exit 0
fi
