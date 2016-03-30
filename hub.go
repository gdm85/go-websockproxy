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
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/songgao/water/waterutil"
	"golang.org/x/net/websocket"
)

const (
	defaultFrameBufferSize = 100
)

// Client is a websocket client managed by a Hub.
type Client struct {
	upload, download BandwidthAllowance
	remoteAddress    string
	ws               *websocket.Conn
	authorized       bool

	frameReceiver chan ([]byte)
	terminator    chan (bool)

	mac net.HardwareAddr
}

// Hub is a websocket clients manager.
type Hub struct {
	sync.Mutex
	clients      map[*websocket.Conn]*Client
	clientsByMAC map[string]*Client
}

// RateLimiter is an interface to limit upload and/or download bandwidths.
type RateLimiter interface {
	UploadThrottle(frameLen int) bool
	DownloadThrottle(frameLen int) bool
}

// Add will add a client to the hub and initialize its frames delivery and eventual bandwidth limiting features.
func (h *Hub) Add(ws *websocket.Conn) *Client {
	h.Lock()
	c := &Client{
		remoteAddress: ws.Request().RemoteAddr,
		ws:            ws,
		authorized:    authKey == "", // pre-authorize all clients when authorization is disabled
		frameReceiver: make(chan []byte, defaultFrameBufferSize),
		terminator:    make(chan bool),
	}
	if uploadBandwidth != 0 {
		c.upload.rate = uploadBandwidth
		c.upload.allowance = uploadBandwidth
		c.upload.lastCheck = time.Now()
	}
	if downloadBandwidth != 0 {
		c.download.rate = downloadBandwidth
		c.download.allowance = downloadBandwidth
		c.download.lastCheck = time.Now()
	}

	h.clients[ws] = c
	h.Unlock()

	go func() {
		err := c.deliverFrames()
		if err != nil {
			ErrorPrintf("client %v: dropping client because of error during send: %v", c, err)
			h.Remove(c)
		}
	}()

	return c
}

// deliverFrames delivers the frames buffered in the receiving channel.
func (c *Client) deliverFrames() error {
	for {
		select {
		case <-c.terminator:
			DebugPrintf("client %v: terminated delivery of received frames (%d pending)", len(c.frameReceiver))
			return nil
		case frame := <-c.frameReceiver:
			if c.DownloadThrottle(len(frame)) {
				WarningPrintf("client %v, frame %v: discarding because of download rate limiting", c, frame)
			} else {
				err := websocket.Message.Send(c.ws, frame)
				if err != nil {
					return err
				}
				DebugPrintf("client %v, frame %v: sent", c, frame)
			}
		}
	}
}

// UploadThrottle returns true if the payload should be throttled.
func (c *Client) UploadThrottle(frameLen int) bool {
	return c.upload.DoThrottle(frameLen)
}

// DownloadThrottle returns true if the payload should be throttled.
func (c *Client) DownloadThrottle(frameLen int) bool {
	return c.download.DoThrottle(frameLen)
}

// String returns a human-readable descriptive text of the client.
func (c *Client) String() string {
	return fmt.Sprintf("{remote=%s mac=%v authorized=%v pendingFrames=%d}", c.remoteAddress, c.mac, c.authorized, len(c.frameReceiver))
}

// Remove will remove the client from the hub and terminate its delivery goroutine.
func (h *Hub) Remove(c *Client) {
	h.Lock()
	if _, ok := h.clients[c.ws]; ok {
		// stop delivery of messages
		c.terminator <- true

		delete(h.clients, c.ws)
		if c.mac.String() != "" {
			delete(h.clientsByMAC, c.mac.String())
		}
		DebugPrintf("deleted client %v", c)
	}
	h.Unlock()
}

// Clear will remove all clients and terminate their delivery goroutines.
func (h *Hub) Clear() {
	h.Lock()
	for _, c := range h.clients {
		// stop delivery of messages
		c.terminator <- true
		DebugPrintf("deleted client %v", c)
	}
	h.clients = map[*websocket.Conn]*Client{}
	h.clientsByMAC = map[string]*Client{}
	h.Unlock()
}

// NewHub returns an initialized hub.
func NewHub() *Hub {
	h := &Hub{}
	h.clients = map[*websocket.Conn]*Client{}
	h.clientsByMAC = map[string]*Client{}
	return h
}

