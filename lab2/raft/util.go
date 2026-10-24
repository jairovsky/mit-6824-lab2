package raft

import "log"
import "os"
import "encoding/binary"

// Debugging
const Debug = 0

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug > 0 {
		log.Printf(format, a...)
	}
	return
}

func randomInt64FromSystem() int64 {

	f, err := os.Open("/dev/urandom")
	if err != nil {
		log.Fatalf("error opening file %v", err)
	}

	var result int64
	err = binary.Read(f, binary.LittleEndian, &result)
	if err != nil {
		log.Fatalf("error reading bytes %v", err)
	}

	return result
}
