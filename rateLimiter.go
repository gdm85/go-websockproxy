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
	"strconv"
	"strings"
	"sync"
	"time"
)

func parseBandwidth(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "kbps") {
		s = strings.TrimSpace(s[:len(s)-4])
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, err
		}
		return n * 1000, nil
	}
	if strings.HasSuffix(s, "mbps") {
		s = strings.TrimSpace(s[:len(s)-4])
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, err
		}
		return n * 1000 * 1000, nil
	}
	if strings.HasSuffix(s, "kbit") {
		s = strings.TrimSpace(s[:len(s)-4])
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, err
		}
		return n * 125, nil
	}
	if strings.HasSuffix(s, "mbit") {
		s = strings.TrimSpace(s[:len(s)-4])
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, err
		}
		return n * 1000 * 125, nil
	}
	if strings.HasSuffix(s, "bps") {
		s = strings.TrimSpace(s[:len(s)-4])
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return n, nil
}

/* BandwidthAllowance is a simple rate-limiting device.
Bandwidths or rates can be specified in:
* kbps - Kilobytes per second
* mbps - Megabytes per second
* kbit - Kilobits per second
* mbit - Megabits per second
* bps or a bare number - Bytes per second */
type BandwidthAllowance struct {
	sync.Mutex
	lastCheck       time.Time
	allowance, rate int64
}

// DoThrottle returns true if the payload of specified size needs to be throttled (dropped).
func (ba BandwidthAllowance) DoThrottle(size int) bool {
	if ba.rate == 0 {
		return false
	}
	ba.Lock()
	now := time.Now()
	timePassed := now.Sub(ba.lastCheck)

	ba.lastCheck = now
	ba.allowance += int64(timePassed.Seconds() * float64(ba.rate))

	if ba.allowance > ba.rate {
		ba.allowance = ba.rate // throttle
	}

	if ba.allowance > 1.0 {
		ba.allowance -= int64(size)
		ba.Unlock()
		return true
	}

	ba.Unlock()
	return false
}
