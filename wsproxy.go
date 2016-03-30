/* go-websockproxy - https://github.com/gdm85/go-websockproxy
Copyright (C) 2016 gdm85

This program is free software; you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation; either version 2 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License along
with this program; if not, write to the Free Software Foundation, Inc.,
51 Franklin Street, Fifth Floor, Boston, MA 02110-1301 USA.
*/
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"

	flag "github.com/ogier/pflag"
	"github.com/songgao/water"
	"github.com/songgao/water/waterutil"
	"golang.org/x/net/websocket"
)

// Frame is a TAP ethernet frame (byte array).
type Frame []byte

// String returns a human-readable description for the TAP frame, with eventual IPv4 packet decoding if frame contains an IPv4 payload.
func (f Frame) String() string {
	if waterutil.MACEthertype(f) == waterutil.IPv4 {
		p := waterutil.MACPayload(f)
		return fmt.Sprintf("{%d bytes [%s](%s) -> [%s](%s) TTL=%d}", len(f), waterutil.MACSource(f), waterutil.IPv4Source(p), waterutil.MACDestination(f), waterutil.IPv4Destination(p), waterutil.IPv4TTL(p))
	}
	return fmt.Sprintf("{%d bytes [%s] -> [%s]}", len(f), waterutil.MACSource(f), waterutil.MACDestination(f))
}

// websocketHandler is the main websocekt connections handling entrypoint.
func websocketHandler(ws *websocket.Conn) {
	var flaggedAsBad bool
	client := hub.Add(ws)
	for {
		var frame []byte
		err := websocket.Message.Receive(ws, &frame)
		if err != nil {
			if err == io.EOF {
				// EOF is considered normal for a websocket closing the connection
				hub.Remove(client)
				return
			}
			WarningPrintf("client %v: dropping after read error: %v", client, err)
			hub.Remove(client)
			return
		}

		if flaggedAsBad {
			// discard all frames of this connection, but keep it open to mitigate many reconnections
			DebugPrintf("frame %v sent to /dev/null", frame)
			continue
		}

		///
		/// a new frame is available
		///

		if len(frame) < 12 {
			// this frame can't possibly be good
			WarningPrintf("client %v: skipping too short frame (%d bytes)", client, len(frame))
			continue
		}

		// special frames have an invalid source MAC made of 0s
		if string(frame[:6]) == "\x00\x00\x00\x00\x00\x00" {
			skipFrame, flagAsBad, err := client.HandleSpecialFrame(frame[6:])
			if err != nil {
				WarningPrintf("client %v, frame %v: %v", client, Frame(frame), err)
			}
			if flagAsBad {
				flaggedAsBad = true
				continue
			}
			if skipFrame {
				continue
			}
		}

		// discard frames of clients that are not authorized
		if !client.authorized {
			WarningPrintf("client %v, frame %v: discarding unauthorized", client, Frame(frame))
			if len(frame) < 60 {
				WarningPrintf("discarded: %s", string(frame))
			}
			continue
		}

		///
		/// not a special frame, parse as a normal TAP frame
		///

		// check if client can send this frame with its source MAC
		flagAsBad, err := hub.CanSourceMAC(client, waterutil.MACSource(frame))
		if err != nil {
			WarningPrintf("client %v, frame %v: %v", client, Frame(frame), err)
			if flagAsBad {
				flaggedAsBad = true
			}
			continue
		}

		switched, err := hub.SwitchFrame(client, frame)
		if err != nil {
			ErrorPrintf("client %v, frame %v: dropping client because of TAP switch error: %v", client, Frame(frame), err)
			hub.Remove(client)
			return
		}

		if !switched {
			DebugPrintf("client %v, frame %v: frame could not be switched")
		}
	}
}

func readTAPTraffic() error {
	frame := make([]byte, 1500+18)
	for {
		n, err := tap.Read(frame)
		if err != nil {
			return err
		}
		if n < 12 {
			WarningPrintf("discarding invalid frame with size of %d bytes read from TAP interface", n)
			continue
		}

		switched, err := hub.SwitchFrame(nil, frame)
		if err != nil {
			return err
		}

		if !switched {
			DebugPrintf("frame %v: could not switch from TAP interface", Frame(frame))
		}
	}
}

type PrintFunc func(string, ...interface{})

// ErrorPrintf prints a (formatted) error to standard error.
func ErrorPrintf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
}
func warningPrintf(format string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, "WARNING: "+format+"\n", a...)
}
func debugPrintf(format string, a ...interface{}) {
	fmt.Printf("DEBUG: "+format+"\n", a...)
}
func infoPrintf(format string, a ...interface{}) {
	fmt.Printf(format+"\n", a...)
}
func dummyPrintf(format string, a ...interface{}) {
	// do nothing
}

