/*
	kobo-sim-usb - Enter USBMS mode for kobo devices
    Copyright (C) 2018 Sherman Perry

    This file is part of kobo-sim-usb.

    kobo-sim-usb is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    kobo-sim-usb is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU General Public License for more details.

    You should have received a copy of the GNU General Public License
    along with kobo-sim-usb.  If not, see <https://www.gnu.org/licenses/>.
*/

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
	err = u.Start(true, true)
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
