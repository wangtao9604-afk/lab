//go:build !linux || !amd64 || noattr
// +build !linux !amd64 noattr

package log

func createLogFile() {}

func getCurrentLogFileName() string {
	return ""
}