// HandleSpecialFrame handles a special frame; currently only AUTH is supported, PING could be added here.
func (c *Client) HandleSpecialFrame(payload []byte) (skipFrame, flagAsBad bool, e error) {
	if len(payload) < 8 {
		skipFrame = true
		e = fmt.Errorf("too short special frame payload (%d bytes)", len(payload))
		return
	}

	prefix := string(payload[:5])
	switch prefix {
	case "AUTH ":
		DebugPrintf("received auth frame: %q", string(payload))
		if authKey == "" {
			e = errors.New("ignoring AUTH frame (authorization disabled on server side)")
			skipFrame = true
			return
		}
		if c.authorized {
			skipFrame = true
			e = errors.New("client already authorized, ignoring AUTH")
			return
		}
		key := string(payload[5:])
		if key == authKey {
			c.authorized = true
			InfoPrintf("AUTH key accepted")
			skipFrame = true
			return
		}
		// failure to authorize
		e = errors.New("AUTH key not accepted")
		skipFrame = true
		// do not close the connection but put it in an idle loop
		flagAsBad = true
		return
	}
	e = errors.New("invalid special frame: " + prefix)
	skipFrame = true
	return
}

// CanSourceMac returns true if the client is misbehaving and should be blocked, and an error if client is not allowed to source frames from the specified MAC address.
func (h *Hub) CanSourceMAC(c *Client, mac net.HardwareAddr) (bool, error) {
	h.Lock()
	src := mac.String()

	existingClient, ok := h.clientsByMAC[src]
	if !ok {
		///
		/// first time client sends a ethernet frame, associate with this MAC
		///

		// make sure this is a valid MAC address
		if src == "ff:ff:ff:ff:ff:ff" || strings.HasPrefix(src, "33:33") || strings.HasPrefix(src, "01:00:5e") {
			h.Unlock()
			return true, errors.New("MAC address is invalid")
		}

		// if MAC prefix whitelisting is enabled, validate against it
		if macPrefix != "" && !strings.HasPrefix(src, macPrefix) {
			h.Unlock()
			return true, errors.New("MAC address will not be accepted")
		}

		c.mac = mac
		h.clientsByMAC[src] = c
		InfoPrintf("client %v: now associated with MAC %s", c, src)
		h.Unlock()
		return false, nil
	}

	if c != existingClient {
		// very bad thing: the client is trying to send with the MAC of another client :(
		h.Unlock()
		return true, fmt.Errorf("client %v tried to send traffic as client %v", c, existingClient)
	}

	// AOK - client can send with this MAC
	h.Unlock()
	return false, nil
}

// SwitchFrame switches a frame to either broadcast addresses or local websocket clients; returns true if frame was handled and an error in case of delivery errors.
// based on https://github.com/benjamincburns/websockproxy/blob/master/switchedrelay.py
func (h *Hub) SwitchFrame(source RateLimiter, frame []byte) (bool, error) {
	h.Lock()
	defer h.Unlock()

	dst := waterutil.MACDestination(frame)
	if waterutil.IsBroadcast(dst) && waterutil.IsIPv4Multicast(dst) {
		// broadcast message to all known peers
		for _, peer := range h.clientsByMAC {
			peer.Download(frame)
		}
		if source != nil {
			// finally broadcast on TAP interface itself
			if source.UploadThrottle(len(frame)) {
				WarningPrintf("client %v, frame %v: discarding because of upload rate limiting", source, frame)
			} else {
				_, err := tap.Write(frame)
				if err != nil {
					return false, err
				}
			}
		}
		return true, nil
	}

	// send to a specific peer
	if peer, ok := h.clientsByMAC[dst.String()]; ok {
		peer.Download(frame)
		return true, nil
	}
	if source != nil {
		// send on TAP interface itself
		if source.UploadThrottle(len(frame)) {
			WarningPrintf("client %v, frame %v: discarding because of upload rate limiting", source, frame)
		} else {
			_, err := tap.Write(frame)
			if err != nil {
				return false, err
			}
		}
		return true, nil
	}
	return false, nil
}

// Download queues a frame for receipt into the websocket stream of a specific client; the call is non-blocking.
func (c *Client) Download(frame []byte) {
	go func() { c.frameReceiver <- frame }()
	DebugPrintf("client %v, frame %v: queued for receipt", c, frame)
}
