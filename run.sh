#!/bin/bash

# build
go build ping.go

# run
sudo ./ping -s 40 www.google.com
