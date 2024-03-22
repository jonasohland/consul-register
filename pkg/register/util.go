package register

import (
	"math/rand"
	"time"
)

func Jitter(base time.Duration, jitter time.Duration) time.Duration {
	return base + time.Duration(rand.Int63n(int64(jitter)))
}
