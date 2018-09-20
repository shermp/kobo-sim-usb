package main

import (
	"fmt"
	"os"
	"time"

	"github.com/shermp/go-fbink-v2/gofbink"
	"github.com/shermp/kobo-sim-usb/simusb"
)

func main() {
	cfg := gofbink.FBInkConfig{}
	rCfg := gofbink.RestrictedConfig{}
	rCfg.IsQuiet = true
	rCfg.Fontname = gofbink.UNSCII

	fb := gofbink.New(&cfg, &rCfg)
	fb.Open()
	defer fb.Close()
	fb.Init(&cfg)

	u, err := simusb.New(fb)
	if err != nil {
		fmt.Println(err)
	}
	err = u.Start(true)
	if err != nil {
		fmt.Println(err)
		return
	}
	wd, _ := os.Getwd()
	fmt.Println("Current dir is:", wd)
	fmt.Println("Sleeping for 10s")
	time.Sleep(10 * time.Second)
	fmt.Println("Leaving USBMS")
	err = u.End(true)
	if err != nil {
		fmt.Println(err)
	}
	wd, _ = os.Getwd()
	fmt.Println("Current dir is:", wd)
}
