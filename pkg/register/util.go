package register

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/r3labs/diff/v3"
)

func Jitter(base time.Duration, jitter time.Duration) time.Duration {
	return base + time.Duration(rand.Int63n(int64(jitter)))
}

func ChangelogToString(changes diff.Changelog) string {
	out := ""

	for i, change := range changes {
		if i != 0 {
			out += ", "
		}

		path := ""
		for pi, e := range change.Path {
			if pi != 0 {
				path += "."
			}

			path += e
		}

		out += fmt.Sprintf("[%s]{%s -> %s}", path, change.From, change.To)
	}

	return out
}