var (
	hub = NewHub()       // clients management hub
	tap *water.Interface // TAP interface
	// set of functions to provide CLI logging output
	DebugPrintf, InfoPrintf, WarningPrintf PrintFunc

	uploadBandwidth, downloadBandwidth int64
	// CLI options follow:
	logLevel             string
	listenAddress        string
	staticDirectory      string
	maxUploadBandwidth   string
	maxDownloadBandwidth string
	tapName              string // re-using an existing TAP is not yet supported
	tapIPv4              string
	authKey              string
	macPrefix            string
	certFile             string
	keyFile              string
)

func init() {
	flag.StringVar(&tapIPv4, "tap-ipv4", "10.3.0.1/16", "IPv4 address for the TAP interface; used only when interface is created")
	flag.StringVar(&maxUploadBandwidth, "max-upload-bandwidth", "", "max upload bandwidth per client; leave empty for unlimited")
	flag.StringVar(&maxDownloadBandwidth, "max-download-bandwidth", "", "max upload bandwidth per client; leave empty for unlimited")
	flag.StringVar(&listenAddress, "listen-address", ":8000", "address to listen on for incoming websocket connections; URI is '/wstap'")
	flag.StringVar(&staticDirectory, "static-directory", "", "static files directory to serve at '/'; disabled by default")
	flag.StringVar(&logLevel, "log-level", "warning", "one of 'debug', 'info', 'warning', 'error'")
	flag.StringVar(&authKey, "auth-key", "", "accept TAP traffic via websockets only if authorized with this key; by default is disabled (accepts any traffic)")
	flag.StringVar(&macPrefix, "mac-prefix", "", "accept websockets traffic only with MACs starting with the specified prefix (default is disabled)")
	flag.StringVar(&certFile, "cert-file", "", "certificate for listening on TLS connections; by default TLS is disabled")
	flag.StringVar(&keyFile, "key-file", "", "key file for listening on TLS connections; by default TLS is disabled")
}

func main() {
	flag.Parse()

	switch logLevel {
	case "debug":
		DebugPrintf = debugPrintf
		InfoPrintf = infoPrintf
		WarningPrintf = warningPrintf
	case "info":
		DebugPrintf = dummyPrintf
		InfoPrintf = infoPrintf
		WarningPrintf = warningPrintf
	case "warning":
		DebugPrintf = dummyPrintf
		InfoPrintf = dummyPrintf
		WarningPrintf = warningPrintf
	case "error":
		DebugPrintf = dummyPrintf
		InfoPrintf = dummyPrintf
		WarningPrintf = dummyPrintf
	default:
		ErrorPrintf("invalid log level specified")
		os.Exit(1)
	}

	if (certFile != "" && keyFile == "") || (keyFile != "" && certFile == "") {
		ErrorPrintf("both certificate and key file should be specified in order to enable TLS connections")
		os.Exit(2)
	}

	var err error
	uploadBandwidth, err = parseBandwidth(maxUploadBandwidth)
	if err != nil {
		ErrorPrintf("invalid upload bandwidth specified: %v", err)
		os.Exit(3)
	}
	downloadBandwidth, err = parseBandwidth(maxDownloadBandwidth)
	if err != nil {
		ErrorPrintf("invalid download bandwidth specified: %v", err)
		os.Exit(4)
	}

	tap, err = water.NewTAP(tapName)
	if err != nil {
		ErrorPrintf("creating TAP interface: %v", err)
		os.Exit(5)
	}
	if err := exec.Command("ip", "link", "set", tap.Name(), "up").Run(); err != nil {
		ErrorPrintf("bringing TAP interface up: %v", err)
	}
	if err := exec.Command("ip", "addr", "add", tapIPv4, "brd", "+", "dev", tap.Name()).Run(); err != nil {
		ErrorPrintf("configuring TAP interface IPv4: %v", err)
	}
	InfoPrintf("device %s is up with IPv4 %s", tap.Name(), tapIPv4)

	if staticDirectory != "" {
		http.Handle("/", http.FileServer(http.Dir(staticDirectory)))
	}
	http.Handle("/wstap", websocket.Handler(websocketHandler))

	InfoPrintf("listening on %s", listenAddress)

	mainFlow := make(chan error, 2)

	go func() {
		if keyFile == "" {
			mainFlow <- http.ListenAndServe(listenAddress, nil)
		} else {
			mainFlow <- http.ListenAndServeTLS(listenAddress, certFile, keyFile, nil)
		}
	}()

	go func() {
		// start a polling goroutine that reads and switches frames from the TAP interface
		mainFlow <- readTAPTraffic()
	}()

	err = <-mainFlow
	if err != nil {
		ErrorPrintf("%v", err)
		hub.Clear()
		os.Exit(7)
	} else {
		hub.Clear()
	}
}
