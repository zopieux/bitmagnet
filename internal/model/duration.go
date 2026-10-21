package model

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Duration is stored as integer seconds.
type Duration time.Duration

func (d Duration) Value() (driver.Value, error) {
	return int64(time.Duration(d) / time.Second), nil
}

func (d *Duration) Scan(value interface{}) error {
	switch v := value.(type) {
	case int64:
		*d = Duration(time.Duration(v) * time.Second)
		return nil
	default:
		return fmt.Errorf("cannot sql.Scan() Duration from: %#v", v)
	}
}
