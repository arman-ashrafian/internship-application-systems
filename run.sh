#!/bin/bash

# build
go build ping.go

# run
sudo ./ping -t 60 www.google.com  
