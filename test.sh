#!/bin/sh

# I've written a test *shell* script instead of a more idiomatic" main_test.go" file because I wanted to do end to end testing, and a primary design goal of mine was to make the pubsub service friendly to use from `curl`, so I thought it would make sense to use curl.

cd $(dirname $0)

set -e

/bin/echo -n Checking for presence of curl and jq...
which curl > /dev/null
which jq > /dev/null
echo found

data_dir=./data
exit_status=0

./pubsubd --data-dir $data_dir&
pid=$!
echo Started pubsubd service \(PID $pid\), waiting a second
sleep 1

echo Creating subscription sub0 by requesting zero messages
curl -D - -X GET "http://localhost:8080/pull?sub=sub0&n=0" 2> /dev/null > /dev/null

echo Sending ten messages:
curl -D - -X POST \
    -d "message=foo&message=bar&message=john&message=paul&message=george&message=ringo&message=six&message=seven&message=eight&message=nine&message=ten" \
    http://localhost:8080/send \
    2> /dev/null > /dev/null

echo Implicitly creating sub1 by pulling up to ten messages \(but will receive zero\)
n_messages=$(curl "http://localhost:8080/pull?sub=sub1&n=10" 2> /dev/null | jq .n_messages)
if [ $n_messages != 0 ];
then 
    echo FAILURE: Expected 0 remaining messages but got ${n_messages}
    exit_status=1
else 
    echo SUCCESS: found zero remaining messages
fi

echo Subscription sub0 acknowledges message 0
curl -D - -X POST \
    -d "sub=sub0&id=0" \
    http://localhost:8080/ack \
    2> /dev/null > /dev/null


echo Subscription sub0 acknowledges messages 1-9
curl -D - -X POST \
    -d "sub=sub0&id=1&id=2&id=3&id=4&id=5&id=6&id=7&id=8&id=9" \
    http://localhost:8080/ack \
    2> /dev/null > /dev/null


echo Verifying one message remains unacked for sub0
n_messages=$(curl "http://localhost:8080/pull?sub=sub0&n=10" 2> /dev/null | jq .n_messages)
if [ $n_messages != 1 ];
then 
    echo FAILURE: Expected 1 remaining message but got ${n_messages}
    exit_status=1
else 
    echo SUCCESS: Found one remaining message
fi

echo Killing pubsubd
kill $pid > /dev/null 2> /dev/null
rm -rf $data_dir
exit $exit_status
