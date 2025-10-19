//go:build debug

package debug

import (
	"io"
	"log"
	"os"
)

var logger *log.Logger

func init() {
	f, err := os.OpenFile("/tmp/debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		// fallback to stdout
		logger = log.New(os.Stdout, "[DEBUG] ", log.LstdFlags|log.Lshortfile)
		return
	}
	stream := io.MultiWriter(f, os.Stdout)
	logger = log.New(stream, "[DEBUG] ", log.LstdFlags|log.Lshortfile)
}

func Printf(format string, v ...any) {
	logger.Printf(format, v...)
}
