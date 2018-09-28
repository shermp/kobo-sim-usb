# kobo-sim-usb
kobo-sim-usb provides those developing on Kobo devices a means to enter the Kobo USBMS mode, with Wifi enabled, and the internal memory remounted.

It is currently ALPHA software. It works, but there are no guarantees it is completely safe and will not corrupt your user partition.

## Installation & Usage
kobo-usb-sim requires CGO to be enabled. A GCC ARM cross compiler is also required. The following environment variables need to be set as well:
```
GOOS=linux
GOARCH=arm
CGO_ENABLED=1

# Set C cross compiler variables
CC=/home/<user>/x-tools/arm-kobo-linux-gnueabihf/bin/arm-kobo-linux-gnueabihf-gcc
CXX=/home/<user>/x-tools/arm-kobo-linux-gnueabihf/bin/arm-kobo-linux-gnueabihf-g++
```

kobo-usb-sim can be obtained using go get:
```
go get github.com/shermp/kobo-sim-usb/...
```
Note that `go-fbink-v2` is also required, however, `go get` should resolve this dependency.

Refer to `example/main.go` for a basic usage example.