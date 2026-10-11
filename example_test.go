// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

package talq_test

import (
	"fmt"

	"github.com/cB-Sigma/talq"
)

// Attribute values flatten to a reading; a log record's source splits
// into device and function.
func Example() {
	v, at := talq.AttrValue(map[string]any{"type": "AttributeLevelState", "timestamp": "2026-09-24T12:00:00Z",
		"value": map[string]any{"name": "LevelState", "value": 80.0}})
	fmt.Println(v, at.Format("15:04"))
	dev, fn := talq.SourceDevice("/devices/6F1C0000-0000-4000-8000-0000000000A1/lampActuator/actualLightState")
	fmt.Println(dev, fn)
	fmt.Println(talq.MajorMinor("2.7.1"), talq.IsUUID(dev))
	// Output:
	// 80 12:00
	// 6f1c0000-0000-4000-8000-0000000000a1 lampActuator
	// 2.7 true
}
