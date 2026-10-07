package main

// Regenerate the checked-in Windows executable icons from the README image.
//go:generate go run github.com/tc-hib/go-winres@v0.3.3 simply --icon ../../infinicave.png --arch amd64,386,arm64 --manifest none --out rsrc
