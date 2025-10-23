// Copyright 2020 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package retry

import (
	"testing"
	"time"
)

func TestExponentialBackOff(t *testing.T) {
	backoff := newExponentialBackoff(
		/* initial */ 5*time.Second,
		/* max */ 64*time.Second,
		/* multiplier */ 2,
	)

	expectedDurations := []time.Duration{
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		64 * time.Second,
		64 * time.Second,
	}
	for _, want := range expectedDurations {
		if got := backoff.nextBackoff(); got != want {
			t.Errorf("Expecting backoff of %s, got %s", want, got)
		}
	}
}
