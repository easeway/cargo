#!/bin/sh
mkdir -p vendor/src/github.com/easeway
ln -sfT ../../../.. vendor/src/github.com/easeway/cargo
GOPATH=`pwd`/vendor go get -d .
mkdir -p bin
GOPATH=`pwd`/vendor go build -o bin/cargo .

