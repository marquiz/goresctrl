/*
Copyright 2026 Intel Corporation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mbox

import (
	"fmt"

	"github.com/intel/goresctrl/pkg/sst/isst"
)

// GetTRLBucketCoreCounts reads the 8 TRL bucket core counts from MSR 0x1AE
// (Turbo Ratio Limit Cores). Each byte of the 64-bit MSR value encodes the
// core count for one bucket.
func GetTRLBucketCoreCounts(cpu uint16) ([8]int, error) {
	var data uint64
	if err := isst.SendMSRCmd(cpu, 0x1ae, false, &data); err != nil {
		return [8]int{}, fmt.Errorf("failed to read TRL bucket core counts: %w", err)
	}
	var counts [8]int
	for i := range 8 {
		counts[i] = int((data >> (uint(i) * 8)) & 0xff)
	}
	return counts, nil
}
