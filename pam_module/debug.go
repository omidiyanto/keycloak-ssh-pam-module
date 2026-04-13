package main

import (
	"fmt"
	"os"
	"time"
)

func debugLog(format string, args ...interface{}) {
	f, err := os.OpenFile("/tmp/keycloak-pam-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err == nil {
		msg := fmt.Sprintf("[%s] [DEBUG] ", time.Now().Format(time.RFC3339)) + fmt.Sprintf(format, args...) + "\n"
		f.WriteString(msg)
		f.Close()
	}
}
