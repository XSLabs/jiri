// Copyright 2021 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"syscall"
)

func platform_init() {
	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		fmt.Println("Warning: unable to obtain rlimit: ", err)
		return
	}
	// The max file limit is 10240 for osx, even though
	// the max returned by Getrlimit is 1<<63-1.
	// This is defined in OPEN_MAX in sys/syslimits.h.
	if rLimit.Cur < 10240 {
		rLimit.Cur = 10240
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
			fmt.Println("Warning: unable to increase rlimit: ", err)
		}
	}

}
